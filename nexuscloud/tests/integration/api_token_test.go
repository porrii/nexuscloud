package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Tokens de API (§78, ADR-037): acceso todo-o-nada a la API REST completa,
// creado/gestionado por el propio usuario ya autenticado con una sesión
// normal. A diferencia de las credenciales WebAuthn, el token resultante SÍ
// sirve por sí mismo como Bearer contra cualquier endpoint protegido -- es
// lo que se comprueba aquí además del CRUD.

func TestAPITokenCreateListAndUseAsBearer(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	sessionToken := loginAndGetToken(t, c, "admin", "contraseña-admin-segura")

	resp := c.do(http.MethodPost, "/api/v1/auth/api-tokens", map[string]string{"label": "script de backup"}, sessionToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear token: status = %d", resp.StatusCode)
	}
	created := decodeJSON[struct {
		ID        string  `json:"id"`
		Label     string  `json:"label"`
		Token     string  `json:"token"`
		ExpiresAt *string `json:"expires_at"`
	}](t, resp)
	if !strings.HasPrefix(created.Token, "nat_") {
		t.Errorf("token = %q, esperado que empiece por 'nat_'", created.Token)
	}
	if created.Label != "script de backup" {
		t.Errorf("label = %q", created.Label)
	}
	if created.ExpiresAt != nil {
		t.Errorf("expires_at = %v, esperado ausente (nunca expira)", *created.ExpiresAt)
	}

	// El propio token de API, usado como Bearer, autentica una petición
	// normal -- no solo la sesión que lo creó.
	resp = c.do(http.MethodGet, "/api/v1/users/me", nil, created.Token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("usar el token de API como Bearer: status = %d", resp.StatusCode)
	}
	me := decodeJSON[struct {
		Username string `json:"username"`
	}](t, resp)
	if me.Username != "admin" {
		t.Errorf("Me con token de API = %q, esperado admin", me.Username)
	}

	// El listado nunca expone el valor en claro (§172): ni siquiera la
	// clave "token" debería estar presente.
	resp = c.do(http.MethodGet, "/api/v1/auth/api-tokens", nil, sessionToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("listar: status = %d", resp.StatusCode)
	}
	list := decodeJSON[[]map[string]any](t, resp)
	if len(list) != 1 || list[0]["id"] != created.ID {
		t.Fatalf("list = %+v", list)
	}
	if _, exposed := list[0]["token"]; exposed {
		t.Error("el listado no debe exponer el valor en claro del token")
	}
}

func TestAPITokenRevokeRequiresOwnerAndDisablesIt(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	adminToken := loginAndGetToken(t, c, "admin", "contraseña-admin-segura")

	resp := c.do(http.MethodPost, "/api/v1/auth/api-tokens", map[string]string{"label": "mi token"}, adminToken)
	created := decodeJSON[struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}](t, resp)

	// IDOR: otro usuario no puede revocar el token del admin.
	createAdmin(t, srv, "otro", "contraseña-otro-usuario")
	otroToken := loginAndGetToken(t, c, "otro", "contraseña-otro-usuario")
	resp = c.do(http.MethodDelete, "/api/v1/auth/api-tokens/"+created.ID, nil, otroToken)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("revocar con otro usuario: status = %d, esperado 404", resp.StatusCode)
	}
	// El token sigue funcionando: el intento fallido no lo tocó.
	resp = c.do(http.MethodGet, "/api/v1/users/me", nil, created.Token)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("tras el intento fallido, el token debería seguir autenticando: status = %d", resp.StatusCode)
	}

	resp = c.do(http.MethodDelete, "/api/v1/auth/api-tokens/"+created.ID, nil, adminToken)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revocar con el propietario: status = %d", resp.StatusCode)
	}

	resp = c.do(http.MethodGet, "/api/v1/users/me", nil, created.Token)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("tras revocar, usar el token = %d, esperado 401", resp.StatusCode)
	}
}

func TestAPITokenCreateRejectsTooLongLabel(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	token := loginAndGetToken(t, c, "admin", "contraseña-admin-segura")

	resp := c.do(http.MethodPost, "/api/v1/auth/api-tokens", map[string]string{"label": strings.Repeat("a", 101)}, token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("label de 101 runas: status = %d, esperado 400", resp.StatusCode)
	}
}

func TestAPITokenExpiredTokenIsRejected(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	sessionToken := loginAndGetToken(t, c, "admin", "contraseña-admin-segura")

	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	resp := c.do(http.MethodPost, "/api/v1/auth/api-tokens", map[string]string{"label": "ya expirado", "expires_at": past}, sessionToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear con expiración pasada: status = %d", resp.StatusCode)
	}
	created := decodeJSON[struct {
		Token string `json:"token"`
	}](t, resp)

	resp = c.do(http.MethodGet, "/api/v1/users/me", nil, created.Token)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("token ya expirado al crearlo: status = %d, esperado 401", resp.StatusCode)
	}
}

func TestAPITokenRequiresAuthToCreate(t *testing.T) {
	ts, _ := newTestServer(t)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	resp := c.do(http.MethodPost, "/api/v1/auth/api-tokens", map[string]string{"label": "x"}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("crear sin sesión: status = %d, esperado 401", resp.StatusCode)
	}
}
