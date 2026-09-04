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
}

func (s *Session) IsValid(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
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
	DeleteExpiredSessions(ctx context.Context, before time.Time) (int64, error)
}
