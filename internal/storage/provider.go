package storage

import (
	"context"
	"io"
)

// Provider abstrae el backend físico de almacenamiento (§102). Fase 1 solo
// implementa LocalFilesystemProvider; el diseño deja sitio para S3-compatible
// o "remote NexusCloud" en el futuro (§157) sin tocar FileService.
type Provider interface {
	// Write escribe el contenido de r en relPath (ruta lógica, con "/" como
	// separador) y devuelve el tamaño y el sha256 escritos.
	Write(ctx context.Context, relPath string, r io.Reader) (size int64, sha256Hex string, err error)
	Read(ctx context.Context, relPath string) (io.ReadCloser, error)
	Delete(ctx context.Context, relPath string) error
	Exists(ctx context.Context, relPath string) (bool, error)
	MkdirAll(ctx context.Context, relPath string) error
}
