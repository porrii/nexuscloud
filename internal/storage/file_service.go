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
	files       FileRepository
	directories DirectoryRepository
	pools       PoolRepository
	provider    Provider
}

func NewFileService(files FileRepository, directories DirectoryRepository, pools PoolRepository, provider Provider) *FileService {
	return &FileService{files: files, directories: directories, pools: pools, provider: provider}
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

// ListResult combina subcarpetas y archivos de una misma ruta lógica, tal
// como los mostraría un explorador de archivos real.
type ListResult struct {
	Directories []*Directory
	Files       []*FileMeta
}

func (s *FileService) List(ctx context.Context, ownerID, parentPath string) (*ListResult, error) {
	parent := normalizeParentPath(parentPath)

	dirs, err := s.directories.ListDirectories(ctx, ownerID, parent)
	if err != nil {
		return nil, fmt.Errorf("listando carpetas: %w", err)
	}
	files, err := s.files.ListFiles(ctx, ownerID, parent)
	if err != nil {
		return nil, fmt.Errorf("listando archivos: %w", err)
	}
	return &ListResult{Directories: dirs, Files: files}, nil
}

// Mkdir crea la carpeta física y su registro de metadata (§13). Es
// idempotente: crear una carpeta ya existente no es un error, igual que
// os.MkdirAll.
func (s *FileService) Mkdir(ctx context.Context, ownerID, parentPath, name string) (*Directory, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	parent := normalizeParentPath(parentPath)
	rel := physicalPath(ownerID, parent, name)
	if err := s.provider.MkdirAll(ctx, rel); err != nil {
		return nil, fmt.Errorf("creando carpeta física: %w", err)
	}

	pool, err := s.pools.DefaultPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolviendo storage pool por defecto: %w", err)
	}
	dir := &Directory{
		ID:         idgen.New(),
		PoolID:     pool.ID,
		OwnerID:    ownerID,
		ParentPath: parent,
		Name:       name,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.directories.CreateDirectory(ctx, dir); err != nil {
		return nil, err
	}
	return dir, nil
}

// DeleteDirectory solo permite borrar carpetas vacías (sin archivos ni
// subcarpetas dentro) -- semántica equivalente a rmdir, no a "rm -rf" --
// para no perder datos por accidente sin una confirmación explícita de
// cada elemento (§184-185). Exige propiedad igual que Download/Delete
// (§198 IDOR).
func (s *FileService) DeleteDirectory(ctx context.Context, requesterID, dirID string) error {
	target, err := s.directories.GetDirectoryByID(ctx, dirID)
	if err != nil {
		return err
	}
	if target.OwnerID != requesterID {
		return ErrForbidden
	}

	childPath := path.Join(target.ParentPath, target.Name)
	subdirs, err := s.directories.ListDirectories(ctx, requesterID, childPath)
	if err != nil {
		return fmt.Errorf("comprobando contenido de la carpeta: %w", err)
	}
	files, err := s.files.ListFiles(ctx, requesterID, childPath)
	if err != nil {
		return fmt.Errorf("comprobando contenido de la carpeta: %w", err)
	}
	if len(subdirs) > 0 || len(files) > 0 {
		return ErrDirectoryNotEmpty
	}

	rel := physicalPath(requesterID, target.ParentPath, target.Name)
	if err := s.provider.Delete(ctx, rel); err != nil {
		return fmt.Errorf("eliminando carpeta física: %w", err)
	}
	return s.directories.DeleteDirectory(ctx, target.ID)
}
