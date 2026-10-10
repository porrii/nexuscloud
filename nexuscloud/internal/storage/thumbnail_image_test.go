package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"
)

func mustEncodeJPEG(w, h int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func mustEncodePNG(w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func mustEncodeGIF(w, h int) []byte {
	var buf bytes.Buffer
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.White, color.Black})
	if err := gif.Encode(&buf, img, nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// fakePNGWithHugeDimensions codifica un PNG real de 1x1 y después
// parchea a mano el ancho/alto declarados en su chunk IHDR (recalculando
// el CRC32 para que siga siendo un PNG "válido" a ojos de
// image.DecodeConfig, que solo lee la cabecera) -- simula el caso clásico
// de decompression bomb: un archivo minúsculo en bytes que declara una
// resolución enorme.
func fakePNGWithHugeDimensions(t *testing.T, width, height uint32) []byte {
	t.Helper()
	data := mustEncodePNG(1, 1)
	const sigLen, lenFieldLen, typeLen = 8, 4, 4
	ihdrDataStart := sigLen + lenFieldLen + typeLen
	if !bytes.Equal(data[sigLen+lenFieldLen:ihdrDataStart], []byte("IHDR")) {
		t.Fatalf("el primer chunk del PNG generado no es IHDR -- fixture inesperado")
	}
	binary.BigEndian.PutUint32(data[ihdrDataStart:], width)
	binary.BigEndian.PutUint32(data[ihdrDataStart+4:], height)
	crc := crc32.ChecksumIEEE(data[sigLen+lenFieldLen : ihdrDataStart+13])
	binary.BigEndian.PutUint32(data[ihdrDataStart+13:], crc)
	return data
}

func defaultTestLimits() ImageThumbnailLimits {
	return ImageThumbnailLimits{
		MaxInputBytes: 25 * 1024 * 1024,
		MaxPixels:     40_000_000,
		Timeout:       5 * time.Second,
	}
}

func decodedDimensions(t *testing.T, jpegBytes []byte) (int, int) {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("la miniatura generada no es un JPEG válido: %v", err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

func TestGenerateImageThumbnail_JPEGValidoSeRedimensiona(t *testing.T) {
	src := mustEncodeJPEG(800, 600)
	out, err := GenerateImageThumbnail(context.Background(), bytes.NewReader(src), int64(len(src)), defaultTestLimits())
	if err != nil {
		t.Fatalf("GenerateImageThumbnail: %v", err)
	}
	w, h := decodedDimensions(t, out)
	if w > thumbnailMaxDimension || h > thumbnailMaxDimension {
		t.Errorf("miniatura de %dx%d, esperado como mucho %d en el lado mayor", w, h, thumbnailMaxDimension)
	}
	if w != 320 || h != 240 {
		t.Errorf("dimensiones = %dx%d, esperado 320x240 (proporción 4:3 preservada)", w, h)
	}
}

func TestGenerateImageThumbnail_PNGValido(t *testing.T) {
	src := mustEncodePNG(100, 50)
	out, err := GenerateImageThumbnail(context.Background(), bytes.NewReader(src), int64(len(src)), defaultTestLimits())
	if err != nil {
		t.Fatalf("GenerateImageThumbnail: %v", err)
	}
	w, h := decodedDimensions(t, out)
	if w != 100 || h != 50 {
		t.Errorf("una imagen ya más pequeña que el máximo no debería reescalarse; dimensiones = %dx%d", w, h)
	}
}

func TestGenerateImageThumbnail_GIFValidoUnSoloFrame(t *testing.T) {
	src := mustEncodeGIF(40, 40)
	out, err := GenerateImageThumbnail(context.Background(), bytes.NewReader(src), int64(len(src)), defaultTestLimits())
	if err != nil {
		t.Fatalf("GenerateImageThumbnail: %v", err)
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("la miniatura de un GIF debería seguir siendo una imagen válida: %v", err)
	}
}

func TestGenerateImageThumbnail_ArchivoDemasiadoGrande(t *testing.T) {
	limits := defaultTestLimits()
	limits.MaxInputBytes = 100
	// Un reader vacío: si el código intentara leerlo antes de comprobar el
	// tamaño, fallaría con un error de formato distinto, no con
	// ErrThumbnailInputTooLarge -- confirma que la comprobación de tamaño
	// ocurre ANTES de tocar el contenido.
	_, err := GenerateImageThumbnail(context.Background(), bytes.NewReader(nil), 200, limits)
	if !errors.Is(err, ErrThumbnailInputTooLarge) {
		t.Errorf("err = %v, esperado ErrThumbnailInputTooLarge", err)
	}
}

func TestGenerateImageThumbnail_DimensionesDeclaradasExcesivas(t *testing.T) {
	fake := fakePNGWithHugeDimensions(t, 50000, 50000) // 2.5 mil millones de píxeles
	limits := defaultTestLimits()
	limits.MaxPixels = 40_000_000

	_, err := GenerateImageThumbnail(context.Background(), bytes.NewReader(fake), int64(len(fake)), limits)
	if !errors.Is(err, ErrThumbnailDimensionsTooLarge) {
		t.Errorf("err = %v, esperado ErrThumbnailDimensionsTooLarge (decompression bomb clásica: PNG minúsculo, dimensiones declaradas enormes)", err)
	}
}

func TestGenerateImageThumbnail_FormatoNoReconocido(t *testing.T) {
	garbage := []byte("esto no es una imagen de ningún formato reconocido")
	_, err := GenerateImageThumbnail(context.Background(), bytes.NewReader(garbage), int64(len(garbage)), defaultTestLimits())
	if !errors.Is(err, ErrThumbnailUnsupportedFormat) {
		t.Errorf("err = %v, esperado ErrThumbnailUnsupportedFormat", err)
	}
}

// TestGenerateImageThumbnail_Timeout usa un contexto padre YA CANCELADO en
// vez de confiar en una carrera de temporización contra la velocidad real
// de decodificación (que sería no determinista) -- el context.WithTimeout
// derivado de un padre ya cancelado está Done() de inmediato, ejerciendo
// exactamente la misma rama del select que un timeout real habría
// disparado, sin depender de cuánto tarde decodeAndResizeImage en correr.
func TestGenerateImageThumbnail_Timeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src := mustEncodeJPEG(50, 50)
	_, err := GenerateImageThumbnail(ctx, bytes.NewReader(src), int64(len(src)), defaultTestLimits())
	if !errors.Is(err, ErrThumbnailTimeout) {
		t.Errorf("err = %v, esperado ErrThumbnailTimeout", err)
	}
}

// TestGenerateImageThumbnail_RecuperaPanic confirma que el recover() de
// GenerateImageThumbnail atrapa de verdad un panic en la goroutine de
// decodificación, en vez de dejarlo propagarse y tumbar el proceso
// entero -- el hallazgo CRÍTICO del pase de security-reviewer sobre el
// diseño (§34 Decisión 6). Inyecta el panic vía decodeAndResizeImageFn en
// vez de depender de encontrar una entrada real que haga paniquear algún
// decodificador.
func TestGenerateImageThumbnail_RecuperaPanic(t *testing.T) {
	original := decodeAndResizeImageFn
	t.Cleanup(func() { decodeAndResizeImageFn = original })
	decodeAndResizeImageFn = func(r io.Reader, maxPixels int64) ([]byte, error) {
		panic("fallo simulado de decodificador")
	}

	_, err := GenerateImageThumbnail(context.Background(), bytes.NewReader([]byte("no importa")), 10, defaultTestLimits())
	if err == nil {
		t.Fatal("se esperaba un error tras el panic simulado (recuperado), no nil -- y el test no debería haber crasheado")
	}
	if !strings.Contains(err.Error(), "panic decodificando imagen") {
		t.Errorf("err = %q, esperado que mencione el panic recuperado", err.Error())
	}
}

// FuzzDecodeAndResizeImage exige "nunca panic" sobre el pipeline de
// decodificación completo (§34 Decisión 4/6) -- no que produzca una
// miniatura válida. `go test ./internal/storage/... -run FuzzDecodeAndResizeImage`
// solo corre el corpus semilla de abajo (rápido, determinista); fuzzing
// real de verdad: `go test ./internal/storage/... -fuzz=FuzzDecodeAndResizeImage -fuzztime=60s`.
func FuzzDecodeAndResizeImage(f *testing.F) {
	f.Add(mustEncodeJPEG(20, 15))
	f.Add(mustEncodePNG(20, 15))
	f.Add(mustEncodeGIF(20, 15))
	f.Add([]byte("no es una imagen"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("decodeAndResizeImage no debe paniquear nunca, recuperado: %v (entrada de %d bytes)", p, len(data))
			}
		}()
		_, _ = decodeAndResizeImage(bytes.NewReader(data), 40_000_000)
	})
}
