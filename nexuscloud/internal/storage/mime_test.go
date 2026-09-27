package storage

import "testing"

// TestDetectMimeType cubre tanto la tabla incorporada de la stdlib de Go
// como el fallback de vídeo/audio (§35, ADR-040 Fase 2) que existe
// precisamente porque esa tabla no los trae y la imagen de producción
// (distroless) no tiene /etc/mime.types para completarla -- ver el
// comentario de fallbackMimeTypesByExt en mime.go. Las extensiones de
// vídeo/audio se comprueban por PREFIJO ("video/"/"audio/"), no por valor
// exacto: en un sistema con su propia base de datos mime (p. ej. la imagen
// de desarrollo golang:1.25-bookworm, que sí trae /etc/mime.types) esa
// fuente tiene prioridad sobre el fallback y puede devolver un valor
// distinto pero igualmente correcto (p. ej. "audio/x-wav" en vez de
// "audio/wav") -- lo único que de verdad importa (y lo único que también
// mira kindOf() en PreviewDialog.tsx) es el prefijo.
func TestDetectMimeType(t *testing.T) {
	cases := []struct {
		name       string
		file       string
		wantPrefix string
		wantExact  string
	}{
		{"pdf (tabla incorporada de Go)", "informe.pdf", "", "application/pdf"},
		{"png (tabla incorporada de Go)", "foto.png", "", "image/png"},
		{"html (tabla incorporada de Go)", "pagina.html", "", "text/html; charset=utf-8"},
		{"mp4 (fallback)", "video.mp4", "video/", ""},
		{"webm (fallback)", "video.webm", "video/", ""},
		{"mov (fallback)", "video.mov", "video/", ""},
		{"mkv (fallback)", "video.mkv", "video/", ""},
		{"mp3 (fallback)", "audio.mp3", "audio/", ""},
		{"wav (fallback)", "audio.wav", "audio/", ""},
		{"ogg (fallback)", "audio.ogg", "audio/", ""},
		{"m4a (fallback)", "audio.m4a", "audio/", ""},
		{"flac (fallback)", "audio.flac", "audio/", ""},
		{"MP4 en mayúsculas (fallback case-insensitive)", "VIDEO.MP4", "video/", ""},
		{"extensión desconocida", "archivo.xyz123", "", "application/octet-stream"},
		{"sin extensión", "Dockerfile", "", "application/octet-stream"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectMimeType(tc.file)
			if tc.wantExact != "" && got != tc.wantExact {
				t.Errorf("detectMimeType(%q) = %q, esperado exactamente %q", tc.file, got, tc.wantExact)
			}
			if tc.wantPrefix != "" && len(got) < len(tc.wantPrefix) || (tc.wantPrefix != "" && got[:len(tc.wantPrefix)] != tc.wantPrefix) {
				t.Errorf("detectMimeType(%q) = %q, esperado el prefijo %q", tc.file, got, tc.wantPrefix)
			}
		})
	}
}

// TestFallbackMimeTypesByExtSonVideoOAudio guarda contra un error de
// tipeo en la tabla (mime.go) que rompería en silencio la clasificación
// de PreviewDialog.tsx (que decide "video"/"audio" mirando exactamente
// ese mismo prefijo).
func TestFallbackMimeTypesByExtSonVideoOAudio(t *testing.T) {
	for ext, mimeType := range fallbackMimeTypesByExt {
		isVideo := len(mimeType) >= 6 && mimeType[:6] == "video/"
		isAudio := len(mimeType) >= 6 && mimeType[:6] == "audio/"
		if !isVideo && !isAudio {
			t.Errorf("fallbackMimeTypesByExt[%q] = %q, no empieza por video/ ni audio/", ext, mimeType)
		}
	}
}
