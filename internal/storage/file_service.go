package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

var (
	ErrForbidden   = errors.New("storage: no tienes permiso sobre este archivo")
	ErrInvalidName = errors.New("storage: nombre de archivo/carpeta inválido")
)

// invalidNameChars cubre los caracteres prohibidos en nombres de archivo de
// Windows además de separadores y caracteres de control (§179: soportar
// Unicode/español mientras se rechazan los que rompen alguna plataforma).
var invalidNameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// FileService es el ÚNICO punto de acceso a archivos de usuario: valida
// autorización (propiedad) y la ruta lógica antes de delegar en Provider.
// Ningún otro paquete debe tocar Provider directamente (§195-196).
type FileService struct {
	files    FileRepository
	pools    PoolRepository
	provider Provider
}

func NewFileService(files FileRepository, pools PoolRepository, provider Provider) *FileService {
	return &FileService{files: files, pools: pools, provider: provider}
}

func validateName(name string) error {
	if name == "" || name == "." || name == ".." || invalidNameChars.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}

// normalizeParentPath fuerza una ruta lógica absoluta y limpia (usa "path",
// no "path/filepath": el árbol lógico de usuario siempre usa "/" con
// independencia del SO del servidor).
func normalizeParentPath(p string) string {
	if p == "" {
		return "/"
	}
	return path.Clean("/" + p)
}

// physicalPath aísla el árbol de cada propietario dentro del Provider, de
// forma que dos usuarios nunca puedan colisionar ni alcanzar el árbol del
// otro a través del filesystem físico, incluso antes de que exista
// compartición (§195).
func physicalPath(ownerID, parentPath, name string) string {
	return path.Join("/", ownerID, parentPath, name)
}

type UploadInput struct {
	OwnerID    string
	ParentPath string
	Name       string
	Content    io.Reader
}

// Upload valida nombre/ruta, escribe el contenido de forma atómica vía el
// Provider y solo entonces persiste los metadatos. Subir al mismo path
// existente sobrescribe (versionado real llega en Fase 2, §498-507).
func (s *FileService) Upload(ctx context.Context, in UploadInput) (*FileMeta, error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	parent := normalizeParentPath(in.ParentPath)

	pool, err := s.pools.DefaultPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolviendo storage pool por defecto: %w", err)
	}

	rel := physicalPath(in.OwnerID, parent, in.Name)
	size, sha, err := s.provider.Write(ctx, rel, in.Content)
	if err != nil {
		return nil, fmt.Errorf("escribiendo archivo: %w", err)
	}

	now := time.Now().UTC()
	meta := &FileMeta{
		ID:         idgen.New(),
		PoolID:     pool.ID,
		OwnerID:    in.OwnerID,
		ParentPath: parent,
		Name:       in.Name,
		SizeBytes:  size,
		SHA256:     sha,
		MimeType:   detectMimeType(in.Name),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.files.UpsertFile(ctx, meta); err != nil {
		_ = s.provider.Delete(ctx, rel) // best-effort: no dejar contenido huérfano si falla el metadato
		return nil, err
	}
	return meta, nil
}

// Download exige que requesterID sea el propietario del archivo (§198
// IDOR): conocer el ID no basta.
func (s *FileService) Download(ctx context.Context, requesterID, fileID string) (*FileMeta, io.ReadCloser, error) {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, nil, err
	}
	if meta.OwnerID != requesterID {
		return nil, nil, ErrForbidden
	}
	rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
	rc, err := s.provider.Read(ctx, rel)
	if err != nil {
		return nil, nil, fmt.Errorf("leyendo archivo: %w", err)
	}
	return meta, rc, nil
}

func (s *FileService) Delete(ctx context.Context, requesterID, fileID string) error {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if meta.OwnerID != requesterID {
		return ErrForbidden
	}
	rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
	if err := s.provider.Delete(ctx, rel); err != nil {
		return fmt.Errorf("eliminando contenido: %w", err)
	}
	return s.files.DeleteFile(ctx, fileID)
}

func (s *FileService) List(ctx context.Context, ownerID, parentPath string) ([]*FileMeta, error) {
	return s.files.ListFiles(ctx, ownerID, normalizeParentPath(parentPath))
}

func (s *FileService) Mkdir(ctx context.Context, ownerID, parentPath, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	rel := physicalPath(ownerID, normalizeParentPath(parentPath), name)
	return s.provider.MkdirAll(ctx, rel)
}
