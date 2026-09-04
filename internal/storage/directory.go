package storage

import (
	"context"
	"errors"
	"time"
)

var (
	ErrDirectoryNotFound = errors.New("storage: carpeta no encontrada")
	ErrDirectoryNotEmpty = errors.New("storage: la carpeta no está vacía")
)

// Directory es un registro explícito de carpeta (§13): existe para poder
// listar subcarpetas -- incluidas las vacías -- igual que se listan
// archivos. El contenido real sigue viviendo únicamente en el filesystem;
// esto es solo metadata de navegación.
type Directory struct {
	ID         string
	PoolID     string
	OwnerID    string
	ParentPath string
	Name       string
	CreatedAt  time.Time
	DeletedAt  *time.Time // no nil = en la papelera (§16)
}

func (d *Directory) IsTrashed() bool { return d.DeletedAt != nil }

type DirectoryRepository interface {
	CreateDirectory(ctx context.Context, d *Directory) error
	// GetDirectoryByNaturalKey no filtra por deleted_at: ver el porqué en
	// FileRepository.GetFileByNaturalKey (§128).
	GetDirectoryByNaturalKey(ctx context.Context, poolID, ownerID, parentPath, name string) (*Directory, error)
	// ListDirectories devuelve solo carpetas activas de esa ruta.
	ListDirectories(ctx context.Context, ownerID, parentPath string) ([]*Directory, error)
	ListTrashedDirectories(ctx context.Context, ownerID string) ([]*Directory, error)
	GetDirectoryByID(ctx context.Context, id string) (*Directory, error)
	SoftDeleteDirectory(ctx context.Context, id string, deletedAt time.Time) error
	RestoreDirectory(ctx context.Context, id string) error
	DeleteDirectory(ctx context.Context, id string) error
	ListDirectoriesDeletedBefore(ctx context.Context, cutoff time.Time) ([]*Directory, error)
}
