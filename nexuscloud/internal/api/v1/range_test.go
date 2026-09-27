package apiv1

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
)

func TestParseResumeRange(t *testing.T) {
	cases := []struct {
		name       string
		header     string
		size       int64
		wantStart  int64
		wantResult rangeResult
	}{
		{"sin cabecera", "", 500, 0, rangeNone},
		{"valido a mitad", "bytes=100-", 500, 100, rangeValid},
		{"valido en el ultimo byte", "bytes=499-", 500, 499, rangeValid},
		{"desde el principio", "bytes=0-", 500, 0, rangeValid},
		{"igual al tamano -> insatisfacible", "bytes=500-", 500, 0, rangeUnsatisfiable},
		{"mayor que el tamano -> insatisfacible", "bytes=600-", 500, 0, rangeUnsatisfiable},
		{"con fin explicito no soportado", "bytes=100-200", 500, 0, rangeNone},
		{"multi-range no soportado", "bytes=100-200,300-400", 500, 0, rangeNone},
		{"unidad distinta de bytes", "items=0-", 500, 0, rangeNone},
		{"numero negativo", "bytes=-1-", 500, 0, rangeNone},
		{"basura", "bytes=abc-", 500, 0, rangeNone},
		{"solo el prefijo", "bytes=", 500, 0, rangeNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, result := parseResumeRange(tc.header, tc.size)
			if start != tc.wantStart || result != tc.wantResult {
				t.Errorf("parseResumeRange(%q, %d) = (%d, %v), esperado (%d, %v)",
					tc.header, tc.size, start, result, tc.wantStart, tc.wantResult)
			}
		})
	}
}

// TestContentDisposition cubre la lista blanca de tipos que pueden
// previsualizarse en línea (§35, ADR-040 Fase 2) -- PDF/vídeo/audio nunca
// ejecutan script como documento de nivel superior en este origen, a
// diferencia de text/html o image/svg+xml, que deben seguir forzando la
// descarga.
func TestContentDisposition(t *testing.T) {
	cases := []struct {
		mimeType string
		want     string
	}{
		{"application/pdf", "inline"},
		{"video/mp4", "inline"},
		{"video/webm", "inline"},
		{"audio/mpeg", "inline"},
		{"audio/ogg", "inline"},
		{"image/png", "attachment"},
		{"image/jpeg", "attachment"},
		{"image/svg+xml", "attachment"},
		{"text/html", "attachment"},
		{"text/plain", "attachment"},
		{"application/octet-stream", "attachment"},
		{"application/zip", "attachment"},
		{"", "attachment"},
	}
	for _, tc := range cases {
		t.Run(tc.mimeType, func(t *testing.T) {
			if got := contentDisposition(tc.mimeType); got != tc.want {
				t.Errorf("contentDisposition(%q) = %q, esperado %q", tc.mimeType, got, tc.want)
			}
		})
	}
}

// nonSeekableReadCloser solo implementa io.Reader/io.Closer -- simula el
// tipo de stream que un provider de storage no local (§157) podría
// devolver algún día, sin io.Seeker.
type nonSeekableReadCloser struct {
	io.Reader
}

func (nonSeekableReadCloser) Close() error { return nil }

func TestServeFileContentSinRange(t *testing.T) {
	content := []byte("contenido de prueba con ñ")
	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	w := httptest.NewRecorder()

	err := serveFileContent(w, req, "archivo.txt", "text/plain", "deadbeef", int64(len(content)),
		io.NopCloser(bytes.NewReader(content)))
	if err != nil {
		t.Fatalf("serveFileContent falló: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, esperado 200", w.Code)
	}
	if got := w.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, esperado \"bytes\"", got)
	}
	if want := strconv.Itoa(len(content)); w.Header().Get("Content-Length") != want {
		t.Errorf("Content-Length = %q, esperado %q", w.Header().Get("Content-Length"), want)
	}
	if w.Body.String() != string(content) {
		t.Errorf("cuerpo = %q, esperado %q", w.Body.String(), content)
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="archivo.txt"` {
		t.Errorf(`Content-Disposition = %q, esperado attachment; filename="archivo.txt"`, got)
	}
}

// TestServeFileContentInlineParaPDF confirma que la previsualización (§35,
// ADR-040 Fase 2) llega de verdad hasta las cabeceras reales, no solo hasta
// las funciones puras -- un <embed type="application/pdf"> se queda en
// blanco si Content-Disposition dice "attachment" O si X-Frame-Options
// sigue en DENY (encontrado verificando la previsualización en un
// navegador real, no con un test: dos causas distintas del mismo síntoma).
func TestServeFileContentInlineParaPDF(t *testing.T) {
	content := []byte("%PDF-1.4 contenido de prueba")
	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	w := httptest.NewRecorder()
	w.Header().Set("X-Frame-Options", "DENY") // security.Headers ya lo puso así antes de llegar aquí

	err := serveFileContent(w, req, "informe.pdf", "application/pdf", "deadbeef", int64(len(content)),
		io.NopCloser(bytes.NewReader(content)))
	if err != nil {
		t.Fatalf("serveFileContent falló: %v", err)
	}
	if got := w.Header().Get("Content-Disposition"); got != `inline; filename="informe.pdf"` {
		t.Errorf(`Content-Disposition = %q, esperado inline; filename="informe.pdf"`, got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, esperado SAMEORIGIN (si no, el <embed> del PDF se queda en blanco)", got)
	}
}

// TestServeFileContentNoRelajaXFrameOptionsFueraDePDF confirma que la
// relajación de arriba no se cuela para ningún otro tipo -- en especial
// audio/vídeo, que no la necesitan (no son un contexto de framing para el
// navegador), y cualquier tipo que sí pudiera ejecutar script si se abriera
// como documento de nivel superior (text/html, image/svg+xml).
func TestServeFileContentNoRelajaXFrameOptionsFueraDePDF(t *testing.T) {
	for _, mimeType := range []string{"audio/mpeg", "video/mp4", "text/html", "image/svg+xml", "text/plain"} {
		t.Run(mimeType, func(t *testing.T) {
			content := []byte("contenido")
			req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
			w := httptest.NewRecorder()
			w.Header().Set("X-Frame-Options", "DENY")

			if err := serveFileContent(w, req, "archivo", mimeType, "deadbeef", int64(len(content)),
				io.NopCloser(bytes.NewReader(content))); err != nil {
				t.Fatalf("serveFileContent falló: %v", err)
			}
			if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q para %s, esperado que siguiera en DENY", got, mimeType)
			}
		})
	}
}

func TestServeFileContentConRangeValido(t *testing.T) {
	content := []byte("0123456789")
	tmp, err := os.CreateTemp(t.TempDir(), "range-test-")
	if err != nil {
		t.Fatalf("CreateTemp falló: %v", err)
	}
	if _, err := tmp.Write(content); err != nil {
		t.Fatalf("escribiendo el temporal: %v", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rebobinando el temporal: %v", err)
	}
	defer tmp.Close()

	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	req.Header.Set("Range", "bytes=4-")
	w := httptest.NewRecorder()

	err = serveFileContent(w, req, "archivo.txt", "text/plain", "deadbeef", int64(len(content)), tmp)
	if err != nil {
		t.Fatalf("serveFileContent falló: %v", err)
	}

	if w.Code != http.StatusPartialContent {
		t.Errorf("status = %d, esperado 206", w.Code)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 4-9/10" {
		t.Errorf("Content-Range = %q, esperado \"bytes 4-9/10\"", got)
	}
	if got := w.Header().Get("Content-Length"); got != "6" {
		t.Errorf("Content-Length = %q, esperado \"6\"", got)
	}
	if w.Body.String() != "456789" {
		t.Errorf("cuerpo = %q, esperado \"456789\" (desde el byte 4)", w.Body.String())
	}
}

func TestServeFileContentRangeFueraDeRango(t *testing.T) {
	content := []byte("0123456789")
	tmp, err := os.CreateTemp(t.TempDir(), "range-test-")
	if err != nil {
		t.Fatalf("CreateTemp falló: %v", err)
	}
	if _, err := tmp.Write(content); err != nil {
		t.Fatalf("escribiendo el temporal: %v", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rebobinando el temporal: %v", err)
	}
	defer tmp.Close()

	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	req.Header.Set("Range", "bytes=999-")
	w := httptest.NewRecorder()

	err = serveFileContent(w, req, "archivo.txt", "text/plain", "deadbeef", int64(len(content)), tmp)
	if err != nil {
		t.Fatalf("serveFileContent falló: %v", err)
	}

	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("status = %d, esperado 416", w.Code)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes */10" {
		t.Errorf("Content-Range = %q, esperado \"bytes */10\"", got)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, esperado \"application/json\" (sobre de error estándar)", got)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("cuerpo del 416 no es JSON válido: %v (%q)", err, w.Body.String())
	}
	if body.Error.Code != "range_not_satisfiable" {
		t.Errorf("error.code = %q, esperado \"range_not_satisfiable\"", body.Error.Code)
	}
}

func TestServeFileContentNoSeekableIgnoraElRange(t *testing.T) {
	content := []byte("0123456789")
	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	req.Header.Set("Range", "bytes=4-")
	w := httptest.NewRecorder()

	rc := nonSeekableReadCloser{Reader: bytes.NewReader(content)}
	err := serveFileContent(w, req, "archivo.txt", "text/plain", "deadbeef", int64(len(content)), rc)
	if err != nil {
		t.Fatalf("serveFileContent falló: %v", err)
	}

	// Sin io.Seeker, el Range se ignora -- se sirve el archivo completo
	// (200), nunca un error.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, esperado 200 (Range ignorado sin Seeker)", w.Code)
	}
	if w.Body.String() != string(content) {
		t.Errorf("cuerpo = %q, esperado el contenido completo %q", w.Body.String(), content)
	}
}
