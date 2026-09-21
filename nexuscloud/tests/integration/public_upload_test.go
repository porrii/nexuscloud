// Tests de integración de la subida por enlace público (§37, ADR-035): no
// sobrescribe en silencio lo que ya hay en la carpeta del propietario y no
// revela su papelera, por HTTP real sobre el servidor completo.
package integration

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/server"
)

// newPublicLinksTestServer es newTestServer con los enlaces públicos activados:
// vienen desactivados por defecto (secure-by-default, §3/§47), así que la ruta
// /api/v1/public/* ni existe a menos que se pida aquí.
func newPublicLinksTestServer(t *testing.T) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true
	cfg.Sharing.PublicLinksEnabled = true

	srv, err := server.Build(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("server.Build falló: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, srv
}

func TestPublicLinkUploadDoesNotOverwriteAnExistingFile(t *testing.T) {
	ts, srv := newPublicLinksTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")

	resp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Buzon"}, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear /Buzon = %d", resp.StatusCode)
	}
	dir := decodeJSON[directoryDTO](t, resp)
	original := uploadTestFile(t, c, owner, "/Buzon", "acta.txt", "original de la dueña")
	borrado := uploadTestFile(t, c, owner, "/Buzon", "borrado.txt", "algo que la dueña borró")
	if resp := c.do(http.MethodDelete, "/api/v1/files/"+borrado.ID, nil, owner); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("borrar el archivo = %d", resp.StatusCode)
	}

	resp = c.do(http.MethodPost, "/api/v1/shares", map[string]any{
		"resource_type": "directory", "resource_id": dir.ID, "share_type": "link", "can_upload": true,
	}, owner)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("crear el enlace = %d, cuerpo = %s", resp.StatusCode, body)
	}
	token := decodeJSON[tokenDTO](t, resp).Token

	upload := func(name, content string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost,
			ts.URL+"/api/v1/public/shares/"+token+"/upload?name="+url.QueryEscape(name), strings.NewReader(content))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := ts.Client().Do(req) // sin sesión: es un enlace público
		if err != nil {
			t.Fatalf("subiendo %s: %v", name, err)
		}
		return resp
	}

	// Un nombre ocupado por un archivo activo no se pisa, y el original queda intacto.
	wantErrorCode(t, upload("acta.txt", "intento de pisarlo"), http.StatusConflict, "destination_occupied")
	dl := c.do(http.MethodGet, "/api/v1/files/"+original.ID, nil, owner)
	defer dl.Body.Close()
	if got, _ := io.ReadAll(dl.Body); string(got) != "original de la dueña" {
		t.Errorf("contenido del original tras el intento = %q", got)
	}

	// Uno ocupado por la papelera da el mismo 409 y no la menciona: un anónimo no
	// debe averiguar qué ha borrado el propietario.
	trashed := upload("borrado.txt", "intento")
	defer trashed.Body.Close()
	body, _ := io.ReadAll(trashed.Body)
	if trashed.StatusCode != http.StatusConflict || !strings.Contains(string(body), `"destination_occupied"`) ||
		strings.Contains(strings.ToLower(string(body)), "papelera") {
		t.Errorf("status = %d, cuerpo = %s; esperado 409 destination_occupied sin mencionar la papelera", trashed.StatusCode, body)
	}

	// Un nombre nuevo sigue subiendo con normalidad.
	fresh := upload("nuevo.txt", "hola")
	defer fresh.Body.Close()
	if fresh.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(fresh.Body)
		t.Errorf("subida de un nombre nuevo: status = %d, cuerpo = %s", fresh.StatusCode, b)
	}
}
