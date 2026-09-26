// Tests de integración de la subida anónima (§38, ADR-039): enlaces de
// subida sin ningún acceso al resto del contenido, por HTTP real sobre el
// servidor completo (router, middleware, handlers, servicios y
// repositorios).
package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/server"
)

// newAnonymousUploadTestServer es newTestServer con la subida anónima
// activada (viene desactivada por defecto, §3/§47) y su propio cubo de rate
// limit generoso -- los tests de límite propio lo ajustan aparte con mutate.
func newAnonymousUploadTestServer(t *testing.T, mutate func(*config.Config)) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.Security.RateLimit.AnonymousUploadPerMinute = 1000
	cfg.API.Enabled = true
	cfg.Sharing.AnonymousUploadEnabled = true
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

type anonymousUploadDTO struct {
	ID                 string `json:"id"`
	DirectoryID        string `json:"directory_id"`
	DirectoryName      string `json:"directory_name"`
	Label              string `json:"label"`
	MaxUploadSizeBytes *int64 `json:"max_upload_size_bytes"`
	UploadCount        int    `json:"upload_count"`
	Token              string `json:"token"`
}

type anonymousUploadInfoDTO struct {
	Label              string `json:"label"`
	MaxUploadSizeBytes *int64 `json:"max_upload_size_bytes"`
}

// anonymousUpload sube contenido en crudo sin ninguna cabecera de sesión --
// es la superficie pública, la misma forma que uploadTestFile usa para la
// autenticada pero sin Authorization.
func anonymousUpload(t *testing.T, ts *httptest.Server, token, name, content string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		ts.URL+"/api/v1/public/anonymous-uploads/"+token+"/upload?name="+url.QueryEscape(name), strings.NewReader(content))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("subiendo %s vía enlace anónimo: %v", name, err)
	}
	return resp
}

func TestAnonymousUploadFullFlow(t *testing.T) {
	ts, srv := newAnonymousUploadTestServer(t, nil)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")

	resp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Buzon"}, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear /Buzon = %d", resp.StatusCode)
	}
	dir := decodeJSON[directoryDTO](t, resp)

	// Crear (autenticado): el token en claro solo viene en esta respuesta.
	resp = c.do(http.MethodPost, "/api/v1/anonymous-uploads", map[string]any{
		"directory_id": dir.ID, "label": "Fotos de la boda",
	}, owner)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("crear el enlace = %d, cuerpo = %s", resp.StatusCode, body)
	}
	created := decodeJSON[anonymousUploadDTO](t, resp)
	if created.Token == "" {
		t.Fatal("la respuesta de creación debería incluir el token en claro")
	}
	if created.DirectoryName != "Buzon" {
		t.Errorf("directory_name = %q, esperado Buzon", created.DirectoryName)
	}

	// Probe público (sin sesión): solo label y límite, nunca propietario/carpeta.
	probeResp, err := ts.Client().Get(ts.URL + "/api/v1/public/anonymous-uploads/" + created.Token)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if probeResp.StatusCode != http.StatusOK {
		t.Fatalf("probe = %d", probeResp.StatusCode)
	}
	info := decodeJSON[anonymousUploadInfoDTO](t, probeResp)
	if info.Label != "Fotos de la boda" {
		t.Errorf("probe label = %q, esperado %q", info.Label, "Fotos de la boda")
	}

	// Subida pública (sin sesión).
	upResp := anonymousUpload(t, ts, created.Token, "regalo.jpg", "contenido del regalo")
	defer upResp.Body.Close()
	if upResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(upResp.Body)
		t.Fatalf("subir vía enlace anónimo = %d, cuerpo = %s", upResp.StatusCode, body)
	}
	uploaded := decodeJSON[fileDTO](t, upResp)
	if uploaded.ParentPath != "/Buzon" || uploaded.Name != "regalo.jpg" {
		t.Errorf("archivo subido = %+v", uploaded)
	}
	// El archivo pertenece a la dueña: lo ve en su propio listado, no en el
	// del subidor anónimo (que no existe como cuenta).
	dl := c.do(http.MethodGet, "/api/v1/files/"+uploaded.ID, nil, owner)
	defer dl.Body.Close()
	if got, _ := io.ReadAll(dl.Body); string(got) != "contenido del regalo" {
		t.Errorf("contenido tras la subida anónima = %q", got)
	}

	// Listar (autenticado): upload_count refleja la subida, sin token.
	resp = c.do(http.MethodGet, "/api/v1/anonymous-uploads", nil, owner)
	list := decodeJSON[[]anonymousUploadDTO](t, resp)
	if len(list) != 1 || list[0].UploadCount != 1 || list[0].Token != "" {
		t.Errorf("listado tras subir = %+v, esperado 1 enlace con upload_count=1 y sin token", list)
	}

	// Auditoría: alta del enlace y la subida en sí con via=anonymous_upload.
	events, err := audit.NewSQLRepository(db.Wrap("sqlite", srv.DB)).ListEvents(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	var createdEvent, uploadEvent *audit.Event
	for _, ev := range events {
		if ev.EventType == audit.EventAnonymousUploadLinkCreated && ev.TargetID == created.ID {
			createdEvent = ev
		}
		if ev.EventType == audit.EventUpload && ev.TargetID == uploaded.ID {
			uploadEvent = ev
		}
	}
	if createdEvent == nil {
		t.Error("no se encontró el evento de auditoría anonymous_upload_link_created")
	}
	if uploadEvent == nil || uploadEvent.Metadata["via"] != "anonymous_upload" || uploadEvent.ActorUserID != "" {
		t.Errorf("evento de subida = %+v, esperado via=anonymous_upload y sin actor (subida sin sesión)", uploadEvent)
	}

	// Revocar (autenticado): a partir de aquí, un único 404 genérico tanto
	// en el probe como en la subida -- quien solo tiene el token no necesita
	// distinguir el motivo.
	resp = c.do(http.MethodDelete, "/api/v1/anonymous-uploads/"+created.ID, nil, owner)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revocar = %d", resp.StatusCode)
	}
	revokedProbe, err := ts.Client().Get(ts.URL + "/api/v1/public/anonymous-uploads/" + created.Token)
	if err != nil {
		t.Fatalf("probe tras revocar: %v", err)
	}
	wantErrorCode(t, revokedProbe, http.StatusNotFound, "not_found")
	wantErrorCode(t, anonymousUpload(t, ts, created.Token, "tarde.jpg", "ya revocado"), http.StatusNotFound, "not_found")

	events, err = audit.NewSQLRepository(db.Wrap("sqlite", srv.DB)).ListEvents(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	var revokedEvent *audit.Event
	for _, ev := range events {
		if ev.EventType == audit.EventAnonymousUploadLinkRevoked && ev.TargetID == created.ID {
			revokedEvent = ev
		}
	}
	if revokedEvent == nil {
		t.Error("no se encontró el evento de auditoría anonymous_upload_link_revoked")
	}
}

func TestAnonymousUploadManagementRoutesRequireSession(t *testing.T) {
	ts, _ := newAnonymousUploadTestServer(t, nil)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	wantErrorCode(t, c.do(http.MethodPost, "/api/v1/anonymous-uploads", map[string]string{"directory_id": "x"}, ""), http.StatusUnauthorized, "")
	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/anonymous-uploads", nil, ""), http.StatusUnauthorized, "")
	wantErrorCode(t, c.do(http.MethodDelete, "/api/v1/anonymous-uploads/no-existe", nil, ""), http.StatusUnauthorized, "")
}

func TestAnonymousUploadCreateRejectsForeignDirectory(t *testing.T) {
	ts, srv := newAnonymousUploadTestServer(t, nil)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")

	resp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Privada"}, owner)
	dir := decodeJSON[directoryDTO](t, resp)

	_, stranger := createMemberUser(t, srv, c, owner, "ajena", "contrasena-larga-456")
	wantErrorCode(t, c.do(http.MethodPost, "/api/v1/anonymous-uploads", map[string]any{"directory_id": dir.ID}, stranger),
		http.StatusForbidden, "forbidden")
}

// TestAnonymousUploadDisabledByDefault confirma secure-by-default (§3, §47):
// con newTestServer normal (sharing.anonymousUploadEnabled=false), ni crear
// ni resolver un token funcionan.
func TestAnonymousUploadDisabledByDefault(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")

	resp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Buzon"}, owner)
	dir := decodeJSON[directoryDTO](t, resp)

	wantErrorCode(t, c.do(http.MethodPost, "/api/v1/anonymous-uploads", map[string]any{"directory_id": dir.ID}, owner),
		http.StatusForbidden, "anonymous_upload_disabled")

	probeResp, err := ts.Client().Get(ts.URL + "/api/v1/public/anonymous-uploads/cualquier-token")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	wantErrorCode(t, probeResp, http.StatusForbidden, "anonymous_upload_disabled")
}

func TestAnonymousUploadHasItsOwnRateLimit(t *testing.T) {
	ts, srv := newAnonymousUploadTestServer(t, func(c *config.Config) { c.Security.RateLimit.AnonymousUploadPerMinute = 5 })
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")
	resp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Buzon"}, owner)
	dir := decodeJSON[directoryDTO](t, resp)
	resp = c.do(http.MethodPost, "/api/v1/anonymous-uploads", map[string]any{"directory_id": dir.ID}, owner)
	token := decodeJSON[anonymousUploadDTO](t, resp).Token

	var limited int
	for i := 0; i < 12; i++ {
		probeResp, err := ts.Client().Get(ts.URL + "/api/v1/public/anonymous-uploads/" + token)
		if err != nil {
			t.Fatalf("probe %d: %v", i, err)
		}
		if probeResp.StatusCode == http.StatusTooManyRequests {
			limited++
		}
		probeResp.Body.Close()
	}
	if limited == 0 {
		t.Error("ninguna de las 12 peticiones seguidas recibió 429 con anonymousUploadPerMinute=5")
	}
	// La API REST autenticada no comparte cubo con el de subida anónima.
	if resp := c.do(http.MethodGet, "/api/v1/anonymous-uploads", nil, owner); resp.StatusCode != http.StatusOK {
		t.Errorf("la API debería seguir respondiendo aunque el enlace anónimo esté limitado: %d", resp.StatusCode)
	}
}
