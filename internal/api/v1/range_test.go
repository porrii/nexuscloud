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
