package storage

import (
	"context"
	"errors"
	"io"
	"path"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// WithAnonymousUploads activa la subida anónima (§38, ADR-039). El
// repositorio se conecta siempre que se pase esta opción; el interruptor
// real de la funcionalidad es enabled (sharing.anonymousUploadEnabled),
// comprobado en cada operación que lo necesita -- ver el campo en
// file_service.go para el criterio exacto de qué respeta el interruptor y
// qué no.
func WithAnonymousUploads(repo AnonymousUploadRepository, enabled bool) FileServiceOption {
	return func(s *FileService) {
		s.anonymousUploads = repo
		s.anonymousUploadEnabled = enabled
	}
}

// CreateAnonymousUploadLink crea un enlace de subida sobre una carpeta
// PROPIA de ownerID (§38: el creador elige su propia carpeta, no un buzón
// designado por el administrador). maxUploadSizeBytes/expiresAt son
// opcionales (nil = sin límite/nunca expira). Devuelve, además del
// registro, el token en claro -- única ocasión en que existe fuera de la
// memoria de quien lo crea.
func (s *FileService) CreateAnonymousUploadLink(
	ctx context.Context, ownerID, directoryID, label string, maxUploadSizeBytes *int64, expiresAt *time.Time,
) (*AnonymousUpload, string, error) {
	if s.anonymousUploads == nil || !s.anonymousUploadEnabled {
		return nil, "", ErrAnonymousUploadDisabled
	}
	dir, err := s.directories.GetDirectoryByID(ctx, directoryID)
	if err != nil {
		return nil, "", err
	}
	if dir.OwnerID != ownerID {
		return nil, "", ErrAnonymousUploadForbidden
	}

	token, err := idgen.Token()
	if err != nil {
		return nil, "", err
	}
	a := &AnonymousUpload{
		ID: idgen.New(), OwnerID: ownerID, DirectoryID: directoryID, TokenHash: hashShareToken(token),
		Label: label, MaxUploadSizeBytes: maxUploadSizeBytes, ExpiresAt: expiresAt, CreatedAt: time.Now().UTC(),
	}
	if err := s.anonymousUploads.CreateLink(ctx, a); err != nil {
		return nil, "", err
	}
	return a, token, nil
}

func (s *FileService) ListAnonymousUploadLinks(ctx context.Context, ownerID string) ([]*AnonymousUpload, error) {
	if s.anonymousUploads == nil {
		return nil, ErrAnonymousUploadDisabled
	}
	return s.anonymousUploads.ListLinksForOwner(ctx, ownerID)
}

// DirectoryNameForAnonymousUpload resuelve el nombre de la carpeta de un
// enlace para mostrarlo en el listado del propietario (mismo papel que
// ResourceInfoForShare para shares). Sin comprobar propiedad: para cuando se
// invoca, el enlace ya salió de ListAnonymousUploadLinks(ownerID), que ya
// filtró por propietario.
func (s *FileService) DirectoryNameForAnonymousUpload(ctx context.Context, a *AnonymousUpload) (string, error) {
	dir, err := s.directories.GetDirectoryByID(ctx, a.DirectoryID)
	if err != nil {
		return "", err
	}
	return dir.Name, nil
}

func (s *FileService) RevokeAnonymousUploadLink(ctx context.Context, ownerID, id string) error {
	if s.anonymousUploads == nil {
		return ErrAnonymousUploadDisabled
	}
	return s.anonymousUploads.RevokeLink(ctx, id, ownerID, time.Now().UTC())
}

// ResolveAnonymousUploadForAccess valida un token en claro recibido en la
// URL pública: el interruptor global, que exista, y que no esté revocado ni
// expirado. Sin contraseña (a diferencia de Share, no se pidió para esta
// primera versión).
func (s *FileService) ResolveAnonymousUploadForAccess(ctx context.Context, token string) (*AnonymousUpload, error) {
	if s.anonymousUploads == nil || !s.anonymousUploadEnabled {
		return nil, ErrAnonymousUploadDisabled
	}
	a, err := s.anonymousUploads.GetLinkByTokenHash(ctx, hashShareToken(token))
	if err != nil {
		return nil, err
	}
	if err := a.checkLifecycle(time.Now().UTC()); err != nil {
		return nil, err
	}
	return a, nil
}

// UploadViaAnonymousLink sube un archivo directo a la RAÍZ de la carpeta del
// enlace (§38: "sin acceso al resto del contenido") -- a diferencia de
// UploadViaPublicShare, no admite ninguna sub-ruta: no hay concepto de
// navegar dentro, así que no hay nada que resolver más allá de la propia
// carpeta. Deliberadamente NO existe ningún método de listado/navegación en
// todo este archivo -- es la garantía estructural de §38, no una
// comprobación que se pueda olvidar.
func (s *FileService) UploadViaAnonymousLink(ctx context.Context, token, name string, content io.Reader, sizeHint int64) (*FileMeta, error) {
	a, err := s.ResolveAnonymousUploadForAccess(ctx, token)
	if err != nil {
		return nil, err
	}
	dir, err := s.directories.GetDirectoryByID(ctx, a.DirectoryID)
	if err != nil {
		return nil, err
	}
	if dir.IsTrashed() {
		return nil, ErrDirectoryNotFound
	}

	if a.MaxUploadSizeBytes != nil {
		content = &errLimitReader{r: content, remaining: *a.MaxUploadSizeBytes, err: ErrAnonymousUploadTooLarge}
	}

	// Los archivos son del propietario de la carpeta: Upload aplica SU
	// cuota (ADR-036), no la de quien sube (que ni siquiera tiene cuenta).
	meta, err := s.Upload(ctx, UploadInput{
		OwnerID: dir.OwnerID, ParentPath: path.Join(dir.ParentPath, dir.Name), Name: name, Content: content,
		SizeHint: sizeHint, NoOverwrite: true,
	})
	if err != nil {
		return nil, err
	}
	// Best-effort: FileService no tiene logger propio (ninguna otra
	// operación de este archivo lo necesita); un fallo aquí es solo el
	// contador informativo, nunca debe deshacer una subida que ya tuvo
	// éxito ni bloquear la respuesta al que sube.
	_ = s.anonymousUploads.IncrementUploadCount(ctx, a.ID)
	return meta, nil
}

// ErrAnonymousUploadTooLarge se comprueba con errors.Is desde la capa de
// API, mismo patrón que ErrShareUploadTooLarge.
var ErrAnonymousUploadTooLarge = errors.New("storage: el archivo supera el límite de tamaño de este enlace")
