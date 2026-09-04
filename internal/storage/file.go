package storage

import (
	"context"
	"errors"
	"time"
)

var ErrFileNotFound = errors.New("storage: archivo no encontrado")

// FileMeta son los metadatos persistidos de un archivo (§9: la base de
// datos solo guarda metadatos; el contenido vive en el Provider).
type FileMeta struct {
	ID         string
	PoolID     string
	OwnerID    string
	ParentPath string // ruta lógica dentro del árbol del propietario, ej. "/Documentos"
	Name       string
	SizeBytes  int64
	SHA256     string
	MimeType   string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type FileRepository interface {
	// UpsertFile inserta o actualiza (por clave natural pool_id+owner_id+
	// parent_path+name, §498-507 versionado simple de Fase 1: sobrescribe).
	// Si ya existía una fila, meta.ID y meta.CreatedAt se rellenan con los
	// valores reales tras la operación.
	UpsertFile(ctx context.Context, meta *FileMeta) error
	GetFileByID(ctx context.Context, id string) (*FileMeta, error)
	ListFiles(ctx context.Context, ownerID, parentPath string) ([]*FileMeta, error)
	DeleteFile(ctx context.Context, id string) error
}
