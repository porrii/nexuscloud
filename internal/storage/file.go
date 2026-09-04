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
	DeletedAt  *time.Time // no nil = en la papelera (§16)
}

func (f *FileMeta) IsTrashed() bool { return f.DeletedAt != nil }

type FileRepository interface {
	// UpsertFile inserta o actualiza (por clave natural pool_id+owner_id+
	// parent_path+name, §498-507 versionado simple de Fase 1: sobrescribe).
	// Si ya existía una fila, meta.ID y meta.CreatedAt se rellenan con los
	// valores reales tras la operación.
	UpsertFile(ctx context.Context, meta *FileMeta) error
	GetFileByID(ctx context.Context, id string) (*FileMeta, error)
	// GetFileByNaturalKey busca por (pool,owner,parent_path,name) SIN
	// filtrar por deleted_at: se usa antes de subir para detectar si el
	// destino está ocupado por un elemento en la papelera y evitar que un
	// upload lo resucite/sobrescriba en silencio (§128 protección
	// ransomware). Devuelve ErrFileNotFound si no existe ninguna fila.
	GetFileByNaturalKey(ctx context.Context, poolID, ownerID, parentPath, name string) (*FileMeta, error)
	// ListFiles devuelve solo archivos activos (deleted_at IS NULL) de esa
	// ruta. Usa ListTrash para ver los archivos en la papelera.
	ListFiles(ctx context.Context, ownerID, parentPath string) ([]*FileMeta, error)
	ListTrashedFiles(ctx context.Context, ownerID string) ([]*FileMeta, error)
	SoftDeleteFile(ctx context.Context, id string, deletedAt time.Time) error
	RestoreFile(ctx context.Context, id string) error
	// DeleteFile elimina la fila definitivamente (purga o papelera
	// desactivada). No toca el contenido físico: eso es responsabilidad
	// del llamador vía Provider.
	DeleteFile(ctx context.Context, id string) error
	// ListFilesDeletedBefore alimenta la purga automática por retención.
	ListFilesDeletedBefore(ctx context.Context, cutoff time.Time) ([]*FileMeta, error)
}
