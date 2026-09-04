// Package integration contiene pruebas end-to-end que arrancan el servidor
// NexusCloud completo (server.Build) y lo ejercitan por HTTP real, tal como
// lo haría un cliente. Complementa (no sustituye) los tests unitarios de
// cada paquete: aquí se comprueba el cableado completo — router, middleware,
// handlers, servicios y repositorios — junto con las propiedades de
// seguridad que solo son observables a este nivel (§14 del plan de Fase 1).
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/server"
	"github.com/porrii/nexuscloud/internal/users"
)

// listResponseDTO refleja la forma de GET /api/v1/files y GET /api/v1/trash
// (ver internal/api/v1/files_handlers.go: listResponse).
type listResponseDTO struct {
	Directories []struct {
		ID string `json:"id"`
	} `json:"directories"`
	Files []struct {
		ID string `json:"id"`
	} `json:"files"`
}

// newTestServer arranca un server.Server real sobre sqlite en un directorio
// temporal, con rate limiting generoso (los tests de rate limiting propios
// usan límites ajustados por separado).
func newTestServer(t *testing.T) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.Build(cfg, logger)
	if err != nil {
		t.Fatalf("server.Build falló: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, srv
}

// createAdmin crea directamente en base de datos el primer usuario
// (super_admin), replicando lo que hace `nexuscloud admin create-user` sin
// pasar por el CLI.
func createAdmin(t *testing.T, srv *server.Server, username, password string) {
	t.Helper()
	cfg := config.Defaults()
	hasher := auth.NewHasher(cfg.Security.Argon2)
	hash, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash falló: %v", err)
	}

	// Todos los tests de este fichero usan sqlite (ver newTestServer).
	conn := db.Wrap("sqlite", srv.DB)
	userRepo := users.NewSQLRepository(conn)
	if _, err := users.NewService(userRepo).CreateUser(context.Background(), users.CreateUserInput{
		Username: username, PasswordHash: hash, Role: users.RoleSuperAdmin,
	}); err != nil {
		t.Fatalf("creando admin de prueba: %v", err)
	}
}

type apiClient struct {
	t      *testing.T
	base   string
	token  string
	client *http.Client
}

func (c *apiClient) do(method, path string, body any, token string) *http.Response {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("petición %s %s falló: %v", method, path, err)
	}
	return resp
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decodificando JSON: %v", err)
	}
	return v
}

func TestFullHappyPathFlow(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	t.Run("health y ready responden", func(t *testing.T) {
		if resp := c.do(http.MethodGet, "/health", nil, ""); resp.StatusCode != http.StatusOK {
			t.Errorf("/health = %d", resp.StatusCode)
		}
		if resp := c.do(http.MethodGet, "/ready", nil, ""); resp.StatusCode != http.StatusOK {
			t.Errorf("/ready = %d", resp.StatusCode)
		}
	})

	var adminToken string
	t.Run("login admin", func(t *testing.T) {
		resp := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
			"username": "admin", "password": "contraseña-admin-segura",
		}, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login = %d", resp.StatusCode)
		}
		login := decodeJSON[struct {
			Token string `json:"token"`
		}](t, resp)
		if login.Token == "" {
			t.Fatal("login no devolvió token")
		}
		adminToken = login.Token
	})

	t.Run("/users/me sin token da 401", func(t *testing.T) {
		resp := c.do(http.MethodGet, "/api/v1/users/me", nil, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, esperado 401", resp.StatusCode)
		}
	})

	t.Run("/users/me con token da el usuario correcto", func(t *testing.T) {
		resp := c.do(http.MethodGet, "/api/v1/users/me", nil, adminToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		me := decodeJSON[struct {
			Username string `json:"username"`
		}](t, resp)
		if me.Username != "admin" {
			t.Errorf("username = %q, esperado admin", me.Username)
		}
	})

	var fileID string
	t.Run("subir y descargar un archivo (round-trip íntegro)", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/files?name=informe.txt&path=/Documentos",
			bytes.NewReader([]byte("contenido con ñ y áéíóú")))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("upload falló: %v", err)
		}
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("upload status = %d", resp.StatusCode)
		}
		f := decodeJSON[struct {
			ID string `json:"id"`
		}](t, resp)
		fileID = f.ID

		resp = c.do(http.MethodGet, "/api/v1/files/"+fileID, nil, adminToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download status = %d", resp.StatusCode)
		}
		defer resp.Body.Close()
		got, _ := io.ReadAll(resp.Body)
		if string(got) != "contenido con ñ y áéíóú" {
			t.Errorf("contenido descargado = %q", got)
		}
		if cd := resp.Header.Get("Content-Disposition"); cd == "" {
			t.Error("falta Content-Disposition en la descarga (§192)")
		}
	})

	t.Run("path traversal en el nombre es rechazado", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/files?name=..%2F..%2F..%2Fetc%2Fpasswd&path=/",
			bytes.NewReader([]byte("x")))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("petición falló: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, esperado 400 (path traversal debe rechazarse, §73)", resp.StatusCode)
		}
	})

	var victimToken, attackerToken string
	t.Run("crear un segundo usuario vía invitación y comprobar IDOR real por HTTP", func(t *testing.T) {
		resp := c.do(http.MethodPost, "/api/v1/invitations", map[string]any{"max_uses": 1}, adminToken)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("crear invitación = %d", resp.StatusCode)
		}
		inv := decodeJSON[struct {
			Token string `json:"token"`
		}](t, resp)

		resp = c.do(http.MethodPost, "/api/v1/invitations/redeem", map[string]string{
			"token": inv.Token, "username": "atacante", "password": "contraseña-del-atacante",
		}, "")
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("redeem = %d", resp.StatusCode)
		}

		// Un segundo canje de la misma invitación (max_uses=1) debe fallar.
		resp = c.do(http.MethodPost, "/api/v1/invitations/redeem", map[string]string{
			"token": inv.Token, "username": "otro", "password": "otra-contraseña",
		}, "")
		if resp.StatusCode != http.StatusGone {
			t.Errorf("segundo canje = %d, esperado 410 Gone (invitación agotada)", resp.StatusCode)
		}

		resp = c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
			"username": "atacante", "password": "contraseña-del-atacante",
		}, "")
		login := decodeJSON[struct {
			Token string `json:"token"`
		}](t, resp)
		attackerToken = login.Token
		victimToken = adminToken

		// El "atacante" conoce el ID del archivo del admin (podría haberlo
		// visto en un log, una URL compartida, etc.) pero no es su dueño.
		resp = c.do(http.MethodGet, "/api/v1/files/"+fileID, nil, attackerToken)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("acceso cruzado a archivo ajeno = %d, esperado 403 (§198 IDOR)", resp.StatusCode)
		}
		// El propietario real conserva el acceso.
		resp = c.do(http.MethodGet, "/api/v1/files/"+fileID, nil, victimToken)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("el propietario legítimo debería seguir accediendo: %d", resp.StatusCode)
		}
	})

	t.Run("un usuario normal no puede listar usuarios (admin-only)", func(t *testing.T) {
		resp := c.do(http.MethodGet, "/api/v1/users", nil, attackerToken)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, esperado 403", resp.StatusCode)
		}
	})

	t.Run("borrar archivo lo mueve a la papelera, restaurar y purgar para siempre", func(t *testing.T) {
		// Delete por defecto es soft-delete (§16): el archivo sigue
		// existiendo (y siendo descargable por su dueño) pero desaparece
		// del listado normal y aparece en /trash.
		resp := c.do(http.MethodDelete, "/api/v1/files/"+fileID, nil, adminToken)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete = %d", resp.StatusCode)
		}
		resp = c.do(http.MethodGet, "/api/v1/files/"+fileID, nil, adminToken)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("un archivo en la papelera debería seguir siendo descargable por su dueño: status = %d", resp.StatusCode)
		}

		resp = c.do(http.MethodGet, "/api/v1/files?path=/Documentos", nil, adminToken)
		listing := decodeJSON[listResponseDTO](t, resp)
		for _, f := range listing.Files {
			if f.ID == fileID {
				t.Error("un archivo en la papelera no debería aparecer en el listado normal")
			}
		}

		resp = c.do(http.MethodGet, "/api/v1/trash", nil, adminToken)
		trash := decodeJSON[listResponseDTO](t, resp)
		found := false
		for _, f := range trash.Files {
			if f.ID == fileID {
				found = true
			}
		}
		if !found {
			t.Errorf("el archivo borrado debería aparecer en /trash: %+v", trash.Files)
		}

		// Restaurar lo devuelve al listado normal.
		resp = c.do(http.MethodPost, "/api/v1/files/"+fileID+"/restore", nil, adminToken)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("restore = %d", resp.StatusCode)
		}
		resp = c.do(http.MethodGet, "/api/v1/files?path=/Documentos", nil, adminToken)
		listing = decodeJSON[listResponseDTO](t, resp)
		found = false
		for _, f := range listing.Files {
			if f.ID == fileID {
				found = true
			}
		}
		if !found {
			t.Error("tras restaurar, el archivo debería volver a aparecer en el listado normal")
		}

		// Borrado definitivo: ahora sí desaparece de verdad.
		resp = c.do(http.MethodDelete, "/api/v1/files/"+fileID+"?permanent=true", nil, adminToken)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete permanente = %d", resp.StatusCode)
		}
		resp = c.do(http.MethodGet, "/api/v1/files/"+fileID, nil, adminToken)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status tras borrado definitivo = %d, esperado 404", resp.StatusCode)
		}
	})

	t.Run("logout invalida la sesión", func(t *testing.T) {
		resp := c.do(http.MethodPost, "/api/v1/auth/logout", nil, adminToken)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("logout = %d", resp.StatusCode)
		}
		resp = c.do(http.MethodGet, "/api/v1/users/me", nil, adminToken)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("tras logout, el token debería quedar inválido: status = %d", resp.StatusCode)
		}
	})
}

func TestLoginIsRateLimited(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 3
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.Build(cfg, logger)
	if err != nil {
		t.Fatalf("server.Build falló: %v", err)
	}
	defer srv.Close()
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	var last *http.Response
	for i := 0; i < 6; i++ {
		last = c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
			"username": "admin", "password": "contraseña-incorrecta",
		}, "")
		last.Body.Close()
		if last.StatusCode == http.StatusTooManyRequests {
			break
		}
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Errorf("tras varios intentos fallidos seguidos, esperaba 429 en algún momento; último status = %d (§27)", last.StatusCode)
	}
}

func TestDoubleSubmitOfSamePathOverwrites(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	resp := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "contraseña-admin-segura",
	}, "")
	login := decodeJSON[struct {
		Token string `json:"token"`
	}](t, resp)

	upload := func(content string) string {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/files?name=notas.txt&path=/", bytes.NewReader([]byte(content)))
		req.Header.Set("Authorization", "Bearer "+login.Token)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("upload falló: %v", err)
		}
		f := decodeJSON[struct {
			ID string `json:"id"`
		}](t, resp)
		return f.ID
	}

	firstID := upload("v1")
	secondID := upload("versión más larga")
	if firstID != secondID {
		t.Errorf("subir dos veces al mismo path debería conservar el mismo id: %q != %q", firstID, secondID)
	}
}
