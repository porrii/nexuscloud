// Package accountadmin reúne las operaciones de administración sobre la
// cuenta de OTRO usuario que tocan a la vez identidad y credenciales
// (ADR-042, B1): restablecer la contraseña, quitar el segundo factor o un
// passkey y revocar todo el acceso. Lo usan la CLI y la API (Decisión 4),
// así que las reglas viven aquí y no en los handlers.
//
// Existe como paquete propio porque necesita users (reglas de dominio),
// auth (hashing, sesiones, tokens de API, passkeys) y los tokens WebDAV, y
// ninguno de esos paquetes puede importar a los otros sin crear un ciclo.
package accountadmin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/users"
)

var (
	// ErrSelfAction: estas operaciones son sobre OTRA cuenta; la propia se
	// gestiona desde el autoservicio (sesiones, 2FA, passkeys).
	ErrSelfAction = errors.New("accountadmin: esta acción es para otra cuenta, no para la tuya")
	// ErrWeakPassword: mismas reglas que al crear una cuenta (§25).
	ErrWeakPassword = errors.New("accountadmin: la contraseña debe tener al menos 8 caracteres")
	// ErrTOTPNotEnabled: la cuenta no tiene 2FA por TOTP que quitar.
	ErrTOTPNotEnabled = errors.New("accountadmin: la cuenta no tiene la verificación en dos pasos activada")
)

// MinPasswordLength es el mismo mínimo que exigen CreateUser y el canje de
// invitaciones.
const MinPasswordLength = 8

// TokenRevoker borra todos los tokens de un usuario. Lo cumplen
// auth.APITokenRepository y webdav.TokenRepository; se declara aquí para
// no importar el paquete webdav.
type TokenRevoker interface {
	RevokeAllForUser(ctx context.Context, userID string) (int64, error)
}

// Service orquesta las operaciones. webauthn puede ser nil si la instancia
// nunca tuvo passkeys configurados (la tabla existe igualmente; en la
// práctica server.go siempre lo conecta).
type Service struct {
	users        *users.Service
	userRepo     users.Repository
	hasher       *auth.Hasher
	sessions     auth.SessionRepository
	apiTokens    TokenRevoker
	webdavTokens TokenRevoker
	webauthn     auth.WebAuthnCredentialRepository
}

func New(
	userSvc *users.Service,
	userRepo users.Repository,
	hasher *auth.Hasher,
	sessions auth.SessionRepository,
	apiTokens TokenRevoker,
	webdavTokens TokenRevoker,
	webauthn auth.WebAuthnCredentialRepository,
) *Service {
	return &Service{
		users: userSvc, userRepo: userRepo, hasher: hasher, sessions: sessions,
		apiTokens: apiTokens, webdavTokens: webdavTokens, webauthn: webauthn,
	}
}

// Revoked resume lo que se cortó (para la respuesta y la auditoría).
type Revoked struct {
	APITokens    int64 `json:"api_tokens"`
	WebDAVTokens int64 `json:"webdav_tokens"`
}

// checkTarget aplica las reglas comunes: no sobre uno mismo, la cuenta
// existe y el actor puede gestionarla (un administrator no toca a un
// super_admin). actorID vacío = CLI local, sin reglas entre administradores.
func (s *Service) checkTarget(ctx context.Context, actorID, targetID string) (*users.User, error) {
	if actorID != "" && actorID == targetID {
		return nil, ErrSelfAction
	}
	u, err := s.userRepo.GetUserByID(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if actorID != "" {
		if err := s.users.EnsureCanManage(ctx, actorID, targetID); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// ResetPassword fija una contraseña nueva elegida por el administrador
// (ADR-042 Decisión 9) y revoca todas las sesiones y todos los tokens de API
// y WebDAV de la cuenta: quien tuviera la contraseña antigua, o algo
// derivado de ella, deja de entrar.
func (s *Service) ResetPassword(ctx context.Context, actorID, targetID, newPassword string) (Revoked, error) {
	if len(newPassword) < MinPasswordLength {
		return Revoked{}, ErrWeakPassword
	}
	u, err := s.checkTarget(ctx, actorID, targetID)
	if err != nil {
		return Revoked{}, err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return Revoked{}, fmt.Errorf("hasheando la contraseña: %w", err)
	}
	u.PasswordHash = hash
	u.UpdatedAt = time.Now().UTC()
	if err := s.userRepo.UpdateUser(ctx, u); err != nil {
		return Revoked{}, err
	}
	return s.RevokeAllAccess(ctx, targetID)
}

// RemoveTOTP quita el 2FA por TOTP de otra cuenta (Decisión 10) y revoca sus
// sesiones: las abiertas se validaron con el segundo factor que se retira.
func (s *Service) RemoveTOTP(ctx context.Context, actorID, targetID string) error {
	u, err := s.checkTarget(ctx, actorID, targetID)
	if err != nil {
		return err
	}
	if !u.HasTOTP() {
		return ErrTOTPNotEnabled
	}
	u.TOTPSecret = ""
	u.UpdatedAt = time.Now().UTC()
	if err := s.userRepo.UpdateUser(ctx, u); err != nil {
		return err
	}
	return s.sessions.RevokeAllForUser(ctx, targetID)
}

// ListPasskeys devuelve los passkeys de otra cuenta (solo metadatos).
func (s *Service) ListPasskeys(ctx context.Context, targetID string) ([]*auth.WebAuthnCredential, error) {
	if s.webauthn == nil {
		return nil, nil
	}
	if _, err := s.userRepo.GetUserByID(ctx, targetID); err != nil {
		return nil, err
	}
	return s.webauthn.ListCredentialsForUser(ctx, targetID)
}

// RemovePasskey borra un passkey de otra cuenta (Decisión 10) y revoca sus
// sesiones. credentialID es el id interno de la fila (el que lista
// ListPasskeys), y tiene que pertenecer a targetID.
func (s *Service) RemovePasskey(ctx context.Context, actorID, targetID, credentialID string) error {
	if s.webauthn == nil {
		return auth.ErrWebAuthnCredentialNotFound
	}
	if _, err := s.checkTarget(ctx, actorID, targetID); err != nil {
		return err
	}
	if err := s.webauthn.DeleteCredential(ctx, credentialID, targetID); err != nil {
		return err
	}
	return s.sessions.RevokeAllForUser(ctx, targetID)
}

// RevokeAllAccess cierra todas las sesiones y borra todos los tokens de API
// y WebDAV de la cuenta. Los tokens WebDAV se borran aunque WebDAV esté
// desactivado hoy: si se reactivara, no deben volver a valer.
func (s *Service) RevokeAllAccess(ctx context.Context, targetID string) (Revoked, error) {
	var out Revoked
	if err := s.sessions.RevokeAllForUser(ctx, targetID); err != nil {
		return out, err
	}
	n, err := s.apiTokens.RevokeAllForUser(ctx, targetID)
	if err != nil {
		return out, err
	}
	out.APITokens = n
	if s.webdavTokens != nil {
		n, err = s.webdavTokens.RevokeAllForUser(ctx, targetID)
		if err != nil {
			return out, err
		}
		out.WebDAVTokens = n
	}
	return out, nil
}
