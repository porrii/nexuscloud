// Tests de integración de miniaturas (§34, ADR-041): GET /files/{id}/thumbnail
// (bajo demanda) y GET /admin/thumbnail-jobs, por HTTP real sobre el
// servidor completo -- arrancan un server.Server real (mismo patrón que el
// resto de este paquete).
package integration

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/server"
)

// mustEncodeJPEGForTest genera un JPEG real y válido en memoria (800x600,
// un solo color) -- el pipeline de miniaturas de verdad lo decodifica, a
// diferencia de subir bytes cualesquiera con un nombre ".jpg" (detectMimeType
// es puramente por extensión, así que eso bastaría para encolar el job pero
// nunca para generarlo con éxito).
func mustEncodeJPEGForTest(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("codificando JPEG de prueba: %v", err)
	}
	return buf.Bytes()
}

// newThumbnailsTestServer arranca el servidor con thumbnails.enabled=true --
// viene desactivado por defecto (§34 Decisión 5), así que solo los tests que
// ejercitan la generación de verdad lo activan explícitamente, mismo
// criterio que newSearchDisabledTestServer en sentido inverso.
func newThumbnailsTestServer(t *testing.T) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true
	cfg.Thumbnails.Enabled = true

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

func TestGetFileThumbnailDisabledByDefaultIs404(t *testing.T) {
	ts, srv := newTestServer(t) // thumbnails.enabled=false (default)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")
	f := uploadTestFile(t, c, owner, "/", "foto.jpg", string(mustEncodeJPEGForTest(t)))

	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/files/"+f.ID+"/thumbnail", nil, owner), http.StatusNotFound, "not_found")
}

func TestGetFileThumbnailGeneratesAndServesFromCache(t *testing.T) {
	ts, srv := newThumbnailsTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")
	f := uploadTestFile(t, c, owner, "/", "foto.jpg", string(mustEncodeJPEGForTest(t)))

	resp := c.do(http.MethodGet, "/api/v1/files/"+f.ID+"/thumbnail", nil, owner)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET .../thumbnail = %d, cuerpo = %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, esperado image/jpeg", ct)
	}
	first, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("leyendo cuerpo: %v", err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(first))
	if err != nil {
		t.Fatalf("la miniatura devuelta no es un JPEG válido: %v", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() > 320 || bounds.Dy() > 320 {
		t.Errorf("miniatura de %dx%d, esperado <=320x320 (Decisión 2)", bounds.Dx(), bounds.Dy())
	}

	// Segunda petición: debe servir desde caché, mismos bytes exactos --
	// GenerateOrGetThumbnail comprueba la caché antes de generar de nuevo.
	resp2 := c.do(http.MethodGet, "/api/v1/files/"+f.ID+"/thumbnail", nil, owner)
	second, err := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if err != nil {
		t.Fatalf("leyendo segundo cuerpo: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("la segunda petición debería servir exactamente los mismos bytes desde caché")
	}
}

func TestGetFileThumbnailForbiddenForOtherOwner(t *testing.T) {
	ts, srv := newThumbnailsTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "duena", "contrasena-larga-123")
	_, member := createMemberUser(t, srv, c, admin, "miembro", "contrasena-larga-456")
	f := uploadTestFile(t, c, admin, "/", "foto.jpg", string(mustEncodeJPEGForTest(t)))

	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/files/"+f.ID+"/thumbnail", nil, member), http.StatusForbidden, "forbidden")
}

func TestGetFileThumbnailUnsupportedFormatIs404(t *testing.T) {
	ts, srv := newThumbnailsTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")
	f := uploadTestFile(t, c, owner, "/", "notas.txt", "contenido de texto plano, sin miniatura posible")

	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/files/"+f.ID+"/thumbnail", nil, owner), http.StatusNotFound, "not_found")
}

func TestGetFileThumbnailRequiresSession(t *testing.T) {
	ts, srv := newThumbnailsTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")
	f := uploadTestFile(t, c, owner, "/", "foto.jpg", string(mustEncodeJPEGForTest(t)))

	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/files/"+f.ID+"/thumbnail", nil, ""), http.StatusUnauthorized, "")
}

func TestListThumbnailJobsRequiresAdminRoleAndListsPending(t *testing.T) {
	ts, srv := newThumbnailsTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "duena", "contrasena-larga-123")
	_, member := createMemberUser(t, srv, c, admin, "miembro", "contrasena-larga-456")
	f := uploadTestFile(t, c, admin, "/", "foto.jpg", string(mustEncodeJPEGForTest(t)))

	// Un usuario normal no puede usar /admin/thumbnail-jobs.
	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/admin/thumbnail-jobs", nil, member), http.StatusForbidden, "")

	// El upload encoló un job "pending" (enqueueThumbnailJob) que el bucle
	// en segundo plano (tick cada 10s) todavía no ha tenido tiempo de
	// procesar -- nunca se llamó a GET .../thumbnail, así que sigue ahí.
	resp := c.do(http.MethodGet, "/api/v1/admin/thumbnail-jobs?status=pending", nil, admin)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /admin/thumbnail-jobs = %d, cuerpo = %s", resp.StatusCode, body)
	}
	jobs := decodeJSON[[]struct {
		FileID string `json:"file_id"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	}](t, resp)
	found := false
	for _, j := range jobs {
		if j.FileID == f.ID {
			found = true
			if j.Kind != "image" || j.Status != "pending" {
				t.Errorf("job de %s = kind=%q status=%q, esperado kind=image status=pending", f.ID, j.Kind, j.Status)
			}
		}
	}
	if !found {
		t.Errorf("GET /admin/thumbnail-jobs?status=pending no incluye el job recién encolado de %s: %+v", f.ID, jobs)
	}
}

func TestListThumbnailJobsRejectsInvalidStatus(t *testing.T) {
	ts, srv := newThumbnailsTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "duena", "contrasena-larga-123")

	wantErrorCode(t, c.do(http.MethodGet, "/api/v1/admin/thumbnail-jobs?status=lo-que-sea", nil, admin), http.StatusBadRequest, "invalid_request")
}
