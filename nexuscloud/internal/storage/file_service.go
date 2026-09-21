package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

var (
	ErrForbidden           = errors.New("storage: no tienes permiso sobre este elemento")
	ErrInvalidName         = errors.New("storage: nombre de archivo/carpeta inválido")
	ErrNameOccupiedByTrash = errors.New("storage: ya hay un elemento con ese nombre en la papelera; restáuralo, elimínalo definitivamente o usa otro nombre")
	// ErrDestinationOccupied (ADR-030): a diferencia de Upload (que
	// sobrescribe un archivo activo del mismo nombre por diseño, §498-507),
	// Move siempre rechaza un destino ya ocupado -- activo o en papelera --
	// en vez de fusionar dos semánticas distintas ("mover" y "sobrescribir")
	// en una sola operación.
	ErrDestinationOccupied = errors.New("storage: ya hay un archivo o carpeta con ese nombre en el destino")
	// ErrInvalidMoveDestination (ADR-030): mover una carpeta dentro de sí
	// misma o de uno de sus propios descendientes es un ciclo sin sentido
	// en el árbol -- se rechaza explícitamente en vez de dejar que el
	// UPDATE de reescritura de prefijos produzca un resultado indefinido.
	ErrInvalidMoveDestination = errors.New("storage: no se puede mover una carpeta dentro de sí misma ni de una de sus subcarpetas")
)

// invalidNameChars cubre los caracteres prohibidos en nombres de archivo de
// Windows además de separadores y caracteres de control (§179: soportar
// Unicode/español mientras se rechazan los que rompen alguna plataforma).
var invalidNameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// FileService es el ÚNICO punto de acceso a archivos de usuario: valida
// autorización (propiedad) y la ruta lógica antes de delegar en Provider.
// Ningún otro paquete debe tocar Provider directamente (§195-196).
type FileService struct {
	files                     FileRepository
	directories               DirectoryRepository
	versions                  VersionRepository
	shares                    ShareRepository
	pools                     PoolRepository
	providers                 ProviderResolver
	hasher                    PasswordHasher
	trashEnabled              bool
	versioningEnabled         bool
	maxVersionsPerFile        int
	maxVersionAgeDays         int
	maxVersionsTotalSizeBytes int64
	sharingEnabled            bool
	publicLinksEnabled        bool

	// Cuotas (ADR-036): nil = sin cuotas, ver WithQuotas.
	quotaResolver QuotaResolver
	usage         UsageRepository
	owners        ownerLocks
}

func NewFileService(
	files FileRepository, directories DirectoryRepository, versions VersionRepository, shares ShareRepository,
	pools PoolRepository, providers ProviderResolver, hasher PasswordHasher,
	trashEnabled, versioningEnabled bool, maxVersionsPerFile, maxVersionAgeDays int, maxVersionsTotalSizeBytes int64,
	sharingEnabled, publicLinksEnabled bool,
	opts ...FileServiceOption,
) *FileService {
	s := &FileService{
		files: files, directories: directories, versions: versions, shares: shares,
		pools: pools, providers: providers, hasher: hasher,
		trashEnabled: trashEnabled, versioningEnabled: versioningEnabled, maxVersionsPerFile: maxVersionsPerFile,
		maxVersionAgeDays: maxVersionAgeDays, maxVersionsTotalSizeBytes: maxVersionsTotalSizeBytes,
		sharingEnabled: sharingEnabled, publicLinksEnabled: publicLinksEnabled,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// TrashEnabled indica si borrar es lógico (a la papelera, que reserva el
// nombre, §128) o físico. Los adaptadores como WebDAV lo necesitan para saber
// si tras borrar un elemento su nombre sigue ocupado.
func (s *FileService) TrashEnabled() bool { return s.trashEnabled }

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

// stagingPath es una ubicación temporal fuera del árbol lógico visible,
// usada para escribir (y poder hashear) el contenido entrante de Upload
// antes de decidir si hay que versionar el contenido anterior.
func stagingPath(ownerID string) string {
	return path.Join("/", ownerID, ".nexuscloud-staging", idgen.New())
}

// versionStoragePath ubica el contenido de una versión superada, fuera del
// árbol lógico visible del usuario (§15).
func versionStoragePath(ownerID, fileID string, versionNum int) string {
	return path.Join("/", ownerID, ".nexuscloud-versions", fileID, strconv.Itoa(versionNum))
}

type UploadInput struct {
	OwnerID    string
	ParentPath string
	Name       string
	Content    io.Reader
	// PoolID, si no está vacío, fija el pool destino explícitamente (p.ej.
	// restaurar un backup a un pool concreto, que puede no ser el por
	// defecto). Vacío (el caso normal) resuelve el pool por defecto, mismo
	// comportamiento de siempre -- ver resolveTargetPool.
	PoolID string
	// NoOverwrite hace que Upload falle con ErrDestinationOccupied si ya hay
	// un archivo ACTIVO con ese nombre en esa carpeta, en vez de sobrescribirlo
	// (dejando una versión). Es lo que necesita quien sube a una carpeta que
	// NO es suya (§40: no sobrescribir silenciosamente).
	NoOverwrite bool
	// SizeHint es el tamaño que anuncia quien sube (Content-Length; 0 o
	// negativo = desconocido). Solo sirve para rechazar por cuota ANTES de
	// leer el cuerpo: no se fía de él, el tamaño real lo da lo escrito.
	SizeHint int64
	// SkipQuota exime la subida de la cuota del propietario (ADR-036). Solo
	// para restaurar un backup, que no debe fallar porque la política cambiara.
	SkipQuota bool
}

// resolveTargetPool centraliza la resolución de pool destino que Upload y
// Mkdir necesitan por igual: sin poolID explícito, el por defecto (todo el
// comportamiento anterior a esto, intacto); con uno, ese pool concreto --
// necesario para reinsertar un backup en un pool que puede no ser el por
// defecto (§18, ADR-025).
func (s *FileService) resolveTargetPool(ctx context.Context, poolID string) (*Pool, error) {
	if poolID == "" {
		return s.pools.DefaultPool(ctx)
	}
	return s.pools.GetPoolByID(ctx, poolID)
}

// Upload valida nombre/ruta y escribe primero a una ubicación provisional
// para poder conocer el hash del contenido entrante sin perder todavía el
// contenido anterior (necesario para decidir si versionarlo). Si el path
// está ocupado por un elemento en la papelera, se rechaza en vez de
// resucitarlo/sobrescribirlo en silencio (§128).
func (s *FileService) Upload(ctx context.Context, in UploadInput) (*FileMeta, error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	parent := normalizeParentPath(in.ParentPath)

	pool, err := s.resolveTargetPool(ctx, in.PoolID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo storage pool destino: %w", err)
	}
	prov, err := s.providers.For(ctx, pool.ID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}

	if err := s.rejectIfTrashOccupiesName(ctx, pool.ID, in.OwnerID, parent, in.Name); err != nil {
		return nil, err
	}
	if in.NoOverwrite {
		// Antes de escribir el temporal: un archivo grande que se va a
		// rechazar no debe gastar E/S. Sigue siendo una comprobación previa,
		// no atómica: dos subidas simultáneas con el mismo nombre pueden
		// cruzarse, y entonces manda el comportamiento normal (versión).
		existing, err := s.files.GetFileByNaturalKey(ctx, pool.ID, in.OwnerID, parent, in.Name)
		switch {
		case err == nil && !existing.IsTrashed():
			return nil, ErrDestinationOccupied
		case err != nil && !errors.Is(err, ErrFileNotFound):
			return nil, err
		}
	}

	// Cuota (ADR-036): antes de leer el cuerpo se decide cuánto cabe; si el
	// tamaño anunciado ya no cabe se rechaza sin leer nada, y si no se anuncia
	// la lectura se corta en cuanto se pasa de lo que cabe. La decisión
	// definitiva se toma más abajo, con el contenido ya escrito.
	content := in.Content
	gate, err := s.beginQuota(ctx, in, pool.ID, parent)
	if err != nil {
		return nil, err
	}
	if gate != nil {
		content = &errLimitReader{r: content, remaining: gate.avail, err: ErrQuotaExceeded}
	}

	staging := stagingPath(in.OwnerID)
	size, sha, err := prov.Write(ctx, staging, content)
	if err != nil {
		return nil, fmt.Errorf("escribiendo archivo: %w", err)
	}
	movedToFinal := false
	defer func() {
		if !movedToFinal {
			_ = prov.Delete(ctx, staging) // best-effort: limpia el staging si algo falló después
		}
	}()

	// Con cuota, el tramo de confirmar (comprobar + mover + registrar) va
	// bajo el cerrojo del propietario: así dos subidas simultáneas no pueden
	// pasarse las dos de la cuota. La lectura del cuerpo, lo lento, ya acabó.
	if gate != nil {
		unlock := s.owners.lock(in.OwnerID)
		defer unlock()
	}

	rel := physicalPath(in.OwnerID, parent, in.Name)
	existing, existingErr := s.files.GetFileByNaturalKey(ctx, pool.ID, in.OwnerID, parent, in.Name)
	hasActiveExisting := existingErr == nil && !existing.IsTrashed()

	if gate != nil {
		if err := s.checkQuotaAtCommit(ctx, gate, in.OwnerID, size, sha, existing, hasActiveExisting); err != nil {
			return nil, err
		}
	}

	if hasActiveExisting && s.versioningEnabled && existing.SHA256 != sha {
		if err := s.snapshotVersion(ctx, existing); err != nil {
			return nil, fmt.Errorf("guardando versión anterior: %w", err)
		}
	}

	if err := prov.Move(ctx, staging, rel); err != nil {
		return nil, fmt.Errorf("moviendo archivo a destino final: %w", err)
	}
	movedToFinal = true

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
		return nil, err
	}
	return meta, nil
}

// snapshotVersion aparta el contenido actualmente en rel(existing) a su
// propia ubicación versionada y registra la fila de historial, antes de que
// Upload lo sobrescriba. CreatedAt de la versión es existing.UpdatedAt: el
// momento en que ESE contenido pasó a ser la versión vigente, no "ahora".
func (s *FileService) snapshotVersion(ctx context.Context, existing *FileMeta) error {
	prov, err := s.providers.For(ctx, existing.PoolID)
	if err != nil {
		return fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	nextNum, err := s.versions.LatestVersionNum(ctx, existing.ID)
	if err != nil {
		return err
	}
	nextNum++

	oldRel := physicalPath(existing.OwnerID, existing.ParentPath, existing.Name)
	versionKey := versionStoragePath(existing.OwnerID, existing.ID, nextNum)
	if err := prov.Move(ctx, oldRel, versionKey); err != nil {
		return fmt.Errorf("apartando contenido anterior: %w", err)
	}

	v := &FileVersion{
		ID:         idgen.New(),
		FileID:     existing.ID,
		VersionNum: nextNum,
		SizeBytes:  existing.SizeBytes,
		SHA256:     existing.SHA256,
		MimeType:   existing.MimeType,
		StorageKey: versionKey,
		CreatedAt:  existing.UpdatedAt,
	}
	if err := s.versions.CreateVersion(ctx, v); err != nil {
		return err
	}

	return s.enforceMaxVersions(ctx, existing.PoolID, existing.ID)
}

// enforceMaxVersions purga las versiones del historial que sobren según las
// políticas activas (§15 "política automática de limpieza"): cantidad
// (MaxVersionsPerFile), antigüedad (MaxVersionAgeDays) y espacio total
// ocupado (MaxVersionsTotalSizeBytes). Cada política es independiente (0 =
// desactivada) y componen como "el más restrictivo gana" -- una versión se
// purga si CUALQUIER política activa lo pide, no solo si todas coinciden.
// Es la composición inversa a la retención de backups (ADR-017, "el más
// generoso gana"): aquí las tres políticas son límites de protección de
// espacio en disco, no una promesa de cuánto historial conservar -- ver
// ADR-024. El contenido de las versiones vive en el mismo pool que el
// fichero.
func (s *FileService) enforceMaxVersions(ctx context.Context, poolID, fileID string) error {
	if s.maxVersionsPerFile <= 0 && s.maxVersionAgeDays <= 0 && s.maxVersionsTotalSizeBytes <= 0 {
		return nil
	}
	prov, err := s.providers.For(ctx, poolID)
	if err != nil {
		return fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	versions, err := s.versions.ListVersions(ctx, fileID) // ya viene ordenado version_num DESC (más nueva primero)
	if err != nil {
		return err
	}

	var cutoff time.Time
	if s.maxVersionAgeDays > 0 {
		cutoff = time.Now().UTC().AddDate(0, 0, -s.maxVersionAgeDays)
	}

	var cumulativeBytes int64
	for i, v := range versions {
		cumulativeBytes += v.SizeBytes
		prune := false
		if s.maxVersionsPerFile > 0 {
			prune = prune || i >= s.maxVersionsPerFile
		}
		if s.maxVersionAgeDays > 0 {
			prune = prune || v.CreatedAt.Before(cutoff)
		}
		if s.maxVersionsTotalSizeBytes > 0 {
			prune = prune || cumulativeBytes > s.maxVersionsTotalSizeBytes
		}
		if !prune {
			continue
		}
		if err := prov.Delete(ctx, v.StorageKey); err != nil {
			return fmt.Errorf("purgando contenido de versión antigua: %w", err)
		}
		if err := s.versions.DeleteVersion(ctx, v.ID); err != nil {
			return err
		}
	}
	return nil
}

// ListVersions devuelve el historial de un archivo, más reciente primero.
// Exige propiedad (§198).
func (s *FileService) ListVersions(ctx context.Context, requesterID, fileID string) ([]*FileVersion, error) {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if meta.OwnerID != requesterID {
		return nil, ErrForbidden
	}
	return s.versions.ListVersions(ctx, fileID)
}

// DownloadVersion sirve el contenido de una versión concreta, no la actual.
func (s *FileService) DownloadVersion(ctx context.Context, requesterID, fileID string, versionNum int) (*FileVersion, io.ReadCloser, error) {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, nil, err
	}
	if meta.OwnerID != requesterID {
		return nil, nil, ErrForbidden
	}
	v, err := s.versions.GetVersion(ctx, fileID, versionNum)
	if err != nil {
		return nil, nil, err
	}
	prov, err := s.providers.For(ctx, meta.PoolID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	rc, err := prov.Read(ctx, v.StorageKey)
	if err != nil {
		return nil, nil, fmt.Errorf("leyendo versión: %w", err)
	}
	return v, rc, nil
}

// RestoreVersion hace que una versión antigua vuelva a ser el contenido
// vigente: la versión actual pasa a su vez a formar parte del historial
// (nunca se pierde), y la versión restaurada se retira de la tabla de
// versiones porque ahora es -de nuevo- el archivo activo.
func (s *FileService) RestoreVersion(ctx context.Context, requesterID, fileID string, versionNum int) (*FileMeta, error) {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if meta.OwnerID != requesterID {
		return nil, ErrForbidden
	}
	target, err := s.versions.GetVersion(ctx, fileID, versionNum)
	if err != nil {
		return nil, err
	}
	prov, err := s.providers.For(ctx, meta.PoolID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}

	if s.versioningEnabled {
		if err := s.snapshotVersion(ctx, meta); err != nil {
			return nil, fmt.Errorf("guardando versión actual antes de restaurar: %w", err)
		}
	} else {
		rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
		if err := prov.Delete(ctx, rel); err != nil {
			return nil, fmt.Errorf("descartando contenido actual: %w", err)
		}
	}

	rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
	if err := prov.Move(ctx, target.StorageKey, rel); err != nil {
		return nil, fmt.Errorf("restaurando versión: %w", err)
	}
	if err := s.versions.DeleteVersion(ctx, target.ID); err != nil {
		return nil, err
	}

	meta.SizeBytes = target.SizeBytes
	meta.SHA256 = target.SHA256
	meta.MimeType = target.MimeType
	meta.UpdatedAt = time.Now().UTC()
	if err := s.files.UpsertFile(ctx, meta); err != nil {
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

// rejectIfDestinationOccupied (ADR-030, usado por MoveFile/MoveDirectory):
// a diferencia de rejectIfTrashOccupiesName (que solo mira la papelera,
// porque Upload SÍ puede sobrescribir un archivo activo), Move rechaza el
// destino tanto si está ocupado por algo activo como por algo en la
// papelera -- nunca sobrescribe.
func (s *FileService) rejectIfDestinationOccupied(ctx context.Context, poolID, ownerID, parentPath, name string) error {
	if _, err := s.files.GetFileByNaturalKey(ctx, poolID, ownerID, parentPath, name); err == nil {
		return ErrDestinationOccupied
	} else if !errors.Is(err, ErrFileNotFound) {
		return err
	}
	if _, err := s.directories.GetDirectoryByNaturalKey(ctx, poolID, ownerID, parentPath, name); err == nil {
		return ErrDestinationOccupied
	} else if !errors.Is(err, ErrDirectoryNotFound) {
		return err
	}
	return nil
}

// Download exige que requesterID sea el propietario del archivo, o que
// tenga acceso vía un share de tipo user/group -- directo o a través de una
// carpeta ancestro compartida (§37) -- antes de caer en ErrForbidden (§198
// IDOR): conocer el ID no basta. Un archivo en la papelera sigue siendo
// descargable por su propietario (solo deja de listarse).
func (s *FileService) Download(ctx context.Context, requesterID, fileID string) (*FileMeta, io.ReadCloser, error) {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, nil, err
	}
	if meta.OwnerID != requesterID {
		ok, err := s.hasShareAccessToFile(ctx, requesterID, meta)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, ErrForbidden
		}
	}
	prov, err := s.providers.For(ctx, meta.PoolID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
	rc, err := prov.Read(ctx, rel)
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

// permanentlyDeleteFile también purga el historial de versiones (§15): no
// tiene sentido conservarlo cuando el archivo al que pertenece ya no existe.
func (s *FileService) permanentlyDeleteFile(ctx context.Context, meta *FileMeta) error {
	prov, err := s.providers.For(ctx, meta.PoolID)
	if err != nil {
		return fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
	if err := prov.Delete(ctx, rel); err != nil {
		return fmt.Errorf("eliminando contenido: %w", err)
	}

	versions, err := s.versions.ListVersions(ctx, meta.ID)
	if err != nil {
		return fmt.Errorf("listando versiones a purgar: %w", err)
	}
	for _, v := range versions {
		if err := prov.Delete(ctx, v.StorageKey); err != nil {
			return fmt.Errorf("eliminando contenido de versión %d: %w", v.VersionNum, err)
		}
	}
	if err := s.versions.DeleteAllVersions(ctx, meta.ID); err != nil {
		return fmt.Errorf("eliminando historial de versiones: %w", err)
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

// MoveFile (ADR-030, §85) reubica un archivo a una nueva carpeta y/o le
// cambia el nombre -- DENTRO del mismo pool, nunca cambia PoolID (mover
// entre pools movería bytes entre dos Provider distintos, una feature
// aparte que no encaja en un simple prov.Move; fuera de alcance aquí).
// newParentPath/newName nil significa "mantener el valor actual" (mismo
// espíritu que un rename() de filesystem: mover sin renombrar, o
// renombrar en el sitio, son casos válidos sin repetir lo que no cambia).
// Mismo rigor que Upload en la validación, pero nunca sobrescribe: un
// destino ocupado (activo o en papelera) se rechaza (ErrDestinationOccupied),
// nunca se fusiona con la semántica de "sobrescribir" de Upload. Al ser el
// MISMO archivo (mismo ID, nunca se recrea), su historial de versiones
// (indexado por ID, no por ruta) y sus shares sobreviven intactos sin
// ningún código adicional -- la ganancia real de mover de verdad frente a
// borrar+volver a subir.
func (s *FileService) MoveFile(ctx context.Context, requesterID, fileID string, newParentPath, newName *string) (*FileMeta, error) {
	meta, err := s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if meta.OwnerID != requesterID {
		return nil, ErrForbidden
	}
	if meta.IsTrashed() {
		return nil, ErrFileNotFound
	}
	name := meta.Name
	if newName != nil {
		name = *newName
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	parent := meta.ParentPath
	if newParentPath != nil {
		parent = normalizeParentPath(*newParentPath)
	}

	if parent == meta.ParentPath && name == meta.Name {
		return meta, nil
	}
	if err := s.rejectIfDestinationOccupied(ctx, meta.PoolID, meta.OwnerID, parent, name); err != nil {
		return nil, err
	}

	prov, err := s.providers.For(ctx, meta.PoolID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	oldRel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
	newRel := physicalPath(meta.OwnerID, parent, name)
	if err := prov.Move(ctx, oldRel, newRel); err != nil {
		return nil, fmt.Errorf("moviendo el contenido: %w", err)
	}
	if err := s.files.MoveFile(ctx, meta.ID, parent, name); err != nil {
		_ = prov.Move(ctx, newRel, oldRel) // best-effort: deshace el movimiento físico si la BD falla
		return nil, err
	}
	meta.ParentPath = parent
	meta.Name = name
	return meta, nil
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
// Mkdir crea una carpeta en poolID (vacío = pool por defecto, ver
// resolveTargetPool). Un solo nivel: quien llame es responsable de
// materializar los padres primero si hacen falta (mismo criterio que
// FilesRepository.createDirectory en el cliente Flutter, ADR-012 punto 8) --
// Mkdir en sí es idempotente (DO NOTHING en conflicto, ver más abajo), así
// que repetir la llamada para una carpeta ya existente es seguro.
func (s *FileService) Mkdir(ctx context.Context, ownerID, parentPath, name, poolID string) (*Directory, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	parent := normalizeParentPath(parentPath)

	pool, err := s.resolveTargetPool(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo storage pool destino: %w", err)
	}
	prov, err := s.providers.For(ctx, pool.ID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	if err := s.rejectIfTrashOccupiesName(ctx, pool.ID, ownerID, parent, name); err != nil {
		return nil, err
	}

	rel := physicalPath(ownerID, parent, name)
	if err := prov.MkdirAll(ctx, rel); err != nil {
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

// MoveDirectory (ADR-030, §85) reubica una carpeta y todo su árbol de
// descendientes -- archivos y subcarpetas, cualquier profundidad -- a una
// nueva ubicación y/o le cambia el nombre. Mismo criterio que MoveFile
// (nunca sobrescribe un destino ocupado, nunca cambia de pool), más una
// comprobación propia de las carpetas: no se puede mover una carpeta
// dentro de sí misma ni de una de sus propias subcarpetas (ciclo sin
// sentido en el árbol). El movimiento físico (`prov.Move` sobre el
// directorio completo, un solo `os.Rename` en el provider local -- nunca
// recorrido fichero a fichero) ocurre ANTES que la reescritura de la base
// de datos (`MoveDirectoryTree`, transaccional): si la base de datos
// fallara después, se deshace el movimiento físico en mejor esfuerzo
// antes de devolver el error, mismo criterio que MoveFile.
func (s *FileService) MoveDirectory(ctx context.Context, requesterID, dirID string, newParentPath, newName *string) (*Directory, error) {
	target, err := s.directories.GetDirectoryByID(ctx, dirID)
	if err != nil {
		return nil, err
	}
	if target.OwnerID != requesterID {
		return nil, ErrForbidden
	}
	if target.IsTrashed() {
		return nil, ErrDirectoryNotFound
	}
	name := target.Name
	if newName != nil {
		name = *newName
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	parent := target.ParentPath
	if newParentPath != nil {
		parent = normalizeParentPath(*newParentPath)
	}

	oldFullPath := path.Join(target.ParentPath, target.Name)
	newFullPath := path.Join(parent, name)
	if newFullPath == oldFullPath {
		return target, nil
	}
	if strings.HasPrefix(newFullPath+"/", oldFullPath+"/") {
		return nil, ErrInvalidMoveDestination
	}
	if err := s.rejectIfDestinationOccupied(ctx, target.PoolID, target.OwnerID, parent, name); err != nil {
		return nil, err
	}

	prov, err := s.providers.For(ctx, target.PoolID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	oldRel := physicalPath(target.OwnerID, target.ParentPath, target.Name)
	newRel := physicalPath(target.OwnerID, parent, name)
	if err := prov.Move(ctx, oldRel, newRel); err != nil {
		return nil, fmt.Errorf("moviendo la carpeta física: %w", err)
	}
	if err := s.directories.MoveDirectoryTree(ctx, target.PoolID, target.OwnerID, target.ID, oldFullPath, parent, name, newFullPath); err != nil {
		_ = prov.Move(ctx, newRel, oldRel) // best-effort: deshace el movimiento físico si la BD falla
		return nil, err
	}
	target.ParentPath = parent
	target.Name = name
	return target, nil
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

	prov, err := s.providers.For(ctx, target.PoolID)
	if err != nil {
		return fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	rel := physicalPath(target.OwnerID, target.ParentPath, target.Name)
	// Con la papelera activa, Delete de un archivo es solo una marca en la
	// base de datos: su contenido sigue físicamente en esta carpeta. Que
	// no esté vacía en disco es correcto (una carpeta con solo elementos en
	// la papelera "cuenta como vacía", ver ensureDirectoryEmpty) aunque
	// os.Remove falle con "directorio no vacío": se conserva, y
	// permanentlyDeleteDirectory la retirará cuando ya no quede nada dentro.
	if err := prov.Delete(ctx, rel); err != nil && !errors.Is(err, fs.ErrExist) {
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
	prov, err := s.providers.For(ctx, target.PoolID)
	if err != nil {
		return fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	rel := physicalPath(target.OwnerID, target.ParentPath, target.Name)
	if err := prov.Delete(ctx, rel); err != nil {
		// Una carpeta activa (papelera desactivada) siempre está vacía en
		// disco: cualquier fallo es real. Una que ya estaba en la papelera
		// puede seguir existiendo físicamente si al trashearla aún contenía
		// archivos en la papelera (ver DeleteDirectory); si esos archivos
		// todavía no se han purgado, "no vacía" es esperable y se ignora --
		// nunca es recursivo, así que jamás se llevaría su contenido por
		// delante.
		if !target.IsTrashed() || !errors.Is(err, fs.ErrExist) {
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
	prov, err := s.providers.For(ctx, target.PoolID)
	if err != nil {
		return fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	rel := physicalPath(target.OwnerID, target.ParentPath, target.Name)
	return prov.MkdirAll(ctx, rel)
}

// PurgeExpiredTrash elimina para siempre cualquier archivo/carpeta que
// lleve en la papelera más tiempo que retention (§16 "limpieza
// automática"), y además, si maxTotalSizeBytes > 0, cualquier archivo
// adicional que sobre para que el total ocupado por la papelera quepa en
// ese límite -- empezando por el borrado más antiguo. Las dos políticas
// componen como "el más restrictivo gana" (se poda si CUALQUIERA lo pide),
// no como la retención de backups (ADR-017, "el más generoso gana"): un
// límite de tamaño es protección de disco, no una promesa de cuánto
// conservar -- ver ADR-023. Las carpetas nunca pesan (solo se puede
// mover a papelera una carpeta ya vacía, ensureDirectoryEmpty), así que
// solo se purgan por antigüedad. Pensado para invocarse periódicamente
// desde un ticker en segundo plano (internal/server) y también bajo
// demanda.
func (s *FileService) PurgeExpiredTrash(ctx context.Context, retention time.Duration, maxTotalSizeBytes int64) (purgedFiles, purgedDirs int, err error) {
	cutoff := time.Now().UTC().Add(-retention)

	trashed, err := s.files.ListAllTrashedFiles(ctx) // ya viene ordenado deleted_at DESC (borrado más reciente primero)
	if err != nil {
		return 0, 0, fmt.Errorf("listando papelera para purga: %w", err)
	}
	var cumulativeBytes int64
	for _, f := range trashed {
		cumulativeBytes += f.SizeBytes
		prune := f.DeletedAt.Before(cutoff)
		if maxTotalSizeBytes > 0 {
			prune = prune || cumulativeBytes > maxTotalSizeBytes
		}
		if !prune {
			continue
		}
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
