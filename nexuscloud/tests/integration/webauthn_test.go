package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/server"
	"github.com/porrii/nexuscloud/internal/users"
)

// newWebAuthnTestServer arranca el mismo servidor real que newTestServer
// (e2e_test.go), pero con security.webAuthn.enabled=true -- por defecto
// está desactivado (secure by default, ver config.Defaults), así que las
// rutas /auth/webauthn/* ni se registran a menos que se pida aquí.
func newWebAuthnTestServer(t *testing.T) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true
	cfg.Security.WebAuthn = config.WebAuthnConfig{Enabled: true, RPID: "localhost", RPOrigin: "http://localhost"}

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

// insertWebAuthnCredential inserta una credencial directamente en base de
// datos, saltándose la ceremonia real -- mismo criterio que createAdmin
// (e2e_test.go), que inserta el usuario admin saltándose el CLI. No hay
// forma de completar una ceremonia WebAuthn de verdad sin un navegador
// real (eso es Task #5, Chrome DevTools virtual authenticator); lo que sí
// se puede probar aquí es todo el cableado alrededor: rutas, sesión, IDOR,
// y la puerta que Login exige cuando el usuario ya tiene un passkey.
func insertWebAuthnCredential(t *testing.T, srv *server.Server, userID, label string) *auth.WebAuthnCredential {
	t.Helper()
	repo := auth.NewSQLWebAuthnCredentialRepository(db.Wrap("sqlite", srv.DB))
	c := &auth.WebAuthnCredential{
		ID: idgen.New(), UserID: userID, CredentialID: "cred-" + idgen.New(),
		PublicKey: "pk-de-prueba", Label: label, CreatedAt: time.Now().UTC(),
	}
	if err := repo.CreateCredential(context.Background(), c); err != nil {
		t.Fatalf("insertando credencial WebAuthn de prueba: %v", err)
	}
	return c
}

func TestWebAuthnRoutesNotRegisteredWhenDisabled(t *testing.T) {
	ts, srv := newTestServer(t) // security.webAuthn.enabled=false, por defecto
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	token := loginAndGetToken(t, c, "admin", "contraseña-admin-segura")
	resp := c.do(http.MethodPost, "/api/v1/auth/webauthn/register/begin", nil, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404 (ruta no registrada con webAuthn desactivado)", resp.StatusCode)
	}
}

func TestWebAuthnRegisterBeginRequiresAuth(t *testing.T) {
	ts, _ := newWebAuthnTestServer(t)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	resp := c.do(http.MethodPost, "/api/v1/auth/webauthn/register/begin", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, esperado 401 sin sesión", resp.StatusCode)
	}
}

func TestWebAuthnRegisterBeginReturnsChallenge(t *testing.T) {
	ts, srv := newWebAuthnTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	token := loginAndGetToken(t, c, "admin", "contraseña-admin-segura")

	resp := c.do(http.MethodPost, "/api/v1/auth/webauthn/register/begin", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := decodeJSON[struct {
		CeremonyID string `json:"ceremony_id"`
		PublicKey  struct {
			RP struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				Name string `json:"name"`
			} `json:"user"`
		} `json:"publicKey"`
	}](t, resp)
	if body.CeremonyID == "" {
		t.Error("ceremony_id no debería estar vacío")
	}
	if body.PublicKey.RP.ID != "localhost" {
		t.Errorf("publicKey.rp.id = %q, esperado localhost", body.PublicKey.RP.ID)
	}
	if body.PublicKey.User.Name != "admin" {
		t.Errorf("publicKey.user.name = %q, esperado admin", body.PublicKey.User.Name)
	}
}

func TestWebAuthnLoginBeginDiscoverableNeedsNoCredentials(t *testing.T) {
	ts, _ := newWebAuthnTestServer(t)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	// Sin username/password: login passwordless discoverable, no requiere
	// ningún usuario/credencial previa para poder EMITIR el reto (el propio
	// navegador es quien decide si tiene algo que ofrecer).
	resp := c.do(http.MethodPost, "/api/v1/auth/webauthn/login/begin", map[string]string{}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := decodeJSON[struct {
		CeremonyID string `json:"ceremony_id"`
	}](t, resp)
	if body.CeremonyID == "" {
		t.Error("ceremony_id no debería estar vacío")
	}
}

func TestWebAuthnLoginBeginTwoFactorRejectsWrongPassword(t *testing.T) {
	ts, srv := newWebAuthnTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	resp := c.do(http.MethodPost, "/api/v1/auth/webauthn/login/begin", map[string]string{
		"username": "admin", "password": "contraseña-incorrecta",
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, esperado 401 con contraseña incorrecta", resp.StatusCode)
	}
}

func TestLoginRequiresWebAuthnWhenCredentialRegistered(t *testing.T) {
	ts, srv := newWebAuthnTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	admin := getUserByUsername(t, srv, "admin")
	insertWebAuthnCredential(t, srv, admin.ID, "portátil de trabajo")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	// Contraseña correcta y sin passkey de por medio: con un passkey ya
	// registrado, /auth/login no debe completar la sesión -- si lo
	// hiciera, el passkey no protegería nada de verdad (el segundo factor
	// sería pura decoración).
	resp := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "contraseña-admin-segura",
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (webauthn_required)", resp.StatusCode)
	}
	body := decodeJSON[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, resp)
	if body.Error.Code != "webauthn_required" {
		t.Errorf("code = %q, esperado webauthn_required", body.Error.Code)
	}
}

func TestWebAuthnCredentialsListAndRevoke(t *testing.T) {
	ts, srv := newWebAuthnTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	admin := getUserByUsername(t, srv, "admin")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	token := loginAndGetToken(t, c, "admin", "contraseña-admin-segura")

	cred := insertWebAuthnCredential(t, srv, admin.ID, "llave USB")

	resp := c.do(http.MethodGet, "/api/v1/auth/webauthn/credentials", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	list := decodeJSON[[]struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}](t, resp)
	if len(list) != 1 || list[0].ID != cred.ID || list[0].Label != "llave USB" {
		t.Errorf("list = %+v", list)
	}

	// IDOR: otro usuario no puede revocar la credencial del admin.
	createAdmin(t, srv, "otro", "contraseña-otro-usuario")
	otroToken := loginAndGetToken(t, c, "otro", "contraseña-otro-usuario")
	resp = c.do(http.MethodDelete, "/api/v1/auth/webauthn/credentials/"+cred.ID, nil, otroToken)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("revocar con otro usuario: status = %d, esperado 404", resp.StatusCode)
	}

	resp = c.do(http.MethodDelete, "/api/v1/auth/webauthn/credentials/"+cred.ID, nil, token)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revocar con el propietario: status = %d", resp.StatusCode)
	}

	resp = c.do(http.MethodGet, "/api/v1/auth/webauthn/credentials", nil, token)
	list = decodeJSON[[]struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}](t, resp)
	if len(list) != 0 {
		t.Errorf("list tras revocar = %+v, esperada vacía", list)
	}
}

func loginAndGetToken(t *testing.T, c *apiClient, username, password string) string {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": username, "password": password,
	}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login de %q = %d", username, resp.StatusCode)
	}
	login := decodeJSON[struct {
		Token string `json:"token"`
	}](t, resp)
	return login.Token
}

func getUserByUsername(t *testing.T, srv *server.Server, username string) *users.User {
	t.Helper()
	conn := db.Wrap("sqlite", srv.DB)
	u, err := users.NewSQLRepository(conn).GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("GetUserByUsername(%q): %v", username, err)
	}
	return u
}
