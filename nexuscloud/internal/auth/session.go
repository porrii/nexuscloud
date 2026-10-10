package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

var ErrSessionNotFound = errors.New("auth: sesión no encontrada")

// Session representa una sesión activa (§26). El token en claro nunca se
// persiste: solo TokenHash (SHA-256), y solo se muestra una vez al cliente
// en el momento del login (§78, §172).
type Session struct {
	ID         string
	UserID     string
	TokenHash  string
	Device     string
	IP         string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	// ReauthenticatedAt (ADR-042 Decisión 2): última vez que esta sesión
	// volvió a presentar la contraseña (modo «sudo»). nil = nunca.
	ReauthenticatedAt *time.Time
}

func (s *Session) IsValid(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

// ReauthWindow es lo que dura el modo «sudo» tras reautenticarse (ADR-042
// Decisión 2).
const ReauthWindow = 10 * time.Minute

// RecentlyReauthenticated indica si la sesión pasó la reautenticación
// reforzada hace menos de ReauthWindow. Una marca en el futuro (reloj
// desfasado o manipulado) no cuenta: falla cerrado.
func (s *Session) RecentlyReauthenticated(now time.Time) bool {
	if s.ReauthenticatedAt == nil {
		return false
	}
	at := *s.ReauthenticatedAt
	return !at.After(now) && now.Sub(at) < ReauthWindow
}

// HashToken calcula el hash de almacenamiento de un token de sesión/API.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type SessionRepository interface {
	CreateSession(ctx context.Context, s *Session) error
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	ListSessionsForUser(ctx context.Context, userID string) ([]*Session, error)
	TouchSession(ctx context.Context, id string, lastSeenAt time.Time) error
	RevokeSession(ctx context.Context, id, userID string) error
	RevokeAllForUser(ctx context.Context, userID string) error
	// MarkReauthenticated fija ReauthenticatedAt de la sesión id, que debe
	// pertenecer a userID y seguir sin revocar (ErrSessionNotFound si no).
	MarkReauthenticated(ctx context.Context, id, userID string, at time.Time) error
	DeleteExpiredSessions(ctx context.Context, before time.Time) (int64, error)
}
