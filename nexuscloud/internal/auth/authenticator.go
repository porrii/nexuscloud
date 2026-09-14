package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

var (
	ErrAuthenticationFailed = errors.New("auth: usuario o contraseña incorrectos")
	ErrUserDisabled         = errors.New("auth: usuario deshabilitado")
	ErrTOTPRequired         = errors.New("auth: se requiere código TOTP")
	ErrTOTPInvalid          = errors.New("auth: código TOTP inválido")
)

// Authenticator orquesta login/logout/validación de sesión combinando
// users.Repository (identidad) con SessionRepository (estado de sesión) y
// Hasher (verificación de contraseña). Vive en internal/auth (no en
// internal/users) porque es este paquete el que depende de users, nunca al
// revés.
type Authenticator struct {
	users      users.Repository
	sessions   SessionRepository
	hasher     *Hasher
	sessionTTL time.Duration
	logger     *slog.Logger
}

func NewAuthenticator(userRepo users.Repository, sessionRepo SessionRepository, hasher *Hasher, sessionTTLHours int, logger *slog.Logger) *Authenticator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Authenticator{
		users:      userRepo,
		sessions:   sessionRepo,
		hasher:     hasher,
		sessionTTL: time.Duration(sessionTTLHours) * time.Hour,
		logger:     logger,
	}
}

func NewAuthenticatorFromConfig(userRepo users.Repository, sessionRepo SessionRepository, cfg *config.Config, logger *slog.Logger) *Authenticator {
	return NewAuthenticator(userRepo, sessionRepo, NewHasher(cfg.Security.Argon2), cfg.Security.SessionTTLHours, logger)
}

type LoginResult struct {
	User    *users.User
	Token   string // token en claro: se devuelve una única vez (§78)
	Session *Session
}

// Login verifica usuario+contraseña (y TOTP si está habilitado) y crea una
// nueva sesión. Siempre devuelve el mismo error genérico ante usuario
// inexistente o contraseña incorrecta, para no permitir enumeración de
// usuarios por temporización o mensaje (§27, §170).
func (a *Authenticator) Login(ctx context.Context, username, password, totpCode, device, ip string) (*LoginResult, error) {
	u, err := a.users.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return nil, ErrAuthenticationFailed
		}
		return nil, err
	}
	if !u.IsActive() {
		return nil, ErrUserDisabled
	}
	if err := a.hasher.Verify(password, u.PasswordHash); err != nil {
		return nil, ErrAuthenticationFailed
	}
	if u.HasTOTP() {
		if totpCode == "" {
			return nil, ErrTOTPRequired
		}
		if !ValidateTOTP(totpCode, u.TOTPSecret) {
			return nil, ErrTOTPInvalid
		}
	}

	token, err := idgen.Token()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sess := &Session{
		ID:         idgen.New(),
		UserID:     u.ID,
		TokenHash:  HashToken(token),
		Device:     device,
		IP:         ip,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(a.sessionTTL),
	}
	if err := a.sessions.CreateSession(ctx, sess); err != nil {
		return nil, err
	}

	u.LastLoginAt = &now
	u.UpdatedAt = now
	if err := a.users.UpdateUser(ctx, u); err != nil {
		return nil, err
	}

	return &LoginResult{User: u, Token: token, Session: sess}, nil
}

// ValidateToken resuelve un token de Bearer/cookie a su sesión + usuario, y
// refresca LastSeenAt (best-effort). La usa el middleware de autenticación
// en cada petición.
func (a *Authenticator) ValidateToken(ctx context.Context, token string) (*users.User, *Session, error) {
	sess, err := a.sessions.GetSessionByTokenHash(ctx, HashToken(token))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	if !sess.IsValid(now) {
		return nil, nil, ErrSessionNotFound
	}
	u, err := a.users.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return nil, nil, err
	}
	if !u.IsActive() {
		return nil, nil, ErrUserDisabled
	}
	if err := a.sessions.TouchSession(ctx, sess.ID, now); err != nil {
		// No crítico: no bloquea la petición, pero se registra (§94/§32).
		a.logger.Warn("no se pudo actualizar last_seen_at de la sesión", "session_id", sess.ID, "error", err)
	}
	return u, sess, nil
}

// Logout exige que sessionID pertenezca a userID (comprobado también en el
// repositorio, defensa en profundidad contra IDOR, §198).
func (a *Authenticator) Logout(ctx context.Context, sessionID, userID string) error {
	return a.sessions.RevokeSession(ctx, sessionID, userID)
}
