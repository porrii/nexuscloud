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
	ErrForbidden           = errors.New("storage: no tienes permiso sobre este elemento")
	ErrInvalidName         = errors.New("storage: nombre de archivo/carpeta inválido")
	ErrNameOccupiedByTrash = errors.New("storage: ya hay un elemento con ese nombre en la papelera; restáuralo, elimínalo definitivamente o usa otro nombre")
)

// invalidNameChars cubre los caracteres prohibidos en nombres de archivo de
// Windows además de separadores y caracteres de control (§179: soportar
// Unicode/español mientras se rechazan los que rompen alguna plataforma).
var invalidNameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// FileService es el ÚNICO punto de acceso a archivos de usuario: valida
// autorización (propiedad) y la ruta lógica antes de delegar en Provider.
// Ningún otro paquete debe tocar Provider directamente (§195-196).
type FileService struct {
	files        FileRepository
	directories  DirectoryRepository
	pools        PoolRepository
	provider     Provider
	trashEnabled bool
}

func NewFileService(files FileRepository, directories DirectoryRepository, pools PoolRepository, provider Provider, trashEnabled bool) *FileService {
	return &FileService{files: files, directories: directories, pools: pools, provider: provider, trashEnabled: trashEnabled}
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
// activo existente sobrescribe (versionado real llega en Fase 2, §498-507);
// si el path está ocupado por un elemento en la papelera, se rechaza en vez
// de resucitarlo/sobrescribirlo en silencio (§128).
func (s *FileService) Upload(ctx context.Context, in UploadInput) (*FileMeta, error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	parent := normalizeParentPath(in.ParentPath)

	pool, err := s.pools.DefaultPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolviendo storage pool por defecto: %w", err)
	}

	if err := s.rejectIfTrashOccupiesName(ctx, pool.ID, in.OwnerID, parent, in.Name); err != nil {
		return nil, err
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

func (s *FileService) rejectIfTrashOccupiesName(ctx context.Context, poolID, ownerID, parentPath, name string) error {
	if existing, err := s.files.GetFileByNaturalKey(ctx, poolID, ownerID, parentPath, name); err == nil && existing.IsTrashed() {
		return ErrNameOccupiedByTrash
	}
	if existing, err := s.directories.GetDirectoryByNaturalKey(ctx, poolID, ownerID, parentPath, name); err == nil && existing.IsTrashed() {
		return ErrNameOccupiedByTrash
	}
	return nil
}

// Download exige que requesterID sea el propietario del archivo (§198
// IDOR): conocer el ID no basta. Un archivo en la papelera sigue siendo
// descargable por su propietario (solo deja de listarse).
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

// Delete mueve el archivo a la papelera (§16) cuando está activada; con la
// papelera desactivada, borra directamente y para siempre.
func (s *FileService) Delete(ctx context.Context, requesterID, fileID string) error {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if meta.OwnerID != requesterID {
		return ErrForbidden
	}

	if !s.trashEnabled {
		return s.permanentlyDeleteFile(ctx, meta)
	}
	return s.files.SoftDeleteFile(ctx, meta.ID, time.Now().UTC())
}

// PermanentlyDeleteFile borra el archivo para siempre sin pasar por la
// papelera (§16 "eliminar definitivamente"), exigiendo propiedad igual que
// el resto de operaciones (§198).
func (s *FileService) PermanentlyDeleteFile(ctx context.Context, requesterID, fileID string) error {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if meta.OwnerID != requesterID {
		return ErrForbidden
	}
	return s.permanentlyDeleteFile(ctx, meta)
}

func (s *FileService) permanentlyDeleteFile(ctx context.Context, meta *FileMeta) error {
	rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
	if err := s.provider.Delete(ctx, rel); err != nil {
		return fmt.Errorf("eliminando contenido: %w", err)
	}
	return s.files.DeleteFile(ctx, meta.ID)
}

// RestoreFile saca un archivo de la papelera. Falla con un error claro si
// mientras tanto se subió/creó otro elemento con el mismo nombre (la
// restricción UNIQUE de la base de datos lo impide, ver migración 0003).
func (s *FileService) RestoreFile(ctx context.Context, requesterID, fileID string) error {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if meta.OwnerID != requesterID {
		return ErrForbidden
	}
	if err := s.files.RestoreFile(ctx, fileID); err != nil {
		if isUniqueViolation(err) {
			return ErrNameOccupiedByTrash
		}
		return err
	}
	return nil
}

// ListResult combina subcarpetas y archivos activos de una misma ruta
// lógica, tal como los mostraría un explorador de archivos real.
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

// TrashResult agrupa el contenido de la papelera de un usuario (vista
// plana, sin jerarquía -- igual que la papelera de Windows/Google Drive).
type TrashResult struct {
	Directories []*Directory
	Files       []*FileMeta
}

func (s *FileService) ListTrash(ctx context.Context, ownerID string) (*TrashResult, error) {
	dirs, err := s.directories.ListTrashedDirectories(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listando papelera de carpetas: %w", err)
	}
	files, err := s.files.ListTrashedFiles(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listando papelera de archivos: %w", err)
	}
	return &TrashResult{Directories: dirs, Files: files}, nil
}

// Mkdir crea la carpeta física y su registro de metadata (§13). Es
// idempotente para carpetas activas (crear una ya existente no es error,
// igual que os.MkdirAll); si el nombre está ocupado por algo en la
// papelera, se rechaza en vez de reutilizarlo en silencio (§128).
func (s *FileService) Mkdir(ctx context.Context, ownerID, parentPath, name string) (*Directory, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	parent := normalizeParentPath(parentPath)

	pool, err := s.pools.DefaultPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolviendo storage pool por defecto: %w", err)
	}
	if err := s.rejectIfTrashOccupiesName(ctx, pool.ID, ownerID, parent, name); err != nil {
		return nil, err
	}

	rel := physicalPath(ownerID, parent, name)
	if err := s.provider.MkdirAll(ctx, rel); err != nil {
		return nil, fmt.Errorf("creando carpeta física: %w", err)
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
	// CreateDirectory es DO NOTHING en conflicto: si ya existía, releemos
	// la fila real para devolver su ID verdadero en vez del recién generado.
	real, err := s.directories.GetDirectoryByNaturalKey(ctx, pool.ID, ownerID, parent, name)
	if err != nil {
		return nil, err
	}
	return real, nil
}

// DeleteDirectory solo permite borrar carpetas vacías -- de contenido
// ACTIVO; una carpeta con solo elementos ya en la papelera cuenta como
// vacía -- (§184-185). Exige propiedad (§198). Mueve a la papelera (junto
// con su marcador físico) cuando está activada; si no, borra para siempre.
func (s *FileService) DeleteDirectory(ctx context.Context, requesterID, dirID string) error {
	target, err := s.directories.GetDirectoryByID(ctx, dirID)
	if err != nil {
		return err
	}
	if target.OwnerID != requesterID {
		return ErrForbidden
	}
	if err := s.ensureDirectoryEmpty(ctx, requesterID, target); err != nil {
		return err
	}

	if !s.trashEnabled {
		return s.permanentlyDeleteDirectory(ctx, target)
	}

	rel := physicalPath(target.OwnerID, target.ParentPath, target.Name)
	if err := s.provider.Delete(ctx, rel); err != nil {
		return fmt.Errorf("eliminando carpeta física: %w", err)
	}
	return s.directories.SoftDeleteDirectory(ctx, target.ID, time.Now().UTC())
}

func (s *FileService) PermanentlyDeleteDirectory(ctx context.Context, requesterID, dirID string) error {
	target, err := s.directories.GetDirectoryByID(ctx, dirID)
	if err != nil {
		return err
	}
	if target.OwnerID != requesterID {
		return ErrForbidden
	}
	if !target.IsTrashed() {
		if err := s.ensureDirectoryEmpty(ctx, requesterID, target); err != nil {
			return err
		}
	}
	return s.permanentlyDeleteDirectory(ctx, target)
}

func (s *FileService) permanentlyDeleteDirectory(ctx context.Context, target *Directory) error {
	if !target.IsTrashed() {
		// Si estaba activa (papelera desactivada), el marcador físico
		// todavía existe y hay que retirarlo; si ya estaba en la papelera,
		// DeleteDirectory ya lo hizo al trashearla.
		rel := physicalPath(target.OwnerID, target.ParentPath, target.Name)
		if err := s.provider.Delete(ctx, rel); err != nil {
			return fmt.Errorf("eliminando carpeta física: %w", err)
		}
	}
	return s.directories.DeleteDirectory(ctx, target.ID)
}

func (s *FileService) ensureDirectoryEmpty(ctx context.Context, ownerID string, target *Directory) error {
	childPath := path.Join(target.ParentPath, target.Name)
	subdirs, err := s.directories.ListDirectories(ctx, ownerID, childPath)
	if err != nil {
		return fmt.Errorf("comprobando contenido de la carpeta: %w", err)
	}
	files, err := s.files.ListFiles(ctx, ownerID, childPath)
	if err != nil {
		return fmt.Errorf("comprobando contenido de la carpeta: %w", err)
	}
	if len(subdirs) > 0 || len(files) > 0 {
		return ErrDirectoryNotEmpty
	}
	return nil
}

// RestoreDirectory saca una carpeta de la papelera y recrea su marcador
// físico (idempotente). Falla con un error claro si el nombre ya fue
// reutilizado mientras tanto.
func (s *FileService) RestoreDirectory(ctx context.Context, requesterID, dirID string) error {
	target, err := s.directories.GetDirectoryByID(ctx, dirID)
	if err != nil {
		return err
	}
	if target.OwnerID != requesterID {
		return ErrForbidden
	}
	if err := s.directories.RestoreDirectory(ctx, dirID); err != nil {
		if isUniqueViolation(err) {
			return ErrNameOccupiedByTrash
		}
		return err
	}
	rel := physicalPath(target.OwnerID, target.ParentPath, target.Name)
	return s.provider.MkdirAll(ctx, rel)
}

// PurgeExpiredTrash elimina para siempre cualquier archivo/carpeta que
// lleve en la papelera más tiempo que retention (§16 "limpieza
// automática"). Pensado para invocarse periódicamente desde un ticker en
// segundo plano (internal/server) y también bajo demanda.
func (s *FileService) PurgeExpiredTrash(ctx context.Context, retention time.Duration) (purgedFiles, purgedDirs int, err error) {
	cutoff := time.Now().UTC().Add(-retention)

	files, err := s.files.ListFilesDeletedBefore(ctx, cutoff)
	if err != nil {
		return 0, 0, fmt.Errorf("listando archivos expirados: %w", err)
	}
	for _, f := range files {
		if err := s.permanentlyDeleteFile(ctx, f); err != nil {
			return purgedFiles, purgedDirs, fmt.Errorf("purgando archivo %s: %w", f.ID, err)
		}
		purgedFiles++
	}

	dirs, err := s.directories.ListDirectoriesDeletedBefore(ctx, cutoff)
	if err != nil {
		return purgedFiles, purgedDirs, fmt.Errorf("listando carpetas expiradas: %w", err)
	}
	for _, d := range dirs {
		// El marcador físico ya se retiró al trashear la carpeta (ver
		// DeleteDirectory); aquí solo queda limpiar la fila.
		if err := s.directories.DeleteDirectory(ctx, d.ID); err != nil {
			return purgedFiles, purgedDirs, fmt.Errorf("purgando carpeta %s: %w", d.ID, err)
		}
		purgedDirs++
	}
	return purgedFiles, purgedDirs, nil
}
