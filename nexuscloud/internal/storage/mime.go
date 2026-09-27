package storage

import (
	"mime"
	"path/filepath"
	"strings"
)

// fallbackMimeTypesByExt cubre vídeo/audio (§35, ADR-040 Fase 2:
// previsualización) que la tabla incorporada de mime.TypeByExtension NO
// trae (solo avif/css/gif/htm/html/jpeg/jpg/js/json/mjs/pdf/png/svg/wasm/
// webp/xml, ver mime/type.go de la stdlib) -- en la mayoría de sistemas se
// completa leyendo /etc/mime.types o la base de datos del SO, pero la
// imagen de producción es gcr.io/distroless/static-debian12, que no trae
// ninguna de las dos (confirmado: builder golang:1.25-bookworm sí tiene
// /etc/mime.types, distroless no). Sin esta tabla, todo archivo de vídeo/
// audio caería a application/octet-stream SOLO en producción -- nunca en
// desarrollo, donde el problema no se ve -- y la previsualización de
// vídeo/audio de PreviewDialog.tsx jamás se activaría para nadie.
var fallbackMimeTypesByExt = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/x-m4v",
	".webm": "video/webm",
	".ogv":  "video/ogg",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".oga":  "audio/ogg",
	".ogg":  "audio/ogg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".weba": "audio/webm",
	".opus": "audio/opus",
}

// detectMimeType infiere el tipo MIME por extensión (§75: nunca confiar
// solo en la extensión para validación de seguridad, pero sí es una base
// razonable para Content-Type informativo en listados/descargas).
func detectMimeType(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return "application/octet-stream"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	if t, ok := fallbackMimeTypesByExt[strings.ToLower(ext)]; ok {
		return t
	}
	return "application/octet-stream"
}
