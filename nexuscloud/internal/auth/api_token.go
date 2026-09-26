package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

var (
	ErrAPITokenNotFound      = errors.New("auth: token de API no encontrado")
	ErrInvalidAPICredentials = errors.New("auth: token de API inválido")
	ErrInvalidAPITokenLabel  = errors.New("auth: el nombre del token es demasiado largo")
)

// APITokenPrefix hace reconocible un token de API (§78, ADR-037) a simple
// vista, en logs y para herramientas de detección de secretos filtrados,
// sin restarle entropía: tras el prefijo van 256 bits aleatorios. Se exporta
// (a diferencia del prefijo del token WebDAV, privado a su propio paquete)
// porque el middleware de la API REST (internal/api/v1) necesita
// distinguirlo de un token de sesión antes de decidir en qué tabla buscar.
const APITokenPrefix = "nat_"

const (
	defaultAPITokenLabel = "Token de API"
	maxAPITokenLabelLen  = 100
	// apiTokenLastUsedThrottle evita una escritura en base de datos por cada
	// petición: mismo criterio que webdav.lastUsedThrottle.
	apiTokenLastUsedThrottle = time.Minute
)

// APIToken es un token de acceso a la API REST completa (§78), de alcance
// todo-o-nada: actúa exactamente como su propietario, sin permisos más
// finos (ADR-037 -- hoy nada en la app tiene permisos más finos que
// administrador/usuario normal, ni para personas ni en ningún otro sitio
// del código). El valor en claro nunca se persiste: solo TokenHash
// (SHA-256 en hex, igual que las sesiones y los tokens WebDAV), y se
// muestra una única vez, al crearlo.
type APIToken struct {
	ID         string
	UserID     string
	TokenHash  string
	Label      string
	CreatedAt  time.Time
	ExpiresAt  *time.Time // nil = nunca expira
	LastUsedAt *time.Time
}

// IsValid es false si el token tiene una expiración ya pasada. Un token sin
// ExpiresAt nunca deja de ser válido por esta vía (solo por revocación).
func (t *APIToken) IsValid(now time.Time) bool {
	return t.ExpiresAt == nil || now.Before(*t.ExpiresAt)
}

type APITokenRepository interface {
	CreateToken(ctx context.Context, t *APIToken) error
	GetTokenByHash(ctx context.Context, tokenHash string) (*APIToken, error)
	ListTokensForUser(ctx context.Context, userID string) ([]*APIToken, error)
	TouchToken(ctx context.Context, id string, at time.Time) error
	// RevokeToken exige coincidencia de userID en la propia cláusula WHERE:
	// defensa en profundidad contra IDOR (§198), mismo criterio que
	// SessionRepository.RevokeSession y webdav.TokenRepository.RevokeToken.
	RevokeToken(ctx context.Context, id, userID string) error
}

// APITokenService orquesta el ciclo de vida de los tokens de API. Mismo
// patrón exacto que webdav.TokenService, adaptado a Bearer general (aquí no
// hay un username que comparar aparte, como sí lo hay en HTTP Basic: el
// Bearer solo trae el token).
type APITokenService struct {
	repo   APITokenRepository
	users  users.Repository
	logger *slog.Logger
	now    func() time.Time
}

func NewAPITokenService(repo APITokenRepository, userRepo users.Repository, logger *slog.Logger) *APITokenService {
	if logger == nil {
		logger = slog.Default()
	}
	return &APITokenService{repo: repo, users: userRepo, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

// Create genera un token nuevo y devuelve, además del registro, el valor en
// claro: es la única ocasión en que existe fuera de la memoria del cliente.
// label vacío usa un nombre por defecto. expiresAt nil = nunca expira.
func (s *APITokenService) Create(ctx context.Context, userID, label string, expiresAt *time.Time) (*APIToken, string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = defaultAPITokenLabel
	}
	if utf8.RuneCountInString(label) > maxAPITokenLabelLen {
		return nil, "", ErrInvalidAPITokenLabel
	}

	secret, err := idgen.Token()
	if err != nil {
		return nil, "", err
	}
	plain := APITokenPrefix + secret

	t := &APIToken{
		ID:        idgen.New(),
		UserID:    userID,
		TokenHash: HashToken(plain),
		Label:     label,
		CreatedAt: s.now(),
		ExpiresAt: expiresAt,
	}
	if err := s.repo.CreateToken(ctx, t); err != nil {
		return nil, "", err
	}
	return t, plain, nil
}

func (s *APITokenService) List(ctx context.Context, userID string) ([]*APIToken, error) {
	return s.repo.ListTokensForUser(ctx, userID)
}

func (s *APITokenService) Revoke(ctx context.Context, id, userID string) error {
	return s.repo.RevokeToken(ctx, id, userID)
}

// Authenticate valida un token de API en claro recibido como Bearer: debe
// llevar el prefijo reconocido, existir, no estar expirado, y su usuario
// debe seguir activo. Cualquier fallo devuelve el mismo
// ErrInvalidAPICredentials, sin distinguir la causa (§27, §170, mismo
// criterio anti-enumeración que el resto de la autenticación).
func (s *APITokenService) Authenticate(ctx context.Context, plainToken string) (*users.User, *APIToken, error) {
	if !strings.HasPrefix(plainToken, APITokenPrefix) {
		return nil, nil, ErrInvalidAPICredentials
	}

	t, err := s.repo.GetTokenByHash(ctx, HashToken(plainToken))
	if err != nil {
		if errors.Is(err, ErrAPITokenNotFound) {
			return nil, nil, ErrInvalidAPICredentials
		}
		return nil, nil, err
	}
	now := s.now()
	if !t.IsValid(now) {
		return nil, nil, ErrInvalidAPICredentials
	}
	u, err := s.users.GetUserByID(ctx, t.UserID)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return nil, nil, ErrInvalidAPICredentials
		}
		return nil, nil, err
	}
	if !u.IsActive() {
		return nil, nil, ErrInvalidAPICredentials
	}

	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= apiTokenLastUsedThrottle {
		if err := s.repo.TouchToken(ctx, t.ID, now); err != nil {
			// No crítico: no bloquea la petición (mismo criterio que
			// Authenticator.ValidateToken con last_seen_at).
			s.logger.Warn("no se pudo actualizar last_used_at del token de API", "token_id", t.ID, "error", err)
		} else {
			t.LastUsedAt = &now
		}
	}
	return u, t, nil
}
