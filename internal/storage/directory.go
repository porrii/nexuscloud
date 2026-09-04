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
}

type DirectoryRepository interface {
	CreateDirectory(ctx context.Context, d *Directory) error
	ListDirectories(ctx context.Context, ownerID, parentPath string) ([]*Directory, error)
	GetDirectoryByID(ctx context.Context, id string) (*Directory, error)
	DeleteDirectory(ctx context.Context, id string) error
}
