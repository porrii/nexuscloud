package storage

import (
	"mime"
	"path/filepath"
)

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
	return "application/octet-stream"
}
