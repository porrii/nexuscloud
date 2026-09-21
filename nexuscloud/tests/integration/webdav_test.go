package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/server"
	"github.com/porrii/nexuscloud/internal/users"
	"github.com/porrii/nexuscloud/internal/webdav"
)

// newWebDAVTestServer arranca el mismo servidor real que newTestServer
// (e2e_test.go) con webdav.enabled=true -- por defecto está desactivado
// (§3, §169), así que la ruta ni existe a menos que se pida aquí. mutate
// permite ajustar la configuración por test.
func newWebDAVTestServer(t *testing.T, mutate func(*config.Config)) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true
	cfg.WebDAV = config.WebDAVConfig{Enabled: true, Path: "/webdav"}
	if mutate != nil {
		mutate(cfg)
	}

	srv, err := server.Build(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("server.Build falló: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, srv
}

// davToken crea un token de acceso WebDAV para el usuario, directamente sobre
// la base de datos del servidor (mismo criterio que createAdmin en
// e2e_test.go: la gestión de tokens por API/CLI tiene sus propios tests).
func davToken(t *testing.T, srv *server.Server, username string) string {
	t.Helper()
	conn := db.Wrap("sqlite", srv.DB)
	svc := webdav.NewTokenService(webdav.NewSQLTokenRepository(conn), users.NewSQLRepository(conn), nil)
	u := getUserByUsername(t, srv, username)
	_, plain, err := svc.Create(context.Background(), u.ID, "prueba de integración")
	if err != nil {
		t.Fatalf("creando token WebDAV: %v", err)
	}
	return plain
}

type davClient struct {
	t           *testing.T
	base        string
	user, token string
	client      *http.Client
}

func (c *davClient) do(method, path, body string, headers map[string]string) *http.Response {
	c.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("NewRequest: %v", err)
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s falló: %v", method, path, err)
	}
	return resp
}

func bodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("leyendo el cuerpo: %v", err)
	}
	return string(b)
}

func wantStatus(t *testing.T, resp *http.Response, want int, what string) {
	t.Helper()
	if resp.StatusCode != want {
		t.Errorf("%s: status = %d, esperado %d", what, resp.StatusCode, want)
	}
	resp.Body.Close()
}

func TestWebDAVIsAbsentWhenDisabled(t *testing.T) {
	ts, srv := newTestServer(t) // webdav.enabled=false, por defecto
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &davClient{t: t, base: ts.URL, user: "admin", token: "nwd_da-igual", client: ts.Client()}

	wantStatus(t, c.do("PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"}), http.StatusNotFound, "PROPFIND con WebDAV desactivado")
	wantStatus(t, c.do("OPTIONS", "/webdav/", "", nil), http.StatusNotFound, "OPTIONS con WebDAV desactivado")
}

// Sin chi.RegisterMethod, el router respondería 405 a PROPFIND/MKCOL/MOVE...
// antes de llegar siquiera al handler: este test recorre el router REAL.
func TestWebDAVMethodsReachTheHandlerThroughTheRealRouter(t *testing.T) {
	ts, srv := newWebDAVTestServer(t, nil)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &davClient{t: t, base: ts.URL, user: "admin", token: davToken(t, srv, "admin"), client: ts.Client()}

	anonymous := &davClient{t: t, base: ts.URL, client: ts.Client()}
	resp := anonymous.do("PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"})
	if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic ") {
		t.Errorf("sin credenciales: status %d, WWW-Authenticate %q; esperado 401 + Basic", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	resp.Body.Close()

	// La contraseña de la cuenta no vale por WebDAV (ADR-034): saltaría el 2FA.
	byPassword := &davClient{t: t, base: ts.URL, user: "admin", token: "contraseña-admin-segura", client: ts.Client()}
	wantStatus(t, byPassword.do("PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"}), http.StatusUnauthorized, "contraseña de la cuenta por Basic")

	for method, want := range map[string]int{"PROPFIND": 207, "MKCOL": 201, "OPTIONS": 200} {
		path := "/webdav/"
		if method == "MKCOL" {
			path = "/webdav/docs"
		}
		wantStatus(t, c.do(method, path, "", map[string]string{"Depth": "0"}), want, method+" por el router real")
	}
	wantStatus(t, c.do("MOVE", "/webdav/docs", "", map[string]string{"Destination": ts.URL + "/webdav/documentos"}), http.StatusCreated, "MOVE por el router real")
	wantStatus(t, c.do("DELETE", "/webdav/documentos", "", nil), http.StatusNoContent, "DELETE por el router real")
}

// WebDAV y la API REST son dos puertas al MISMO almacenamiento: papelera,
// versionado y auditoría idénticos, no una vía paralela al disco.
func TestWebDAVAndRESTShareTheSameStorageRules(t *testing.T) {
	ts, srv := newWebDAVTestServer(t, nil)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	dav := &davClient{t: t, base: ts.URL, user: "admin", token: davToken(t, srv, "admin"), client: ts.Client()}
	rest := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	restToken := loginAndGetToken(t, rest, "admin", "contraseña-admin-segura")

	list := func(path string) listResponseDTO {
		resp := rest.do(http.MethodGet, "/api/v1/files?path="+path, nil, restToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /files?path=%s = %d", path, resp.StatusCode)
		}
		return decodeJSON[listResponseDTO](t, resp)
	}

	// Lo subido por WebDAV aparece por la API, con su contenido...
	wantStatus(t, dav.do("PUT", "/webdav/informe.txt", "v1", nil), http.StatusCreated, "PUT v1")
	wantStatus(t, dav.do("PUT", "/webdav/informe.txt", "v2", nil), http.StatusCreated, "PUT v2")
	files := list("/").Files
	if len(files) != 1 {
		t.Fatalf("archivos vistos por la API = %d, esperado 1 (informe.txt)", len(files))
	}
	id := files[0].ID
	resp := rest.do(http.MethodGet, "/api/v1/files/"+id, nil, restToken)
	if got := bodyString(t, resp); got != "v2" {
		t.Errorf("contenido visto por la API = %q, esperado v2", got)
	}
	// ...y el segundo PUT dejó la v1 en el historial (mismo versionado).
	resp = rest.do(http.MethodGet, "/api/v1/files/"+id+"/versions", nil, restToken)
	if versions := decodeJSON[[]struct {
		VersionNum int `json:"version_num"`
	}](t, resp); len(versions) != 1 {
		t.Errorf("versiones vistas por la API = %d, esperada 1 (la v1 desplazada por el segundo PUT)", len(versions))
	}

	// Lo subido por la API aparece por WebDAV.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/files?name=desde-api.txt&path=/", strings.NewReader("subido por REST"))
	req.Header.Set("Authorization", "Bearer "+restToken)
	upResp, err := ts.Client().Do(req)
	if err != nil || upResp.StatusCode != http.StatusCreated {
		t.Fatalf("subida por la API falló: %v %v", err, upResp)
	}
	upResp.Body.Close()
	if got := bodyString(t, dav.do("GET", "/webdav/desde-api.txt", "", nil)); got != "subido por REST" {
		t.Errorf("contenido visto por WebDAV = %q", got)
	}

	// DELETE por WebDAV va a la papelera, igual que por la API.
	wantStatus(t, dav.do("DELETE", "/webdav/informe.txt", "", nil), http.StatusNoContent, "DELETE")
	if names := list("/").Files; len(names) != 1 {
		t.Errorf("tras el DELETE la API ve %d archivos activos, esperado 1", len(names))
	}
	resp = rest.do(http.MethodGet, "/api/v1/trash", nil, restToken)
	if trash := decodeJSON[listResponseDTO](t, resp); len(trash.Files) != 1 || trash.Files[0].ID != id {
		t.Errorf("papelera vista por la API = %+v, esperado el informe.txt borrado por WebDAV", trash.Files)
	}
}

// Un MOVE con Overwrite:T sobre un archivo reemplaza su contenido y deja el
// anterior como versión, y uno que no puede completarse (carpeta sobre
// carpeta con la papelera activa) falla SIN tocar nada: antes el destino
// acababa en la papelera aunque el cliente recibiera un 403. Todo contrastado
// por la API REST, que es la otra puerta al mismo almacenamiento.
func TestWebDAVOverwriteKeepsHistoryAndAFailedOneNeverLosesTheTarget(t *testing.T) {
	ts, srv := newWebDAVTestServer(t, nil)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	dav := &davClient{t: t, base: ts.URL, user: "admin", token: davToken(t, srv, "admin"), client: ts.Client()}
	rest := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	restToken := loginAndGetToken(t, rest, "admin", "contraseña-admin-segura")

	type entry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type listing struct {
		Directories []entry `json:"directories"`
		Files       []entry `json:"files"`
	}
	names := func(es []entry) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.Name)
		}
		sort.Strings(out)
		return out
	}
	active := func() listing {
		return decodeJSON[listing](t, rest.do(http.MethodGet, "/api/v1/files?path=/", nil, restToken))
	}
	trash := func() listing {
		return decodeJSON[listing](t, rest.do(http.MethodGet, "/api/v1/trash", nil, restToken))
	}
	overwrite := func(method, from, to string) *http.Response {
		return dav.do(method, from, "", map[string]string{"Destination": ts.URL + to, "Overwrite": "T"})
	}

	// Archivo sobre archivo: el destino toma el contenido, con historial.
	wantStatus(t, dav.do("PUT", "/webdav/x.txt", "nuevo", nil), http.StatusCreated, "PUT x")
	wantStatus(t, dav.do("PUT", "/webdav/y.txt", "viejo", nil), http.StatusCreated, "PUT y")
	wantStatus(t, overwrite("MOVE", "/webdav/x.txt", "/webdav/y.txt"), http.StatusNoContent, "MOVE con Overwrite:T")
	if got := bodyString(t, dav.do("GET", "/webdav/y.txt", "", nil)); got != "nuevo" {
		t.Errorf("y.txt = %q, esperado %q", got, "nuevo")
	}
	files := active().Files
	if got := names(files); len(got) != 1 || got[0] != "y.txt" {
		t.Fatalf("archivos activos vistos por la API = %v, esperado solo y.txt", got)
	}
	versions := decodeJSON[[]struct {
		VersionNum int `json:"version_num"`
	}](t, rest.do(http.MethodGet, "/api/v1/files/"+files[0].ID+"/versions", nil, restToken))
	if len(versions) != 1 {
		t.Errorf("versiones de y.txt = %d, esperada 1 (el contenido anterior)", len(versions))
	}
	if got := names(trash().Files); len(got) != 1 || got[0] != "x.txt" {
		t.Errorf("papelera = %v, esperado solo el origen x.txt (el destino nunca debe ir a la papelera)", got)
	}

	// Carpeta sobre carpeta: rechazado, y nada cambia.
	wantStatus(t, dav.do("MKCOL", "/webdav/a", "", nil), http.StatusCreated, "MKCOL a")
	wantStatus(t, dav.do("MKCOL", "/webdav/b", "", nil), http.StatusCreated, "MKCOL b")
	wantStatus(t, dav.do("PUT", "/webdav/b/g.txt", "b1", nil), http.StatusCreated, "PUT b/g")
	wantStatus(t, overwrite("MOVE", "/webdav/a", "/webdav/b"), http.StatusForbidden, "MOVE de carpeta sobre carpeta")
	wantStatus(t, overwrite("COPY", "/webdav/a", "/webdav/b"), http.StatusForbidden, "COPY de carpeta sobre carpeta")
	if got := bodyString(t, dav.do("GET", "/webdav/b/g.txt", "", nil)); got != "b1" {
		t.Errorf("b/g.txt = %q tras los intentos fallidos, esperado %q", got, "b1")
	}
	if got := names(active().Directories); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("carpetas activas = %v, esperado a y b intactas", got)
	}
	if got := names(trash().Directories); len(got) != 0 {
		t.Errorf("carpetas en la papelera = %v: un overwrite fallido no debe borrar nada", got)
	}
}

func TestWebDAVReadOnlyConfigBlocksWrites(t *testing.T) {
	ts, srv := newWebDAVTestServer(t, func(c *config.Config) { c.WebDAV.ReadOnly = true })
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &davClient{t: t, base: ts.URL, user: "admin", token: davToken(t, srv, "admin"), client: ts.Client()}

	wantStatus(t, c.do("PUT", "/webdav/x.txt", "x", nil), http.StatusForbidden, "PUT en solo lectura")
	wantStatus(t, c.do("MKCOL", "/webdav/d", "", nil), http.StatusForbidden, "MKCOL en solo lectura")
	wantStatus(t, c.do("PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"}), http.StatusMultiStatus, "PROPFIND en solo lectura")
}

func TestWebDAVCustomPath(t *testing.T) {
	ts, srv := newWebDAVTestServer(t, func(c *config.Config) { c.WebDAV.Path = "/dav" })
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &davClient{t: t, base: ts.URL, user: "admin", token: davToken(t, srv, "admin"), client: ts.Client()}

	wantStatus(t, c.do("PROPFIND", "/dav/", "", map[string]string{"Depth": "0"}), http.StatusMultiStatus, "PROPFIND en /dav")
	wantStatus(t, c.do("PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"}), http.StatusNotFound, "PROPFIND en la ruta por defecto")

	// La API de tokens anuncia la ruta configurada, no la de por defecto: es lo
	// que la interfaz web muestra como URL del servidor.
	rest := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	session := loginAndGetToken(t, rest, "admin", "contraseña-admin-segura")
	resp := rest.do(http.MethodPost, "/api/v1/auth/webdav/tokens", map[string]string{"label": "x"}, session)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear token: status = %d", resp.StatusCode)
	}
	created := decodeJSON[struct {
		WebDAVPath string `json:"webdav_path"`
	}](t, resp)
	if created.WebDAVPath != "/dav" {
		t.Errorf("webdav_path = %q, esperado %q", created.WebDAVPath, "/dav")
	}
}

func TestWebDAVHasItsOwnRateLimit(t *testing.T) {
	ts, srv := newWebDAVTestServer(t, func(c *config.Config) { c.Security.RateLimit.WebDAVPerMinute = 5 })
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &davClient{t: t, base: ts.URL, user: "admin", token: davToken(t, srv, "admin"), client: ts.Client()}

	var limited int
	for i := 0; i < 12; i++ {
		resp := c.do("OPTIONS", "/webdav/", "", nil)
		if resp.StatusCode == http.StatusTooManyRequests {
			limited++
		} else if i < 5 && resp.StatusCode != http.StatusOK {
			t.Errorf("petición %d dentro del límite: status %d", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}
	if limited == 0 {
		t.Error("ninguna de las 12 peticiones seguidas recibió 429 con webdavPerMinute=5")
	}
	// La API REST no comparte cubo con WebDAV.
	rest := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	if token := loginAndGetToken(t, rest, "admin", "contraseña-admin-segura"); token == "" {
		t.Error("la API debería seguir respondiendo aunque WebDAV esté limitado")
	}
}

func TestWebDAVOperationsAreAuditedThroughTheRealServer(t *testing.T) {
	ts, srv := newWebDAVTestServer(t, nil)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &davClient{t: t, base: ts.URL, user: "admin", token: davToken(t, srv, "admin"), client: ts.Client()}
	bad := &davClient{t: t, base: ts.URL, user: "admin", token: "nwd_incorrecto", client: ts.Client()}

	wantStatus(t, c.do("PUT", "/webdav/a.txt", "hola", nil), http.StatusCreated, "PUT")
	wantStatus(t, c.do("GET", "/webdav/a.txt", "", nil), http.StatusOK, "GET")
	wantStatus(t, c.do("DELETE", "/webdav/a.txt", "", nil), http.StatusNoContent, "DELETE")
	wantStatus(t, bad.do("GET", "/webdav/a.txt", "", nil), http.StatusUnauthorized, "GET con token incorrecto")

	events, err := audit.NewSQLRepository(db.Wrap("sqlite", srv.DB)).ListEvents(context.Background(), 50, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	seen := map[string]bool{}
	for _, ev := range events {
		if ev.Metadata["via"] == "webdav" || ev.EventType == audit.EventWebDAVAuthFailed {
			seen[ev.EventType] = true
		}
	}
	for _, want := range []string{audit.EventUpload, audit.EventDownload, audit.EventDelete, audit.EventWebDAVAuthFailed} {
		if !seen[want] {
			b, _ := json.Marshal(seen)
			t.Errorf("falta el evento %q en la auditoría (vistos: %s)", want, b)
		}
	}
}

func TestWebDAVTokenRoutesAreAbsentWhenDisabled(t *testing.T) {
	ts, srv := newTestServer(t) // webdav.enabled=false, por defecto
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	rest := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	token := loginAndGetToken(t, rest, "admin", "contraseña-admin-segura")

	resp := rest.do(http.MethodPost, "/api/v1/auth/webdav/tokens", map[string]string{"label": "x"}, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404 (ruta no registrada con WebDAV desactivado)", resp.StatusCode)
	}
}

func TestWebDAVTokenManagementThroughTheAPI(t *testing.T) {
	ts, srv := newWebDAVTestServer(t, nil)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	createAdmin(t, srv, "otro", "contraseña-otro-usuario")
	rest := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	adminToken := loginAndGetToken(t, rest, "admin", "contraseña-admin-segura")
	otroToken := loginAndGetToken(t, rest, "otro", "contraseña-otro-usuario")

	if resp := rest.do(http.MethodPost, "/api/v1/auth/webdav/tokens", nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("crear sin sesión: status = %d, esperado 401", resp.StatusCode)
	}
	resp := rest.do(http.MethodPost, "/api/v1/auth/webdav/tokens", map[string]string{"label": strings.Repeat("x", 101)}, adminToken)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("label demasiado larga: status = %d, esperado 400", resp.StatusCode)
	}

	resp = rest.do(http.MethodPost, "/api/v1/auth/webdav/tokens", map[string]string{"label": "portátil"}, adminToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear token: status = %d", resp.StatusCode)
	}
	created := decodeJSON[struct {
		ID         string `json:"id"`
		Label      string `json:"label"`
		Token      string `json:"token"`
		WebDAVPath string `json:"webdav_path"`
	}](t, resp)
	if !strings.HasPrefix(created.Token, "nwd_") || created.Label != "portátil" {
		t.Fatalf("respuesta de creación = %+v", created)
	}
	if created.WebDAVPath != "/webdav" {
		t.Errorf("webdav_path = %q, esperado %q (la interfaz lo usa para mostrar la URL)", created.WebDAVPath, "/webdav")
	}

	// El token creado por la API sirve de verdad contra WebDAV.
	dav := &davClient{t: t, base: ts.URL, user: "admin", token: created.Token, client: ts.Client()}
	wantStatus(t, dav.do("PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"}), http.StatusMultiStatus, "WebDAV con el token recién creado")

	// list nunca vuelve a mostrar el secreto ni su hash (§172).
	resp = rest.do(http.MethodGet, "/api/v1/auth/webdav/tokens", nil, adminToken)
	var listed []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decodificando list: %v", err)
	}
	resp.Body.Close()
	if len(listed) != 1 || listed[0]["id"] != created.ID || listed[0]["last_used_at"] == nil {
		t.Errorf("list = %+v, esperado 1 token con last_used_at puesto por el PROPFIND", listed)
	}
	for _, forbidden := range []string{"token", "token_hash", "TokenHash"} {
		if _, leaked := listed[0][forbidden]; leaked {
			t.Errorf("list expone el campo %q", forbidden)
		}
	}

	// IDOR: otro usuario autenticado no puede revocar el token del admin.
	resp = rest.do(http.MethodDelete, "/api/v1/auth/webdav/tokens/"+created.ID, nil, otroToken)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("revocar con otro usuario: status = %d, esperado 404", resp.StatusCode)
	}
	wantStatus(t, dav.do("PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"}), http.StatusMultiStatus, "el token sigue valiendo tras el intento ajeno")

	resp = rest.do(http.MethodDelete, "/api/v1/auth/webdav/tokens/"+created.ID, nil, adminToken)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revocar con el propietario: status = %d", resp.StatusCode)
	}
	wantStatus(t, dav.do("PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"}), http.StatusUnauthorized, "WebDAV con el token revocado")

	events, err := audit.NewSQLRepository(db.Wrap("sqlite", srv.DB)).ListEvents(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	seen := map[string]bool{}
	for _, ev := range events {
		seen[ev.EventType] = true
	}
	for _, want := range []string{audit.EventWebDAVTokenCreated, audit.EventWebDAVTokenRevoked} {
		if !seen[want] {
			t.Errorf("falta el evento de auditoría %q", want)
		}
	}
}
