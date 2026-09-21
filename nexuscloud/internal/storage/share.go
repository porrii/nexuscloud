package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

type ShareType string

const (
	ShareTypeUser  ShareType = "user"
	ShareTypeGroup ShareType = "group"
	ShareTypeLink  ShareType = "link"
)

var (
	ErrShareNotFound           = errors.New("storage: share no encontrado")
	ErrShareExpired            = errors.New("storage: el enlace ha expirado")
	ErrShareRevoked            = errors.New("storage: el enlace ha sido revocado")
	ErrShareExhausted          = errors.New("storage: el enlace alcanzó su límite de descargas")
	ErrSharePasswordRequired   = errors.New("storage: el enlace requiere contraseña")
	ErrSharePasswordIncorrect  = errors.New("storage: contraseña incorrecta")
	ErrShareDownloadNotAllowed = errors.New("storage: este share no permite descarga")
	ErrShareUploadNotAllowed   = errors.New("storage: este share no permite subida")
	ErrShareUploadTooLarge     = errors.New("storage: el archivo supera el límite de tamaño de este enlace")
	ErrPublicLinksDisabled     = errors.New("storage: los enlaces públicos están desactivados en esta instancia")
	ErrSharingDisabled         = errors.New("storage: la compartición está desactivada en esta instancia")
	ErrInvalidShare            = errors.New("storage: datos de share inválidos")
)

// Share concede acceso a un archivo o carpeta (exactamente uno de FileID/
// DirectoryID, nunca ambos ni ninguno) a otro usuario, a un grupo, o a
// cualquiera que posea el token de un enlace (§37). OwnerID es siempre quien
// lo creó: el único que puede revocarlo o verlo en "compartido por mí". Los
// campos string que representan un destino opcional usan "" para "sin
// valor", igual que users.User.Email -- nunca punteros a string.
type Share struct {
	ID                 string
	OwnerID            string
	FileID             string // "" si el recurso compartido es una carpeta
	DirectoryID        string // "" si el recurso compartido es un archivo
	Type               ShareType
	TargetUserID       string // solo Type == ShareTypeUser
	TargetGroupID      string // solo Type == ShareTypeGroup
	TokenHash          string // solo Type == ShareTypeLink
	Label              string // "nombre personalizado" (§37): cosmético, nunca sustituye TokenHash como secreto
	CanDownload        bool
	CanUpload          bool
	PasswordHash       string // "" = sin contraseña
	ExpiresAt          *time.Time
	MaxDownloads       *int
	DownloadCount      int
	MaxUploadSizeBytes *int64
	RevokedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (s *Share) IsDirectoryShare() bool { return s.DirectoryID != "" }
func (s *Share) HasPassword() bool      { return s.PasswordHash != "" }
func (s *Share) IsRevoked() bool        { return s.RevokedAt != nil }
func (s *Share) IsExpired(now time.Time) bool {
	return s.ExpiresAt != nil && now.After(*s.ExpiresAt)
}
func (s *Share) IsExhausted() bool {
	return s.MaxDownloads != nil && s.DownloadCount >= *s.MaxDownloads
}

// checkLifecycle comprueba revocación/expiración/agotamiento -- usado tanto
// al resolver un enlace público como al listar shares internos vigentes.
func (s *Share) checkLifecycle(now time.Time) error {
	if s.IsRevoked() {
		return ErrShareRevoked
	}
	if s.IsExpired(now) {
		return ErrShareExpired
	}
	if s.IsExhausted() {
		return ErrShareExhausted
	}
	return nil
}

// ShareRepository abstrae el acceso a datos de shares (§8). Las
// comprobaciones de revocado/expirado se hacen en SQL (WHERE) porque son
// criterios de una consulta; el agotamiento por max_downloads NO se filtra
// aquí -- bloquea la descarga en sí (IncrementDownloadCount), no la
// visibilidad/navegación, así que listar o navegar un share agotado sigue
// funcionando hasta el intento real de descarga.
type ShareRepository interface {
	CreateShare(ctx context.Context, s *Share) error
	GetShareByID(ctx context.Context, id string) (*Share, error)
	GetShareByTokenHash(ctx context.Context, tokenHash string) (*Share, error)
	// ListSharesByOwner devuelve los shares creados por ownerID, sin incluir
	// los revocados ("compartido por mí" no muestra lo ya revocado --
	// history vive en audit_events, no aquí).
	ListSharesByOwner(ctx context.Context, ownerID string) ([]*Share, error)
	// ListSharesForUser devuelve los shares de tipo 'user' dirigidos
	// directamente a userID, más los de tipo 'group' de cualquier grupo del
	// que userID sea miembro (JOIN con user_groups en SQL, sin que este
	// paquete importe internal/users -- igual que files/directories ya
	// referencian users(id) sin importar ese paquete Go). Excluye
	// revocados/expirados ("compartido conmigo" no muestra accesos ya
	// perdidos).
	ListSharesForUser(ctx context.Context, userID string, now time.Time) ([]*Share, error)
	// HasFileAccess/HasDirectoryAccess reportan si existe un share de tipo
	// 'user' o 'group' (nunca 'link', que se resuelve por token, no por
	// userID), no revocado y no expirado, sobre ese recurso exacto, que
	// conceda acceso de DESCARGA a userID -- directo o vía cualquier grupo
	// del que sea miembro.
	HasFileAccess(ctx context.Context, userID, fileID string, now time.Time) (bool, error)
	HasDirectoryAccess(ctx context.Context, userID, directoryID string, now time.Time) (bool, error)
	// ListUploadSharesForDirectory devuelve los shares user/group no
	// revocados ni expirados que dan a userID -- directo o vía un grupo suyo --
	// permiso de lectura + subida sobre ESA carpeta (no sus ancestros: el
	// llamador los recorre). Devuelve los shares y no un booleano porque cada
	// uno puede llevar su propio límite de tamaño (ADR-035).
	ListUploadSharesForDirectory(ctx context.Context, userID, directoryID string, now time.Time) ([]*Share, error)
	// IncrementDownloadCount incrementa de forma atómica solo si todavía no
	// se alcanzó max_downloads (UPDATE...WHERE + RowsAffected, no
	// leer-luego-escribir): protege el último hueco disponible frente a
	// descargas concurrentes. ok=false si no se aplicó porque ya estaba
	// agotado.
	IncrementDownloadCount(ctx context.Context, shareID string) (ok bool, err error)
	RevokeShare(ctx context.Context, id string, revokedAt time.Time) error
}

// hashShareToken calcula el hash de almacenamiento de un token de enlace
// público, igual criterio que auth.HashToken para sesiones (SHA-256 hex) --
// implementado aquí y no reutilizado desde internal/auth porque
// internal/storage no puede importar internal/auth (regla de dependencia,
// docs/architecture.md): son dos primitivas triviales de stdlib, no vale la
// pena romper la regla por evitar dos líneas duplicadas.
func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
