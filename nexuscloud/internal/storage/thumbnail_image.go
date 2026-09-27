package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // registra "gif" en image.Decode/DecodeConfig -- decodifica UN frame, igual que gif.Decode (nunca gif.DecodeAll)
	"image/jpeg"
	_ "image/png" // registra "png"
	"io"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	// thumbnailMaxDimension: ninguna miniatura mide más que esto en su lado
	// mayor, preservando proporción (§34, ADR-041 Decisión 2).
	thumbnailMaxDimension = 320
	thumbnailJPEGQuality  = 80
)

var (
	ErrThumbnailInputTooLarge      = errors.New("storage: archivo demasiado grande para generar miniatura")
	ErrThumbnailDimensionsTooLarge = errors.New("storage: la imagen declara unas dimensiones demasiado grandes para generar miniatura")
	ErrThumbnailUnsupportedFormat  = errors.New("storage: formato de imagen no reconocido para generar miniatura")
	ErrThumbnailTimeout            = errors.New("storage: tiempo agotado generando la miniatura")
)

// ImageThumbnailLimits agrupa los controles de seguridad del pipeline de
// imagen (§34, ADR-041 Decisión 4): decodificadores de la stdlib de Go +
// x/image/webp, sin subproceso -- JPG/PNG/GIF/WEBP.
type ImageThumbnailLimits struct {
	MaxInputBytes int64
	// MaxPixels acota ancho*alto DECLARADOS (leídos por DecodeConfig, antes
	// de decodificar completo) -- mitigación estándar contra decompression
	// bombs: un archivo minúsculo en bytes que declara una resolución
	// enorme se rechaza sin llegar a decodificar el resto.
	MaxPixels int64
	Timeout   time.Duration
}

// GenerateImageThumbnail decodifica una imagen y devuelve un JPEG
// redimensionado a thumbnailMaxDimension como mucho, preservando
// proporción. sizeBytes es el tamaño YA CONOCIDO del archivo
// (FileMeta.SizeBytes) -- se comprueba contra limits.MaxInputBytes ANTES
// de leer nada de r.
//
// Se ejecuta en su propia goroutine con recover() propio (§34 Decisión 6,
// hallazgo CRÍTICO del pase de security-reviewer: un recover() de
// middleware HTTP NUNCA cubre una goroutine que el propio handler lanza)
// bajo un context.WithTimeout: si expira, esta función devuelve
// ErrThumbnailTimeout de inmediato, pero la goroutine de decodificación
// puede seguir viva hasta terminar por su cuenta -- limitación real y
// documentada de Go (sin puntos de cancelación en código puro), no un
// descuido. El límite de concurrencia de FileService (misma Decisión 6)
// es lo que de verdad acota cuántas de estas fugas pueden acumularse.
func GenerateImageThumbnail(ctx context.Context, r io.Reader, sizeBytes int64, limits ImageThumbnailLimits) ([]byte, error) {
	if sizeBytes > limits.MaxInputBytes {
		return nil, ErrThumbnailInputTooLarge
	}

	ctx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()

	type result struct {
		data []byte
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				resultCh <- result{err: fmt.Errorf("panic decodificando imagen: %v", p)}
			}
		}()
		data, err := decodeAndResizeImageFn(r, limits.MaxPixels)
		resultCh <- result{data: data, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ErrThumbnailTimeout
	case res := <-resultCh:
		return res.data, res.err
	}
}

// decodeAndResizeImageFn es una indirección deliberada (no solo una
// llamada directa a decodeAndResizeImage): permite a
// TestGenerateImageThumbnail_RecuperaPanic inyectar un panic determinista
// y comprobar que el recover() de abajo lo atrapa de verdad, en vez de
// depender de encontrar una entrada real que haga paniquear algún
// decodificador (§34 Decisión 6, hallazgo CRÍTICO del pase de
// security-reviewer).
var decodeAndResizeImageFn = decodeAndResizeImage

func decodeAndResizeImage(r io.Reader, maxPixels int64) ([]byte, error) {
	// Hace falta leer la cabecera (DecodeConfig) y LUEGO el cuerpo completo
	// (Decode) sin perder los bytes ya consumidos -- se bufferiza en
	// memoria; el tamaño ya se comprobó contra MaxInputBytes antes de
	// llegar aquí, así que este buffer tiene un tope conocido de antemano.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("leyendo el archivo: %w", err)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	isWebP := false
	if err != nil {
		// WEBP no lo reconoce la stdlib de Go (no está registrado en
		// image.RegisterFormat) -- se intenta aparte con x/image/webp
		// antes de rendirse.
		webpCfg, werr := webp.DecodeConfig(bytes.NewReader(data))
		if werr != nil {
			return nil, ErrThumbnailUnsupportedFormat
		}
		cfg, isWebP = webpCfg, true
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, ErrThumbnailDimensionsTooLarge
	}

	var img image.Image
	if isWebP {
		img, err = webp.Decode(bytes.NewReader(data))
	} else {
		img, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("decodificando imagen: %w", err)
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, resizeToFit(img, thumbnailMaxDimension), &jpeg.Options{Quality: thumbnailJPEGQuality}); err != nil {
		return nil, fmt.Errorf("codificando miniatura: %w", err)
	}
	return out.Bytes(), nil
}

// resizeToFit reduce src para que su lado mayor mida como mucho maxDim,
// preservando proporción -- nunca amplía una imagen ya más pequeña.
// Siempre devuelve un *image.RGBA nuevo (nunca src tal cual): jpeg.Encode
// no soporta canal alfa, así que cualquier transparencia (PNG/WEBP/GIF)
// se compone sobre negro por draw.Over -- pérdida de transparencia
// consciente y documentada (§34, ADR-041 Decisión 2), no un bug.
func resizeToFit(src image.Image, maxDim int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	newW, newH := w, h
	if w > maxDim || h > maxDim {
		scale := float64(maxDim) / float64(w)
		if hScale := float64(maxDim) / float64(h); hScale < scale {
			scale = hScale
		}
		newW = max(1, int(float64(w)*scale))
		newH = max(1, int(float64(h)*scale))
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	if newW == w && newH == h {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
		return dst
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}
