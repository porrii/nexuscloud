// Tests de integración de ADR-043: los enlaces públicos y de subida anónima
// de una cuenta desactivada responden 404 (como un token inexistente) en
// las seis rutas públicas, y vuelven a funcionar al reactivarla.
package integration

import (
	"net/http"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
)

func TestDisabledOwnerLinksAreNotFoundUntilReactivated(t *testing.T) {
	ts, srv := newAnonymousUploadTestServer(t, func(cfg *config.Config) {
		cfg.Sharing.PublicLinksEnabled = true
		cfg.Security.RateLimit.PublicLinkPerMinute = 1000
	})
	createAdmin(t, srv, "admin", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "admin", "contrasena-larga-123")
	memberID, member := createMemberUser(t, srv, c, admin, "empleado", "contrasena-larga-456")

	resp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Proyecto"}, member)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear /Proyecto = %d", resp.StatusCode)
	}
	dir := decodeJSON[directoryDTO](t, resp)
	file := uploadTestFile(t, c, member, "/", "informe.txt", "datos de la empresa")

	resp = c.do(http.MethodPost, "/api/v1/shares", map[string]any{
		"resource_type": "file", "resource_id": file.ID, "share_type": "link", "can_download": true,
	}, member)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear enlace de archivo = %d", resp.StatusCode)
	}
	fileToken := decodeJSON[tokenDTO](t, resp).Token

	resp = c.do(http.MethodPost, "/api/v1/shares", map[string]any{
		"resource_type": "directory", "resource_id": dir.ID, "share_type": "link",
		"can_download": true, "can_upload": true,
	}, member)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear enlace de carpeta = %d", resp.StatusCode)
	}
	dirToken := decodeJSON[tokenDTO](t, resp).Token

	resp = c.do(http.MethodPost, "/api/v1/anonymous-uploads", map[string]any{"directory_id": dir.ID}, member)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear enlace de subida anónima = %d", resp.StatusCode)
	}
	anonToken := decodeJSON[anonymousUploadDTO](t, resp).Token

	publicStatuses := func() map[string]int {
		t.Helper()
		out := map[string]int{}
		get := func(label, path string) {
			r := c.do(http.MethodGet, path, nil, "")
			r.Body.Close()
			out[label] = r.StatusCode
		}
		get("GET share", "/api/v1/public/shares/"+fileToken)
		get("GET share/download", "/api/v1/public/shares/"+fileToken+"/download")
		get("GET share/browse", "/api/v1/public/shares/"+dirToken+"/browse")
		get("GET anonymous-upload", "/api/v1/public/anonymous-uploads/"+anonToken)

		r := c.do(http.MethodPost, "/api/v1/public/shares/"+dirToken+"/upload?name=desde-enlace.txt", nil, "")
		r.Body.Close()
		out["POST share/upload"] = r.StatusCode
		r = anonymousUpload(t, ts, anonToken, "anonimo.txt", "x")
		r.Body.Close()
		out["POST anonymous-upload"] = r.StatusCode
		return out
	}

	setStatus := func(status string) {
		t.Helper()
		r := c.do(http.MethodPatch, "/api/v1/users/"+memberID, map[string]any{"status": status}, admin)
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("PATCH status=%s = %d", status, r.StatusCode)
		}
	}

	setStatus("disabled")
	for label, code := range publicStatuses() {
		if code != http.StatusNotFound {
			t.Errorf("%s con la cuenta desactivada = %d, esperado 404", label, code)
		}
	}

	setStatus("active")
	for label, code := range publicStatuses() {
		if code == http.StatusNotFound || code >= 500 {
			t.Errorf("%s tras reactivar = %d, esperado que el enlace vuelva a funcionar", label, code)
		}
	}
}
