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
	ErrWebAuthnRequired     = errors.New("auth: se requiere un passkey (WebAuthn)")
)

// Authenticator orquesta login/logout/validación de sesión combinando
// users.Repository (identidad) con SessionRepository (estado de sesión) y
// Hasher (verificación de contraseña). Vive en internal/auth (no en
// internal/users) porque es este paquete el que depende de users, nunca al
// revés.
type Authenticator struct {
	users         users.Repository
	sessions      SessionRepository
	webauthnCreds WebAuthnCredentialRepository
	hasher        *Hasher
	sessionTTL    time.Duration
	logger        *slog.Logger
}

// webauthnCreds es opcional (nil-safe): si es nil, Login se comporta
// exactamente como antes de que existiera WebAuthn (solo contraseña+TOTP).
func NewAuthenticator(userRepo users.Repository, sessionRepo SessionRepository, webauthnCreds WebAuthnCredentialRepository, hasher *Hasher, sessionTTLHours int, logger *slog.Logger) *Authenticator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Authenticator{
		users:         userRepo,
		sessions:      sessionRepo,
		webauthnCreds: webauthnCreds,
		hasher:        hasher,
		sessionTTL:    time.Duration(sessionTTLHours) * time.Hour,
		logger:        logger,
	}
}

func NewAuthenticatorFromConfig(userRepo users.Repository, sessionRepo SessionRepository, webauthnCreds WebAuthnCredentialRepository, cfg *config.Config, logger *slog.Logger) *Authenticator {
	return NewAuthenticator(userRepo, sessionRepo, webauthnCreds, NewHasher(cfg.Security.Argon2), cfg.Security.SessionTTLHours, logger)
}

type LoginResult struct {
	User    *users.User
	Token   string // token en claro: se devuelve una única vez (§78)
	Session *Session
}

// VerifyPassword comprueba usuario+contraseña sin completar el login (no
// crea sesión, no exige segundo factor). La usa el paso "begin" de una
// ceremonia WebAuthn de login con usuario ya conocido (2FA): necesita
// saber qué usuario es antes de emitir el reto, pero completar la sesión
// todavía no corresponde a ese paso -- FinishLogin es quien de verdad
// autentica. Mismo criterio anti-enumeración que Login (§27, §170).
func (a *Authenticator) VerifyPassword(ctx context.Context, username, password string) (*users.User, error) {
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
	return u, nil
}

// Login verifica usuario+contraseña y el segundo factor que corresponda
// -- WebAuthn si el usuario tiene algún passkey registrado (más fuerte y
// resistente a phishing, tiene prioridad sobre TOTP si tiene ambos, §25),
// TOTP en otro caso -- y crea una nueva sesión. Siempre devuelve el mismo
// error genérico ante usuario inexistente o contraseña incorrecta, para
// no permitir enumeración de usuarios por temporización o mensaje (§27,
// §170).
//
// Cuando devuelve ErrWebAuthnRequired, la contraseña ya es correcta pero
// el login no se completa aquí: el cliente debe repetir la verificación
// de contraseña contra /auth/webauthn/login/begin (vía
// Authenticator.VerifyPassword) para obtener el reto, y solo entonces
// FinishLogin crea la sesión real. WebAuthn, a diferencia de TOTP, no se
// puede completar en la misma llamada porque exige un reto servidor
// primero -- no hay un "código" que el cliente ya tenga de antemano.
func (a *Authenticator) Login(ctx context.Context, username, password, totpCode, device, ip string) (*LoginResult, error) {
	u, err := a.VerifyPassword(ctx, username, password)
	if err != nil {
		return nil, err
	}

	if a.webauthnCreds != nil {
		creds, err := a.webauthnCreds.ListCredentialsForUser(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		if len(creds) > 0 {
			return nil, ErrWebAuthnRequired
		}
	}
	if u.HasTOTP() {
		if totpCode == "" {
			return nil, ErrTOTPRequired
		}
		if !ValidateTOTP(totpCode, u.TOTPSecret) {
			return nil, ErrTOTPInvalid
		}
	}

	return a.completeLogin(ctx, u, device, ip)
}

// completeLogin crea la sesión real y actualiza LastLoginAt, una vez que
// el usuario ya está plenamente autenticado (contraseña + el segundo
// factor que correspondiera, cualquiera que haya sido). La usan tanto
// Login (tras pasar TOTP) como los handlers de FinishLogin/
// FinishDiscoverableLogin de WebAuthn (tras validar el passkey) -- ambos
// caminos terminan exactamente igual, así que esta cola no se duplica.
func (a *Authenticator) completeLogin(ctx context.Context, u *users.User, device, ip string) (*LoginResult, error) {
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

// CompleteWebAuthnLogin crea la sesión real tras una ceremonia WebAuthn de
// login válida (segundo factor o passwordless) -- ver completeLogin.
// Exportado porque lo llama el handler HTTP de FinishLogin/
// FinishDiscoverableLogin, en internal/api/v1.
func (a *Authenticator) CompleteWebAuthnLogin(ctx context.Context, u *users.User, device, ip string) (*LoginResult, error) {
	return a.completeLogin(ctx, u, device, ip)
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
