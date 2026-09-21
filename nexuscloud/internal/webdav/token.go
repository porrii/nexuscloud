// Package webdav implementa el módulo WebDAV de NexusCloud (§43, ADR-034):
// tokens de acceso propios para clientes que solo saben HTTP Basic, y un
// adaptador de webdav.FileSystem sobre storage.FileService para que WebDAV
// respete exactamente las mismas reglas que la API REST (propiedad, papelera,
// versionado, pools) en vez de ser una vía paralela al disco.
package webdav

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

var (
	ErrTokenNotFound      = errors.New("webdav: token no encontrado")
	ErrInvalidCredentials = errors.New("webdav: credenciales inválidas")
	ErrInvalidLabel       = errors.New("webdav: el nombre del token es demasiado largo")
)

// tokenPrefix hace reconocible un token (a simple vista, en logs y para
// herramientas de detección de secretos filtrados) sin restarle entropía:
// tras el prefijo van 256 bits aleatorios.
const tokenPrefix = "nwd_"

const (
	defaultTokenLabel = "WebDAV"
	maxTokenLabelLen  = 100
	// lastUsedThrottle evita una escritura en base de datos por cada
	// PROPFIND: un cliente WebDAV de escritorio dispara decenas por segundo.
	lastUsedThrottle = time.Minute
)

// Token es un token de acceso WebDAV. El valor en claro nunca se persiste:
// solo TokenHash (SHA-256 en hex, igual que las sesiones) y se muestra una
// única vez, al crearlo (§78, §172).
type Token struct {
	ID         string
	UserID     string
	TokenHash  string
	Label      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type TokenRepository interface {
	CreateToken(ctx context.Context, t *Token) error
	GetTokenByHash(ctx context.Context, tokenHash string) (*Token, error)
	ListTokensForUser(ctx context.Context, userID string) ([]*Token, error)
	TouchToken(ctx context.Context, id string, at time.Time) error
	// RevokeToken exige coincidencia de userID en la propia cláusula WHERE:
	// defensa en profundidad contra IDOR (§198), mismo criterio que
	// SessionRepository.RevokeSession.
	RevokeToken(ctx context.Context, id, userID string) error
}

// TokenService orquesta el ciclo de vida de los tokens de acceso WebDAV.
type TokenService struct {
	repo   TokenRepository
	users  users.Repository
	logger *slog.Logger
	now    func() time.Time
}

func NewTokenService(repo TokenRepository, userRepo users.Repository, logger *slog.Logger) *TokenService {
	if logger == nil {
		logger = slog.Default()
	}
	return &TokenService{repo: repo, users: userRepo, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

// Create genera un token nuevo y devuelve, además del registro, el valor
// en claro: es la única ocasión en que existe fuera de la memoria del
// cliente. label vacío = "WebDAV".
func (s *TokenService) Create(ctx context.Context, userID, label string) (*Token, string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = defaultTokenLabel
	}
	if utf8.RuneCountInString(label) > maxTokenLabelLen {
		return nil, "", ErrInvalidLabel
	}

	secret, err := idgen.Token()
	if err != nil {
		return nil, "", err
	}
	plain := tokenPrefix + secret

	t := &Token{
		ID:        idgen.New(),
		UserID:    userID,
		TokenHash: auth.HashToken(plain),
		Label:     label,
		CreatedAt: s.now(),
	}
	if err := s.repo.CreateToken(ctx, t); err != nil {
		return nil, "", err
	}
	return t, plain, nil
}

func (s *TokenService) List(ctx context.Context, userID string) ([]*Token, error) {
	return s.repo.ListTokensForUser(ctx, userID)
}

func (s *TokenService) Revoke(ctx context.Context, id, userID string) error {
	return s.repo.RevokeToken(ctx, id, userID)
}

// Authenticate valida las credenciales HTTP Basic de un cliente WebDAV:
// username debe ser el del dueño del token y el token debe existir y no
// estar revocado; el usuario, además, tiene que seguir activo. Cualquier
// fallo de credenciales devuelve el mismo ErrInvalidCredentials, sin
// distinguir cuál de las tres piezas era la incorrecta (§27, §170).
func (s *TokenService) Authenticate(ctx context.Context, username, plainToken string) (*users.User, error) {
	if username == "" || !strings.HasPrefix(plainToken, tokenPrefix) {
		return nil, ErrInvalidCredentials
	}

	t, err := s.repo.GetTokenByHash(ctx, auth.HashToken(plainToken))
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	u, err := s.users.GetUserByID(ctx, t.UserID)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !u.IsActive() || subtle.ConstantTimeCompare([]byte(u.Username), []byte(username)) != 1 {
		return nil, ErrInvalidCredentials
	}

	if now := s.now(); t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= lastUsedThrottle {
		if err := s.repo.TouchToken(ctx, t.ID, now); err != nil {
			// No crítico: no bloquea la petición (mismo criterio que
			// Authenticator.ValidateToken con last_seen_at).
			s.logger.Warn("no se pudo actualizar last_used_at del token WebDAV", "token_id", t.ID, "error", err)
		}
	}
	return u, nil
}
