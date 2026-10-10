// Tests de integración de ADR-044: el rol read_only puede leer y gestionar
// su propia cuenta, pero ninguna ruta que escriba datos lo deja pasar.
package integration

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/server"
)

const (
	readOnlyAdminPassword  = "contrasena-larga-123"
	readOnlyReaderPassword = "contrasena-larga-456"
)

// readOnlySelfServiceRoutes son las ÚNICAS rutas autenticadas que escriben y
// que un read_only puede usar (ADR-044 Decisión 1): el autoservicio de su
// cuenta y revocar lo propio. Añadir una aquí es una decisión de seguridad
// que debe constar en ADR-044, no un arreglo para que pase el test.
var readOnlySelfServiceRoutes = map[string]bool{
	"POST /api/v1/auth/logout":                      true,
	"POST /api/v1/auth/reauthenticate":              true, // ADR-042 Decisión 2 (nota en ADR-044 Decisión 1)
	"DELETE /api/v1/auth/sessions/{id}":             true,
	"POST /api/v1/auth/totp/enroll":                 true,
	"POST /api/v1/auth/totp/verify":                 true,
	"POST /api/v1/auth/api-tokens":                  true,
	"DELETE /api/v1/auth/api-tokens/{id}":           true,
	"POST /api/v1/auth/webauthn/register/begin":     true,
	"POST /api/v1/auth/webauthn/register/finish":    true,
	"DELETE /api/v1/auth/webauthn/credentials/{id}": true,
	"POST /api/v1/auth/webdav/tokens":               true,
	"DELETE /api/v1/auth/webdav/tokens/{id}":        true,
	"POST /api/v1/favorites":                        true,
	"DELETE /api/v1/favorites/{id}":                 true,
	"DELETE /api/v1/shares/{id}":                    true,
	"DELETE /api/v1/anonymous-uploads/{id}":         true,
}

// unauthenticatedPrefixes son las rutas sin sesión: RequireWritable no
// aplica (no hay usuario), y tienen sus propias pruebas.
var unauthenticatedPrefixes = []string{
	"/api/v1/auth/login",
	"/api/v1/auth/webauthn/login/",
	"/api/v1/invitations/redeem",
	"/api/v1/public/",
	"/api/v1/backups/inbound/",
}

func newReadOnlyTestServer(t *testing.T) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 100000
	cfg.API.Enabled = true
	// Activadas para que sus rutas existan y el recorrido las cubra.
	cfg.WebDAV.Enabled = true
	cfg.Sharing.PublicLinksEnabled = true
	cfg.Sharing.AnonymousUploadEnabled = true

	srv, err := server.Build(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("server.Build falló: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, srv
}

// createReader da de alta un usuario read_only por la API, como lo haría
// un administrador, y devuelve su token de sesión.
func createReader(t *testing.T, c *apiClient, adminToken, username string) string {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": username, "password": readOnlyReaderPassword, "role": "read_only",
	}, adminToken)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("crear usuario read_only = %d, cuerpo = %s", resp.StatusCode, b)
	}
	resp.Body.Close()
	return loginToken(t, c, username, readOnlyReaderPassword)
}

func errorCodeOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var e errorDTO
	_ = json.Unmarshal(body, &e)
	return e.Error.Code
}

var routeParam = regexp.MustCompile(`\{[^}]+\}`)

// TestReadOnlyAccountCannotWriteThroughAnyRoute es la función de aptitud de
// ADR-044 Decisión 5: recorre TODAS las rutas registradas y exige que cada
// ruta autenticada que escribe, salvo el autoservicio de la lista, responda
// 403 read_only_account a un usuario read_only. Una ruta nueva colocada
// fuera de RequireWritable rompe este test.
func TestReadOnlyAccountCannotWriteThroughAnyRoute(t *testing.T) {
	ts, srv := newReadOnlyTestServer(t)
	createAdmin(t, srv, "admin", readOnlyAdminPassword)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "admin", readOnlyAdminPassword)
	reader := createReader(t, c, admin, "lector")

	routes, ok := srv.Handler.(chi.Routes)
	if !ok {
		t.Fatalf("srv.Handler es %T, esperado un chi.Routes para poder recorrerlo", srv.Handler)
	}

	var checked int
	seenSelfService := map[string]bool{}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.ReplaceAll(route, "/*/", "/")
		if !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		for _, p := range unauthenticatedPrefixes {
			if strings.HasPrefix(route, p) {
				return nil
			}
		}
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return nil
		}
		key := method + " " + route
		if readOnlySelfServiceRoutes[key] {
			// No se llaman: logout cerraría la sesión del recorrido y TOTP
			// cambiaría la cuenta. Su uso real lo cubre el test de abajo.
			seenSelfService[key] = true
			return nil
		}

		path := routeParam.ReplaceAllString(route, "00000000-0000-4000-8000-000000000000")
		resp := c.do(method, path, map[string]any{}, reader)
		if resp.StatusCode != http.StatusForbidden {
			resp.Body.Close()
			t.Errorf("%s con un usuario read_only = %d, esperado 403 read_only_account "+
				"(¿ruta nueva fuera de RequireWritable? ver ADR-044)", key, resp.StatusCode)
			return nil
		}
		if code := errorCodeOf(t, resp); code != "read_only_account" {
			t.Errorf("%s con un usuario read_only: código = %q, esperado read_only_account", key, code)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	// Salvaguarda contra un recorrido que no encuentre nada (p. ej. si el
	// montaje cambia y deja de ser recorrible): hoy hay más de 20.
	if checked < 20 {
		t.Errorf("solo se comprobaron %d rutas que escriben; el recorrido no está viendo el router", checked)
	}
	for _, key := range []string{"POST /api/v1/auth/logout", "POST /api/v1/favorites", "DELETE /api/v1/shares/{id}"} {
		if !seenSelfService[key] {
			t.Errorf("%s no aparece en el router: la lista de autoservicio está desfasada", key)
		}
	}
}

func TestReadOnlyAccountCanReadAndManageItsOwnAccount(t *testing.T) {
	ts, srv := newReadOnlyTestServer(t)
	createAdmin(t, srv, "admin", readOnlyAdminPassword)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "admin", readOnlyAdminPassword)
	reader := createReader(t, c, admin, "lector")

	// El administrador comparte con el lector una carpeta CON permiso de
	// subida: aun así no puede subir (el rol es un techo, ADR-044).
	resp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Equipo"}, admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear /Equipo = %d", resp.StatusCode)
	}
	dir := decodeJSON[directoryDTO](t, resp)
	file := uploadTestFile(t, c, admin, "/Equipo", "plan.txt", "contenido compartido")
	resp = c.do(http.MethodPost, "/api/v1/shares", map[string]any{
		"resource_type": "directory", "resource_id": dir.ID, "share_type": "user",
		"target_username": "lector", "can_download": true, "can_upload": true,
	}, admin)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("compartir con el lector = %d, cuerpo = %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	for _, path := range []string{"/api/v1/files?path=/", "/api/v1/shares?direction=with-me", "/api/v1/shared-directories/" + dir.ID, "/api/v1/files/" + file.ID} {
		resp := c.do(http.MethodGet, path, nil, reader)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s con read_only = %d, esperado 200", path, resp.StatusCode)
		}
	}

	upload, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/shared-directories/"+dir.ID+"/files?name=intruso.txt", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	upload.Header.Set("Authorization", "Bearer "+reader)
	resp, err = ts.Client().Do(upload)
	if err != nil {
		t.Fatalf("subida a la carpeta compartida: %v", err)
	}
	wantErrorCode(t, resp, http.StatusForbidden, "read_only_account")

	// Autoservicio: crear un token de API funciona... y el token hereda el
	// rol: con él tampoco se escribe.
	resp = c.do(http.MethodPost, "/api/v1/auth/api-tokens", map[string]any{"name": "script"}, reader)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("crear token de API con read_only = %d, cuerpo = %s", resp.StatusCode, b)
	}
	apiToken := decodeJSON[tokenDTO](t, resp).Token
	wantErrorCode(t, c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "X"}, apiToken),
		http.StatusForbidden, "read_only_account")
	resp = c.do(http.MethodGet, "/api/v1/files?path=/", nil, apiToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /files con el token de API de un read_only = %d, esperado 200", resp.StatusCode)
	}

	// Favoritos y revocar lo propio no pasan por el guardia: un id
	// inexistente da 404, no 403 read_only_account.
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/favorites", map[string]string{"resource_type": "file", "resource_id": file.ID}},
		{http.MethodDelete, "/api/v1/shares/00000000-0000-4000-8000-000000000000", nil},
	} {
		resp := c.do(req.method, req.path, req.body, reader)
		if code := errorCodeOf(t, resp); code == "read_only_account" {
			t.Errorf("%s %s con read_only = read_only_account, esperado que no pase por el guardia", req.method, req.path)
		}
	}

	// Un usuario normal sigue pudiendo escribir.
	uploadTestFile(t, c, admin, "/", "sigue-funcionando.txt", "x")
}

func TestUnknownRoleIsRejectedWithoutCreatingAnything(t *testing.T) {
	ts, srv := newReadOnlyTestServer(t)
	createAdmin(t, srv, "admin", readOnlyAdminPassword)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "admin", readOnlyAdminPassword)

	wantErrorCode(t, c.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "intruso", "password": readOnlyReaderPassword, "role": "root",
	}, admin), http.StatusBadRequest, "invalid_role")
	resp := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "intruso", "password": readOnlyReaderPassword}, "")
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("el usuario con rol desconocido no debería existir (antes quedaba creado sin rol)")
	}

	wantErrorCode(t, c.do(http.MethodPost, "/api/v1/invitations", map[string]any{"role": "root"}, admin),
		http.StatusBadRequest, "invalid_role")
}
