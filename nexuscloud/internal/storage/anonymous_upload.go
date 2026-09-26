package storage

import (
	"context"
	"errors"
	"time"
)

var (
	ErrAnonymousUploadNotFound  = errors.New("storage: enlace de subida anónima no encontrado")
	ErrAnonymousUploadExpired   = errors.New("storage: el enlace de subida anónima ha expirado")
	ErrAnonymousUploadRevoked   = errors.New("storage: el enlace de subida anónima ha sido revocado")
	ErrAnonymousUploadDisabled  = errors.New("storage: la subida anónima está desactivada en esta instancia")
	ErrAnonymousUploadForbidden = errors.New("storage: no eres el propietario de esa carpeta")
)

// AnonymousUpload es un enlace de subida sin ningún acceso al resto del
// contenido (§38, ADR-039) -- modelo SEPARADO de Share (share.go), no una
// variante con una bandera: el modelo Share no garantiza de verdad "solo
// subida, sin ver el resto" (BrowsePublicShare ignora CanDownload), así que
// aquí la garantía es estructural -- este tipo no tiene ningún método de
// navegación, nunca lo tendrá por descuido. Siempre apunta a una carpeta
// (nunca a un archivo suelto, a diferencia de Favorite): DirectoryID no es
// nullable/polimórfico.
type AnonymousUpload struct {
	ID                 string
	OwnerID            string
	DirectoryID        string
	TokenHash          string
	Label              string
	MaxUploadSizeBytes *int64
	ExpiresAt          *time.Time
	RevokedAt          *time.Time
	UploadCount        int // informativo para el propietario, igual papel que Share.DownloadCount
	CreatedAt          time.Time
}

func (a *AnonymousUpload) IsRevoked() bool { return a.RevokedAt != nil }
func (a *AnonymousUpload) IsExpired(now time.Time) bool {
	return a.ExpiresAt != nil && now.After(*a.ExpiresAt)
}

// checkLifecycle comprueba revocación/expiración -- mismo criterio que
// Share.checkLifecycle, sin "agotado" (aquí no hay concepto de límite de
// usos en esta primera versión).
func (a *AnonymousUpload) checkLifecycle(now time.Time) error {
	if a.IsRevoked() {
		return ErrAnonymousUploadRevoked
	}
	if a.IsExpired(now) {
		return ErrAnonymousUploadExpired
	}
	return nil
}

type AnonymousUploadRepository interface {
	CreateLink(ctx context.Context, a *AnonymousUpload) error
	GetLinkByTokenHash(ctx context.Context, tokenHash string) (*AnonymousUpload, error)
	ListLinksForOwner(ctx context.Context, ownerID string) ([]*AnonymousUpload, error)
	// RevokeLink exige coincidencia de ownerID en la propia cláusula WHERE:
	// defensa en profundidad contra IDOR (§198), mismo criterio que el
	// resto del proyecto. revokedAt lo decide quien llama (mismo patrón que
	// ShareRepository.RevokeShare), no el repositorio.
	RevokeLink(ctx context.Context, id, ownerID string, revokedAt time.Time) error
	// IncrementUploadCount es best-effort (solo informativo, ver
	// UploadCount): un fallo aquí nunca debe abortar una subida que ya tuvo
	// éxito.
	IncrementUploadCount(ctx context.Context, id string) error
}
