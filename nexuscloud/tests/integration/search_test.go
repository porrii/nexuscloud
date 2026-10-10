// Tests de integración de la búsqueda de metadatos (§33): GET /search
// (personal) y GET /admin/search (cruzando usuarios), por HTTP real sobre
// el servidor completo.
package integration

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/server"
)

// TestMeReportsIsAdmin cubre is_admin en GET /users/me -- lo añadió esta
// misma fase para que la web decida si mostrar la búsqueda entre usuarios
// (§33); antes no existía ningún campo de rol en esa respuesta.
func TestMeReportsIsAdmin(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "duena", "contrasena-larga-123")
	_, member := createMemberUser(t, srv, c, admin, "miembro", "contrasena-larga-456")

	adminMe := decodeJSON[struct {
		IsAdmin bool `json:"is_admin"`
	}](t, c.do(http.MethodGet, "/api/v1/users/me", nil, admin))
	if !adminMe.IsAdmin {
		t.Error("GET /users/me del admin debería reportar is_admin=true")
	}

	memberMe := decodeJSON[struct {
		IsAdmin bool `json:"is_admin"`
	}](t, c.do(http.MethodGet, "/api/v1/users/me", nil, member))
	if memberMe.IsAdmin {
		t.Error("GET /users/me de un usuario normal debería reportar is_admin=false")
	}
}

func TestSearchFindsOwnFilesByNameAcrossFolders(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")

	resp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Contratos"}, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear /Contratos = %d", resp.StatusCode)
	}
	uploadTestFile(t, c, owner, "/", "informe-general.pdf", "contenido")
	uploadTestFile(t, c, owner, "/Contratos", "informe-2026.pdf", "contenido")
	uploadTestFile(t, c, owner, "/", "foto.jpg", "contenido")

	resp = c.do(http.MethodGet, "/api/v1/search?q=informe", nil, owner)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /search = %d, cuerpo = %s", resp.StatusCode, body)
	}
	out := decodeJSON[listResponseDTO](t, resp)
	if len(out.Files) != 2 {
		t.Errorf("GET /search?q=informe = %d archivos, esperados 2 (en carpetas distintas): %+v", len(out.Files), out.Files)
	}
}

func TestSearchByTypeAndExtension(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")
	uploadTestFile(t, c, owner, "/", "a.pdf", "contenido")
	uploadTestFile(t, c, owner, "/", "b.jpg", "contenido")

	resp := c.do(http.MethodGet, "/api/v1/search?ext=pdf", nil, owner)
	out := decodeJSON[listResponseDTO](t, resp)
	if len(out.Files) != 1 || out.Files[0].ID == "" {
		t.Errorf("GET /search?ext=pdf = %+v, esperado solo a.pdf", out.Files)
	}
}

func TestSearchAnnotatesFavoriteID(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")
	f := uploadTestFile(t, c, owner, "/", "favorito.txt", "contenido")

	resp := c.do(http.MethodPost, "/api/v1/favorites", map[string]string{"resource_type": "file", "resource_id": f.ID}, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear favorito = %d", resp.StatusCode)
	}

	resp = c.do(http.MethodGet, "/api/v1/search?q=favorito", nil, owner)
	out := decodeJSON[struct {
		Files []struct {
			ID         string  `json:"id"`
			FavoriteID *string `json:"favorite_id"`
		} `json:"files"`
	}](t, resp)
	if len(out.Files) != 1 || out.Files[0].FavoriteID == nil {
		t.Errorf("GET /search debería anotar favorite_id igual que GET /files: %+v", out.Files)
	}
}

func TestSearchRequiresSession(t *testing.T) {
	ts, _ := newTestServer(t)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/search", nil, ""), http.StatusUnauthorized, "")
}

func TestSearchIsIsolatedByOwner(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "duena", "contrasena-larga-123")
	_, member := createMemberUser(t, srv, c, admin, "miembro", "contrasena-larga-456")
	uploadTestFile(t, c, admin, "/", "solo-de-la-duena.txt", "contenido")

	resp := c.do(http.MethodGet, "/api/v1/search?q=duena", nil, member)
	out := decodeJSON[listResponseDTO](t, resp)
	if len(out.Files) != 0 {
		t.Errorf("un miembro no debería ver archivos de otro usuario por /search: %+v", out.Files)
	}
}

func TestAdminSearchCrossesUsersAndRequiresAdminRole(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "duena", "contrasena-larga-123")
	_, member := createMemberUser(t, srv, c, admin, "miembro", "contrasena-larga-456")
	uploadTestFile(t, c, admin, "/", "de-la-duena.txt", "contenido")
	uploadTestFile(t, c, member, "/", "del-miembro.txt", "contenido")

	// Un usuario normal no puede usar /admin/search.
	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/admin/search?q=txt", nil, member), http.StatusForbidden, "")

	// El admin ve de todos sin filtrar por owner.
	resp := c.do(http.MethodGet, "/api/v1/admin/search?q=txt", nil, admin)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /admin/search = %d, cuerpo = %s", resp.StatusCode, body)
	}
	out := decodeJSON[listResponseDTO](t, resp)
	if len(out.Files) != 2 {
		t.Errorf("GET /admin/search sin owner = %d archivos, esperados 2 (de ambos usuarios): %+v", len(out.Files), out.Files)
	}

	// Filtrando por owner=miembro solo ve lo suyo.
	resp = c.do(http.MethodGet, "/api/v1/admin/search?q=txt&owner=miembro", nil, admin)
	out = decodeJSON[listResponseDTO](t, resp)
	if len(out.Files) != 1 {
		t.Errorf("GET /admin/search?owner=miembro = %+v, esperado solo lo del miembro", out.Files)
	}
}

func TestSearchRejectsMalformedDateAndSizeParams(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")

	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/search?date_from=no-es-una-fecha", nil, owner), http.StatusBadRequest, "invalid_request")
	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/search?size_min=no-es-un-numero", nil, owner), http.StatusBadRequest, "invalid_request")
}

// newSearchDisabledTestServer arranca el servidor con search.enabled=false
// -- la única variante de este paquete que necesita desactivarlo (viene
// activado por defecto, a diferencia de sharing.publicLinksEnabled/
// webdav.enabled).
func newSearchDisabledTestServer(t *testing.T) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true
	cfg.Search.Enabled = false

	srv, err := server.Build(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("server.Build falló: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, srv
}

func TestSearchDisabledByConfigDoesNotRegisterRoutes(t *testing.T) {
	ts, srv := newSearchDisabledTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")

	if resp := c.do(http.MethodGet, "/api/v1/search?q=x", nil, owner); resp.StatusCode != http.StatusNotFound {
		t.Errorf("con search.enabled=false, GET /search = %d, esperado 404 (ruta ni registrada)", resp.StatusCode)
	}
	if resp := c.do(http.MethodGet, "/api/v1/admin/search?q=x", nil, owner); resp.StatusCode != http.StatusNotFound {
		t.Errorf("con search.enabled=false, GET /admin/search = %d, esperado 404 (ruta ni registrada)", resp.StatusCode)
	}
}
