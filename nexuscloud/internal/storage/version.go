package storage

import (
	"context"
	"errors"
	"time"
)

var ErrVersionNotFound = errors.New("storage: versión no encontrada")

// FileVersion es una versión anterior ya superada de un archivo (§15). El
// estado "actual" sigue viviendo únicamente en FileMeta; esto es solo
// historial. StorageKey es la ruta física (relativa al Provider) donde
// vive el contenido de esa versión concreta, fuera del árbol lógico visible
// del usuario.
type FileVersion struct {
	ID         string
	FileID     string
	VersionNum int
	SizeBytes  int64
	SHA256     string
	MimeType   string
	StorageKey string
	CreatedAt  time.Time
}

type VersionRepository interface {
	CreateVersion(ctx context.Context, v *FileVersion) error
	ListVersions(ctx context.Context, fileID string) ([]*FileVersion, error)
	GetVersion(ctx context.Context, fileID string, versionNum int) (*FileVersion, error)
	// LatestVersionNum devuelve el número de versión más alto ya usado para
	// fileID, o 0 si no tiene ninguna todavía.
	LatestVersionNum(ctx context.Context, fileID string) (int, error)
	DeleteVersion(ctx context.Context, id string) error
	// DeleteAllVersions se usa al borrar un archivo para siempre (§15: el
	// historial no sobrevive a su archivo).
	DeleteAllVersions(ctx context.Context, fileID string) error
}
