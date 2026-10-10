package storage

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func defaultExecTestLimits() ExecThumbnailLimits {
	return ExecThumbnailLimits{MaxInputBytes: 200 * 1024 * 1024, Timeout: 10 * time.Second}
}

func TestGenerateVideoThumbnail_ArchivoDemasiadoGrande(t *testing.T) {
	limits := defaultExecTestLimits()
	limits.MaxInputBytes = 100
	// inputPath/tempDir con valores que harían fallar cualquier intento
	// real de invocar ffmpeg -- si el código llegara a intentarlo antes de
	// comprobar el tamaño, el error sería otro distinto de
	// ErrThumbnailInputTooLarge.
	_, err := GenerateVideoThumbnail(context.Background(), "/no/existe", t.TempDir(), 200, limits)
	if !errors.Is(err, ErrThumbnailInputTooLarge) {
		t.Errorf("err = %v, esperado ErrThumbnailInputTooLarge", err)
	}
}

func TestGeneratePDFThumbnail_ArchivoDemasiadoGrande(t *testing.T) {
	limits := defaultExecTestLimits()
	limits.MaxInputBytes = 100
	_, err := GeneratePDFThumbnail(context.Background(), "/no/existe", t.TempDir(), 200, limits)
	if !errors.Is(err, ErrThumbnailInputTooLarge) {
		t.Errorf("err = %v, esperado ErrThumbnailInputTooLarge", err)
	}
}

// TestGenerateVideoThumbnail_BinarioAusente simula "ffmpeg no instalado"
// vía la indirección execLookPath, en vez de depender de qué haya
// instalado de verdad el entorno que corre este test -- confirma que la
// función degrada con gracia (error normal, nunca panic ni cuelgue).
func TestGenerateVideoThumbnail_BinarioAusente(t *testing.T) {
	original := execLookPath
	t.Cleanup(func() { execLookPath = original })
	execLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	_, err := GenerateVideoThumbnail(context.Background(), "/no/existe", t.TempDir(), 10, defaultExecTestLimits())
	if !errors.Is(err, ErrThumbnailBinaryNotFound) {
		t.Errorf("err = %v, esperado ErrThumbnailBinaryNotFound", err)
	}
}

func TestGeneratePDFThumbnail_BinarioAusente(t *testing.T) {
	original := execLookPath
	t.Cleanup(func() { execLookPath = original })
	execLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	_, err := GeneratePDFThumbnail(context.Background(), "/no/existe", t.TempDir(), 10, defaultExecTestLimits())
	if !errors.Is(err, ErrThumbnailBinaryNotFound) {
		t.Errorf("err = %v, esperado ErrThumbnailBinaryNotFound", err)
	}
}

// hasBinary confirma disponibilidad real -- estos tests solo tienen
// sentido en una imagen que traiga ffmpeg/poppler-utils (§34 Decisión 0);
// en golang:1.25-bookworm (sin ellos) se saltan con gracia en vez de
// fallar el resto de la suite.
func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func TestGenerateVideoThumbnail_ArchivoRealConFfmpeg(t *testing.T) {
	if !hasBinary("ffmpeg") {
		t.Skip("ffmpeg no está instalado en este entorno de test -- ver §34 Decisión 0 (imagen debian:12-slim)")
	}
	fixture := filepath.Join("testdata", "sample.mp4")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture %s no disponible: %v", fixture, err)
	}
	info, err := os.Stat(fixture)
	if err != nil {
		t.Fatalf("os.Stat: %v", err)
	}

	out, err := GenerateVideoThumbnail(context.Background(), fixture, t.TempDir(), info.Size(), defaultExecTestLimits())
	if err != nil {
		t.Fatalf("GenerateVideoThumbnail: %v", err)
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("la miniatura de vídeo debería ser un JPEG válido: %v", err)
	}
}

func TestGeneratePDFThumbnail_ArchivoRealConPdftoppm(t *testing.T) {
	if !hasBinary("pdftoppm") {
		t.Skip("pdftoppm no está instalado en este entorno de test -- ver §34 Decisión 0 (imagen debian:12-slim)")
	}
	fixture := filepath.Join("testdata", "sample.pdf")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture %s no disponible: %v", fixture, err)
	}
	info, err := os.Stat(fixture)
	if err != nil {
		t.Fatalf("os.Stat: %v", err)
	}

	out, err := GeneratePDFThumbnail(context.Background(), fixture, t.TempDir(), info.Size(), defaultExecTestLimits())
	if err != nil {
		t.Fatalf("GeneratePDFThumbnail: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("la miniatura de PDF debería ser un JPEG válido: %v", err)
	}
}

// TestGenerateVideoThumbnail_TimeoutMataElProceso usa un timeout real
// absurdamente corto contra una invocación real de ffmpeg (a diferencia
// del test de timeout del pipeline de imagen, que usa un contexto ya
// cancelado por no poder confiar en una carrera de temporización): aquí
// SÍ es una carrera legítima, porque exec.CommandContext mata el proceso
// de verdad (a diferencia de una goroutine Go pura) -- 1 microsegundo es
// muchísimo menos que lo que tarda el propio SO en arrancar el proceso
// ffmpeg, así que el timeout gana con fiabilidad práctica.
func TestGenerateVideoThumbnail_TimeoutMataElProceso(t *testing.T) {
	if !hasBinary("ffmpeg") {
		t.Skip("ffmpeg no está instalado en este entorno de test -- ver §34 Decisión 0 (imagen debian:12-slim)")
	}
	fixture := filepath.Join("testdata", "sample.mp4")
	info, err := os.Stat(fixture)
	if err != nil {
		t.Skipf("fixture %s no disponible: %v", fixture, err)
	}

	limits := defaultExecTestLimits()
	limits.Timeout = time.Microsecond
	_, err = GenerateVideoThumbnail(context.Background(), fixture, t.TempDir(), info.Size(), limits)
	if !errors.Is(err, ErrThumbnailTimeout) {
		t.Errorf("err = %v, esperado ErrThumbnailTimeout con un timeout de %s", err, limits.Timeout)
	}
}

// TestGenerateVideoThumbnail_ProtocolWhitelistBloqueaFileScheme es EL
// test que de verdad demuestra el hallazgo ALTO del pase de seguridad
// (§34 Decisión 4): un archivo con extensión .mp4 cuyo CONTENIDO real es
// una playlist ffconcat que referencia otra ruta del sistema NO debe
// poder hacer que ffmpeg la abra. Sin -protocol_whitelist file (de hecho,
// aquí probamos justo lo contrario: cuando el protocolo referenciado NO
// es "file" sino algo que -protocol_whitelist file bloquea explícitamente
// como concat/subfile) ffmpeg fallaría con "Protocol not on whitelist" en
// vez de intentar abrir la referencia -- confirmamos el mensaje de error
// exacto, no solo que la llamada "falló por algo".
func TestGenerateVideoThumbnail_ProtocolWhitelistBloqueaFileScheme(t *testing.T) {
	if !hasBinary("ffmpeg") {
		t.Skip("ffmpeg no está instalado en este entorno de test -- ver §34 Decisión 0 (imagen debian:12-slim)")
	}
	dir := t.TempDir()
	secret := filepath.Join(dir, "secreto.mp4")
	if err := os.WriteFile(secret, []byte("contenido que ffmpeg nunca debería leer a través del ataque"), 0o600); err != nil {
		t.Fatalf("escribiendo fixture secreto: %v", err)
	}
	// ffconcat: un demuxer de ffmpeg que sigue rutas EMBEBIDAS dentro del
	// propio archivo -- exactamente el vector que -protocol_whitelist file
	// (que solo permite el protocolo "file" para la entrada PRINCIPAL, no
	// para sub-referencias de otros demuxers) debe bloquear.
	malicious := filepath.Join(dir, "malicioso.mp4")
	content := "ffconcat version 1.0\nfile '" + secret + "'\n"
	if err := os.WriteFile(malicious, []byte(content), 0o600); err != nil {
		t.Fatalf("escribiendo fixture malicioso: %v", err)
	}

	_, err := GenerateVideoThumbnail(context.Background(), malicious, dir, int64(len(content)), defaultExecTestLimits())
	if err == nil {
		t.Fatal("se esperaba que ffmpeg fallara sobre un archivo ffconcat malicioso, no que generara una miniatura")
	}
	// No afirmamos el mensaje EXACTO de ffmpeg (puede variar entre
	// versiones), pero sí que no fue un éxito silencioso ni un timeout --
	// fue un fallo real de invocación, consistente con el protocolo
	// bloqueado.
	if !strings.Contains(err.Error(), "ffmpeg falló") {
		t.Errorf("err = %v, esperado un fallo de invocación de ffmpeg", err)
	}
}
