package storage

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// PasswordHasher hashea y verifica contraseñas de enlaces públicos (§37).
// internal/storage no puede importar internal/auth (regla de dependencia,
// docs/architecture.md), así que en vez de duplicar Argon2id aquí, se acepta
// esta interfaz local: *auth.Hasher ya la satisface estructuralmente sin
// ningún cambio en internal/auth, e internal/server (que ya construye un
// *auth.Hasher para el login) lo inyecta tal cual al construir FileService.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) error
}

// CreateShareInput describe una petición de compartición (§37). Exactamente
// uno de ResourceIsDirectory+ResourceID identifica el recurso; los campos
// específicos de cada Type que no aplican se ignoran (validados en
// CreateShare, no aquí).
type CreateShareInput struct {
	ResourceIsDirectory bool
	ResourceID          string
	Type                ShareType
	TargetUserID        string // ShareTypeUser
	TargetGroupID       string // ShareTypeGroup
	Label               string
	CanDownload         bool
	CanUpload           bool // solo sobre carpetas; en user/group significa "lectura + subida" (ADR-035)
	Password            string
	ExpiresAt           *time.Time
	MaxDownloads        *int
	MaxUploadSizeBytes  *int64
}

// CreateShare valida propiedad del recurso y la combinación de campos según
// Type, y persiste el share. Para Type == ShareTypeLink devuelve además el
// token en claro -- se muestra una única vez al llamador, igual que
// sesiones/invitaciones (§78) -- solo su hash se persiste.
func (s *FileService) CreateShare(ctx context.Context, ownerID string, in CreateShareInput) (*Share, string, error) {
	if !s.sharingEnabled {
		return nil, "", ErrSharingDisabled
	}

	// Un share de usuario o de grupo es siempre de lectura, con la subida como
	// añadido opcional ("lectura + subida", ADR-035): el "buzón" de solo subida
	// es cosa de enlaces (§37) y de la subida anónima (§38).
	switch in.Type {
	case ShareTypeUser:
		if in.TargetUserID == "" || !in.CanDownload {
			return nil, "", ErrInvalidShare
		}
	case ShareTypeGroup:
		if in.TargetGroupID == "" || !in.CanDownload {
			return nil, "", ErrInvalidShare
		}
	case ShareTypeLink:
		if !s.publicLinksEnabled {
			return nil, "", ErrPublicLinksDisabled
		}
	default:
		return nil, "", ErrInvalidShare
	}
	if !in.CanDownload && !in.CanUpload {
		return nil, "", ErrInvalidShare
	}
	if in.CanUpload && !in.ResourceIsDirectory {
		return nil, "", ErrInvalidShare // subir exige una carpeta destino, no un archivo concreto
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now().UTC()) {
		return nil, "", ErrInvalidShare
	}
	if in.MaxDownloads != nil && *in.MaxDownloads < 1 {
		return nil, "", ErrInvalidShare
	}
	if in.MaxUploadSizeBytes != nil && *in.MaxUploadSizeBytes < 1 {
		return nil, "", ErrInvalidShare
	}

	now := time.Now().UTC()
	share := &Share{
		ID:                 idgen.New(),
		OwnerID:            ownerID,
		Type:               in.Type,
		TargetUserID:       in.TargetUserID,
		TargetGroupID:      in.TargetGroupID,
		Label:              in.Label,
		CanDownload:        in.CanDownload,
		CanUpload:          in.CanUpload,
		ExpiresAt:          in.ExpiresAt,
		MaxDownloads:       in.MaxDownloads,
		MaxUploadSizeBytes: in.MaxUploadSizeBytes,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if in.ResourceIsDirectory {
		dir, err := s.directories.GetDirectoryByID(ctx, in.ResourceID)
		if err != nil {
			return nil, "", err
		}
		if dir.OwnerID != ownerID {
			return nil, "", ErrForbidden
		}
		share.DirectoryID = dir.ID
	} else {
		meta, err := s.files.GetFileByID(ctx, in.ResourceID)
		if err != nil {
			return nil, "", err
		}
		if meta.OwnerID != ownerID {
			return nil, "", ErrForbidden
		}
		share.FileID = meta.ID
	}

	if in.Password != "" {
		hash, err := s.hasher.Hash(in.Password)
		if err != nil {
			return nil, "", fmt.Errorf("hasheando contraseña de enlace: %w", err)
		}
		share.PasswordHash = hash
	}

	var plaintextToken string
	if in.Type == ShareTypeLink {
		token, err := idgen.Token()
		if err != nil {
			return nil, "", err
		}
		share.TokenHash = hashShareToken(token)
		plaintextToken = token
	}

	if err := s.shares.CreateShare(ctx, share); err != nil {
		return nil, "", err
	}
	return share, plaintextToken, nil
}

// ListSharesByMe devuelve los shares creados por ownerID ("compartido por
// mí", §143).
func (s *FileService) ListSharesByMe(ctx context.Context, ownerID string) ([]*Share, error) {
	return s.shares.ListSharesByOwner(ctx, ownerID)
}

// ListSharesWithMe devuelve los shares recibidos por userID, directos o vía
// grupo ("compartido conmigo", §143).
func (s *FileService) ListSharesWithMe(ctx context.Context, userID string) ([]*Share, error) {
	return s.shares.ListSharesForUser(ctx, userID, time.Now().UTC())
}

// RevokeShare exige que requesterID sea quien creó el share (§198). Revocar
// es un soft-update (revoked_at): el registro sigue existiendo para
// auditoría (audit_events), simplemente deja de conceder acceso.
func (s *FileService) RevokeShare(ctx context.Context, requesterID, shareID string) error {
	share, err := s.shares.GetShareByID(ctx, shareID)
	if err != nil {
		return err
	}
	if share.OwnerID != requesterID {
		return ErrForbidden
	}
	return s.shares.RevokeShare(ctx, shareID, time.Now().UTC())
}

// ShareResourceInfo resume el recurso compartido para mostrarlo en
// "compartido conmigo"/"compartido por mí" sin exponer FileMeta/Directory
// completos. Se puede llamar sin comprobar propiedad porque, para cuando se
// invoca, el propio Share ya demostró autorización (el llamador es su
// propietario, o ya salió de ListSharesForUser -- que ya filtró por
// destinatario).
func (s *FileService) ResourceInfoForShare(ctx context.Context, share *Share) (*ShareResourceInfo, error) {
	if share.IsDirectoryShare() {
		dir, err := s.directories.GetDirectoryByID(ctx, share.DirectoryID)
		if err != nil {
			return nil, err
		}
		return &ShareResourceInfo{Name: dir.Name, IsDirectory: true}, nil
	}
	meta, err := s.files.GetFileByID(ctx, share.FileID)
	if err != nil {
		return nil, err
	}
	return &ShareResourceInfo{Name: meta.Name, SizeBytes: meta.SizeBytes}, nil
}

type ShareResourceInfo struct {
	Name        string
	IsDirectory bool
	SizeBytes   int64 // 0 para carpetas
}

// hasShareAccessToFile reporta si requesterID puede descargar meta a través
// de algún share de tipo user/group -- directo sobre el archivo, o sobre
// cualquier carpeta ancestro (compartir una carpeta da acceso a todo su
// contenido, §37).
func (s *FileService) hasShareAccessToFile(ctx context.Context, requesterID string, meta *FileMeta) (bool, error) {
	if !s.sharingEnabled {
		return false, nil
	}
	ok, err := s.shares.HasFileAccess(ctx, requesterID, meta.ID, time.Now().UTC())
	if err != nil || ok {
		return ok, err
	}
	return s.hasShareAccessToAncestorDirectories(ctx, requesterID, meta.OwnerID, meta.ParentPath)
}

// hasShareAccessToDirectory es el equivalente de hasShareAccessToFile para
// navegar una carpeta compartida (§37: una subcarpeta sin share propio sigue
// siendo accesible si algún ancestro suyo sí lo tiene).
func (s *FileService) hasShareAccessToDirectory(ctx context.Context, requesterID string, dir *Directory) (bool, error) {
	if !s.sharingEnabled {
		return false, nil
	}
	ok, err := s.shares.HasDirectoryAccess(ctx, requesterID, dir.ID, time.Now().UTC())
	if err != nil || ok {
		return ok, err
	}
	return s.hasShareAccessToAncestorDirectories(ctx, requesterID, dir.OwnerID, dir.ParentPath)
}

// walkAncestorDirectories recorre las carpetas ancestro de logicalParentPath
// (bajo ownerID), de la más cercana a la raíz, llamando a visit con cada una
// que tenga registro propio; visit devuelve true para detener el recorrido.
// Bucle acotado por la profundidad de carpetas -- irrelevante a la escala
// objetivo de ~100 usuarios (§160); no hace falta cache ni tabla
// materializada (§162).
func (s *FileService) walkAncestorDirectories(ctx context.Context, ownerID, logicalParentPath string, visit func(*Directory) (stop bool, err error)) error {
	trimmed := strings.Trim(logicalParentPath, "/")
	if trimmed == "" {
		return nil // ya estamos en la raíz: no hay más ancestros que comprobar
	}
	segments := strings.Split(trimmed, "/")

	pool, err := s.pools.DefaultPool(ctx)
	if err != nil {
		return err
	}
	for i := len(segments); i >= 1; i-- {
		ancestorParent := "/" + strings.Join(segments[:i-1], "/")
		ancestorName := segments[i-1]
		dir, err := s.directories.GetDirectoryByNaturalKey(ctx, pool.ID, ownerID, ancestorParent, ancestorName)
		if err != nil {
			continue // esa carpeta ancestro no tiene registro propio; no es un error de acceso
		}
		if stop, err := visit(dir); err != nil || stop {
			return err
		}
	}
	return nil
}

// hasShareAccessToAncestorDirectories recorre las carpetas ancestro de
// logicalParentPath (bajo ownerID), de la más cercana a la raíz, buscando un
// share de carpeta que conceda acceso a requesterID.
func (s *FileService) hasShareAccessToAncestorDirectories(ctx context.Context, requesterID, ownerID, logicalParentPath string) (bool, error) {
	now := time.Now().UTC()
	found := false
	err := s.walkAncestorDirectories(ctx, ownerID, logicalParentPath, func(dir *Directory) (bool, error) {
		ok, err := s.shares.HasDirectoryAccess(ctx, requesterID, dir.ID, now)
		found = ok
		return ok, err
	})
	return found, err
}

// ListSharedDirectory lista el contenido de una carpeta a la que requesterID
// accede vía un share (directo o de un ancestro) en vez de por propiedad.
// Para bajar un nivel más, el llamador vuelve a invocar este método con el
// ID real de la subcarpeta (ya presente en ListResult.Directories) -- así la
// autorización se re-deriva de la base de datos en cada nivel, sin
// necesidad de un parámetro de sub-ruta que el cliente pudiera manipular.
func (s *FileService) ListSharedDirectory(ctx context.Context, requesterID, directoryID string) (*ListResult, error) {
	dir, err := s.directories.GetDirectoryByID(ctx, directoryID)
	if err != nil {
		return nil, err
	}
	if dir.OwnerID != requesterID {
		ok, err := s.hasShareAccessToDirectory(ctx, requesterID, dir)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrForbidden
		}
	}
	return s.List(ctx, dir.OwnerID, path.Join(dir.ParentPath, dir.Name))
}

// ResolvePublicShare busca un enlace por su token en claro (se hashea aquí
// para comparar contra token_hash). No comprueba revocación/expiración/
// agotamiento -- eso es responsabilidad del llamador vía
// Share.checkLifecycle, para poder distinguir un probe de metadata (que
// quiere mostrar el estado) de un intento real de acceso (que debe
// rechazarlo).
func (s *FileService) ResolvePublicShare(ctx context.Context, token string) (*Share, error) {
	if !s.publicLinksEnabled {
		return nil, ErrPublicLinksDisabled
	}
	return s.shares.GetShareByTokenHash(ctx, hashShareToken(token))
}

// verifySharePassword no distingue entre "sin contraseña" y "contraseña
// correcta": ambos devuelven nil. Devuelve ErrSharePasswordRequired si el
// share la exige y no se proporcionó ninguna, para que el llamador pueda
// responder "requiere contraseña" sin filtrar más metadata (§6 del plan).
func (s *FileService) verifySharePassword(share *Share, password string) error {
	if !share.HasPassword() {
		return nil
	}
	if password == "" {
		return ErrSharePasswordRequired
	}
	if err := s.hasher.Verify(password, share.PasswordHash); err != nil {
		return ErrSharePasswordIncorrect
	}
	return nil
}

// ResolvePublicShareForAccess encadena resolución + ciclo de vida +
// contraseña -- el camino común a descarga, navegación y subida vía enlace
// público. El orden (ciclo de vida antes que contraseña) evita gastar
// Argon2id en un token ya revocado o caducado.
func (s *FileService) ResolvePublicShareForAccess(ctx context.Context, token, password string) (*Share, error) {
	share, err := s.ResolvePublicShare(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := share.checkLifecycle(time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := s.verifySharePassword(share, password); err != nil {
		return nil, err
	}
	return share, nil
}

// resolveSharedPath calcula la ruta lógica compartida (dueño + parentPath)
// para navegar/descargar dentro de un share de carpeta, rechazando
// cualquier subPath que intente escapar de la raíz compartida (ej.
// "../../otra-carpeta") -- SafeJoin ya protege el filesystem físico, pero
// esto impide alcanzar OTRAS carpetas del mismo propietario por fuera de lo
// que el enlace concreto autoriza.
func resolveSharedPath(dir *Directory, subPath string) (logicalPath string, ok bool) {
	sharedRoot := path.Join(dir.ParentPath, dir.Name)
	target := normalizeParentPath(path.Join(sharedRoot, subPath))
	if target != sharedRoot && !strings.HasPrefix(target, sharedRoot+"/") {
		return "", false
	}
	return target, true
}

// BrowsePublicShare lista el contenido de un share de carpeta (o una
// subcarpeta suya vía subDirPath, relativo a la raíz compartida).
func (s *FileService) BrowsePublicShare(ctx context.Context, token, password, subDirPath string) (*ListResult, error) {
	share, err := s.ResolvePublicShareForAccess(ctx, token, password)
	if err != nil {
		return nil, err
	}
	if !share.IsDirectoryShare() {
		return nil, ErrInvalidPath
	}
	dir, err := s.directories.GetDirectoryByID(ctx, share.DirectoryID)
	if err != nil {
		return nil, err
	}
	target, ok := resolveSharedPath(dir, subDirPath)
	if !ok {
		return nil, ErrForbidden
	}
	return s.List(ctx, dir.OwnerID, target)
}

// DownloadViaPublicShare sirve el contenido de un archivo compartido
// directamente, o de un archivo dentro de una carpeta compartida (subPath
// relativo a la raíz compartida, "" si el share es de archivo). Incrementa
// el contador de descargas de forma atómica: si el enlace se agotó justo
// ahora (carrera con otra descarga concurrente), rechaza en vez de servir.
func (s *FileService) DownloadViaPublicShare(ctx context.Context, token, password, subPath string) (*FileMeta, io.ReadCloser, error) {
	share, err := s.ResolvePublicShareForAccess(ctx, token, password)
	if err != nil {
		return nil, nil, err
	}
	if !share.CanDownload {
		return nil, nil, ErrShareDownloadNotAllowed
	}

	var meta *FileMeta
	if share.IsDirectoryShare() {
		dir, err := s.directories.GetDirectoryByID(ctx, share.DirectoryID)
		if err != nil {
			return nil, nil, err
		}
		targetParent, ok := resolveSharedPath(dir, path.Dir(subPath))
		name := path.Base(subPath)
		if subPath == "" || !ok || name == "." || name == "/" {
			return nil, nil, ErrInvalidPath
		}
		pool, err := s.pools.DefaultPool(ctx)
		if err != nil {
			return nil, nil, err
		}
		meta, err = s.files.GetFileByNaturalKey(ctx, pool.ID, dir.OwnerID, targetParent, name)
		if err != nil {
			return nil, nil, err
		}
	} else {
		meta, err = s.files.GetFileByID(ctx, share.FileID)
		if err != nil {
			return nil, nil, err
		}
	}

	ok, err := s.shares.IncrementDownloadCount(ctx, share.ID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, ErrShareExhausted
	}

	prov, err := s.providers.For(ctx, meta.PoolID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)
	rc, err := prov.Read(ctx, rel)
	if err != nil {
		return nil, nil, fmt.Errorf("leyendo archivo compartido: %w", err)
	}
	return meta, rc, nil
}

// PublicUploadInput describe una subida a través de un enlace público (§37
// "Subida"). SubPath es la subcarpeta destino relativa a la raíz compartida
// ("" para subir directamente a la raíz del share).
type PublicUploadInput struct {
	Token    string
	Password string
	SubPath  string
	Name     string
	Content  io.Reader
}

// UploadViaPublicShare exige un share de carpeta con CanUpload=true.
// MaxUploadSizeBytes se aplica envolviendo Content en un lector que corta
// con error en cuanto se supera el límite (en vez de truncar en silencio),
// para que Upload aborte de forma natural -- su propio manejo de staging ya
// limpia el fichero parcial sin necesitar borrar nada después.
func (s *FileService) UploadViaPublicShare(ctx context.Context, in PublicUploadInput) (*FileMeta, error) {
	share, err := s.ResolvePublicShareForAccess(ctx, in.Token, in.Password)
	if err != nil {
		return nil, err
	}
	if !share.CanUpload {
		return nil, ErrShareUploadNotAllowed
	}
	if !share.IsDirectoryShare() {
		return nil, ErrShareUploadNotAllowed // ya se valida en CreateShare; defensa en profundidad
	}

	dir, err := s.directories.GetDirectoryByID(ctx, share.DirectoryID)
	if err != nil {
		return nil, err
	}
	target, ok := resolveSharedPath(dir, in.SubPath)
	if !ok {
		return nil, ErrForbidden
	}

	content := in.Content
	if share.MaxUploadSizeBytes != nil {
		content = &errLimitReader{r: content, remaining: *share.MaxUploadSizeBytes}
	}

	return s.Upload(ctx, UploadInput{OwnerID: dir.OwnerID, ParentPath: target, Name: in.Name, Content: content})
}

// errLimitReader corta la lectura con ErrShareUploadTooLarge en cuanto se
// intenta leer más de `remaining` bytes, a diferencia de io.LimitReader
// (que trunca en silencio devolviendo EOF). No depende de net/http
// (http.MaxBytesReader exigiría un http.ResponseWriter, no disponible en
// esta capa), así que sirve igual para subidas vía enlace público que para
// cualquier otro origen futuro del contenido.
type errLimitReader struct {
	r         io.Reader
	remaining int64
}

// Read pide siempre un byte más de lo permitido (remaining+1): si el origen
// tiene exactamente `remaining` bytes, esa lectura de más devuelve EOF de
// forma natural (fin real del contenido) y nunca se dispara el error; si
// tiene más, se lee ese byte de más, remaining pasa a negativo y se
// devuelve ErrShareUploadTooLarge junto con los bytes ya leídos (io.Copy los
// escribe igualmente antes de propagar el error, pero Upload nunca promueve
// el fichero de staging a destino final cuando hay un error, así que no
// queda contenido parcial).
func (l *errLimitReader) Read(p []byte) (int, error) {
	if l.remaining < 0 {
		return 0, ErrShareUploadTooLarge
	}
	if int64(len(p)) > l.remaining+1 {
		p = p[:l.remaining+1]
	}
	n, err := l.r.Read(p)
	l.remaining -= int64(n)
	if l.remaining < 0 {
		return n, ErrShareUploadTooLarge
	}
	return n, err
}

// SharedUploadInput describe una subida de un usuario autenticado a una
// carpeta que NO es suya, compartida con él o con un grupo suyo (§37, ADR-035).
type SharedUploadInput struct {
	RequesterID string
	DirectoryID string
	Name        string
	Content     io.Reader
}

// UploadToSharedDirectory sube un archivo NUEVO a una carpeta que requesterID
// no posee pero tiene compartida con permiso de subida (§37, ADR-035). El
// archivo pertenece al propietario de la carpeta (vive en su árbol); quién lo
// subió solo queda en la auditoría, que registra el llamador. Devuelve además
// el ID del share que autorizó la subida ("" si requesterID es el propietario).
//
// La autorización se re-deriva de la base de datos a partir del ID de carpeta
// en cada llamada, sin ninguna subruta que el cliente pueda manipular (como
// ListSharedDirectory): un share sobre una carpeta ya cubre sus subcarpetas.
// No sobrescribe un archivo existente: un nombre ya ocupado da
// ErrDestinationOccupied (la comprobación es previa, no atómica: ver Upload).
func (s *FileService) UploadToSharedDirectory(ctx context.Context, in SharedUploadInput) (*FileMeta, string, error) {
	if !s.sharingEnabled {
		return nil, "", ErrSharingDisabled
	}
	dir, err := s.directories.GetDirectoryByID(ctx, in.DirectoryID)
	if err != nil {
		return nil, "", err
	}
	if dir.IsTrashed() {
		return nil, "", ErrDirectoryNotFound
	}

	var shareID string
	content := in.Content
	if dir.OwnerID != in.RequesterID {
		grant, err := s.uploadGrantFor(ctx, in.RequesterID, dir)
		if err != nil {
			return nil, "", err
		}
		shareID = grant.ID
		if grant.MaxUploadSizeBytes != nil {
			content = &errLimitReader{r: content, remaining: *grant.MaxUploadSizeBytes}
		}
	}

	meta, err := s.Upload(ctx, UploadInput{
		OwnerID: dir.OwnerID, ParentPath: path.Join(dir.ParentPath, dir.Name), Name: in.Name, Content: content,
		NoOverwrite: true,
	})
	if err != nil {
		return nil, "", err
	}
	return meta, shareID, nil
}

// SharedUploadPermission es lo que un usuario puede subir a una carpeta: si
// puede y, en ese caso, el tamaño máximo por archivo que se le va a aplicar.
type SharedUploadPermission struct {
	Allowed bool
	// MaxSizeBytes es el límite por archivo (nil = sin límite). Solo tiene
	// sentido con Allowed: es el mismo que aplicará UploadToSharedDirectory.
	MaxSizeBytes *int64
}

// SharedUploadPermission dice si requesterID puede subir a directoryID y con qué
// límite, para que la interfaz sepa si ofrecer el botón y qué tamaño anunciar
// (así evita mandar un archivo que el servidor va a rechazar a medio subir). No
// devuelve error por falta de permiso, solo un permiso no concedido.
func (s *FileService) SharedUploadPermission(ctx context.Context, requesterID, directoryID string) (SharedUploadPermission, error) {
	if !s.sharingEnabled {
		return SharedUploadPermission{}, nil
	}
	dir, err := s.directories.GetDirectoryByID(ctx, directoryID)
	if err != nil {
		return SharedUploadPermission{}, err
	}
	if dir.IsTrashed() {
		return SharedUploadPermission{}, nil
	}
	if dir.OwnerID == requesterID {
		return SharedUploadPermission{Allowed: true}, nil
	}
	grants, err := s.uploadGrants(ctx, requesterID, dir)
	if err != nil || len(grants) == 0 {
		return SharedUploadPermission{}, err
	}
	return SharedUploadPermission{Allowed: true, MaxSizeBytes: mostPermissiveUploadGrant(grants).MaxUploadSizeBytes}, nil
}

// uploadGrantFor elige el permiso que autoriza la subida. Sin ninguno distingue
// entre "tiene lectura pero no subida" (ErrShareUploadNotAllowed) y "no tiene
// acceso" (ErrForbidden), igual que el resto de operaciones sobre compartidos.
func (s *FileService) uploadGrantFor(ctx context.Context, requesterID string, dir *Directory) (*Share, error) {
	grants, err := s.uploadGrants(ctx, requesterID, dir)
	if err != nil {
		return nil, err
	}
	if len(grants) > 0 {
		return mostPermissiveUploadGrant(grants), nil
	}
	canRead, err := s.hasShareAccessToDirectory(ctx, requesterID, dir)
	if err != nil {
		return nil, err
	}
	if canRead {
		return nil, ErrShareUploadNotAllowed
	}
	return nil, ErrForbidden
}

// uploadGrants junta los shares user/group de lectura + subida que autorizan a
// requesterID a subir a dir: los de la propia carpeta y los de sus ancestros
// (compartir una carpeta da acceso a todo su contenido, ADR-008). Un share
// sobre una carpeta ancestro que esté en la papelera no concede nada.
func (s *FileService) uploadGrants(ctx context.Context, requesterID string, dir *Directory) ([]*Share, error) {
	now := time.Now().UTC()
	grants, err := s.shares.ListUploadSharesForDirectory(ctx, requesterID, dir.ID, now)
	if err != nil {
		return nil, err
	}
	err = s.walkAncestorDirectories(ctx, dir.OwnerID, dir.ParentPath, func(ancestor *Directory) (bool, error) {
		if ancestor.IsTrashed() {
			return false, nil
		}
		more, err := s.shares.ListUploadSharesForDirectory(ctx, requesterID, ancestor.ID, now)
		grants = append(grants, more...)
		return false, err
	})
	return grants, err
}

// mostPermissiveUploadGrant elige, entre varios permisos aplicables a la vez,
// el de límite de tamaño más alto (sin límite gana a cualquiera): los permisos
// se unen, ninguno recorta a otro.
func mostPermissiveUploadGrant(grants []*Share) *Share {
	best := grants[0]
	for _, g := range grants[1:] {
		switch {
		case best.MaxUploadSizeBytes == nil:
			return best
		case g.MaxUploadSizeBytes == nil, *g.MaxUploadSizeBytes > *best.MaxUploadSizeBytes:
			best = g
		}
	}
	return best
}
