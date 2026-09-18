package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

var ErrWebAuthnCeremonyMismatch = errors.New("auth: la ceremonia WebAuthn no corresponde a este usuario o paso")

// webAuthnCeremonyTTL: el propio estándar espera una respuesta del
// usuario en segundos, nunca minutos -- 5 minutos da margen de sobra sin
// dejar ceremonias abandonadas acumulándose mucho tiempo en la tabla.
const webAuthnCeremonyTTL = 5 * time.Minute

// WebAuthnService orquesta Passkeys/WebAuthn (§25, ADR-033) envolviendo
// go-webauthn/webauthn. Vive en internal/auth por el mismo motivo que
// Authenticator: depende de users.Repository, nunca al revés. Un mismo
// passkey sirve tanto de segundo factor (BeginLogin/FinishLogin, con el
// usuario ya conocido tras la contraseña) como de login sin contraseña
// (BeginDiscoverableLogin/FinishDiscoverableLogin) -- por eso el registro
// (BeginRegistration) siempre exige ResidentKeyRequirementRequired: sin
// eso el credential no sería "discoverable" y el segundo modo no
// funcionaría con él.
//
// Se asume que solo se construye cuando security.webAuthn.enabled=true
// (comprobado por config.Validate y decidido por el código de arranque,
// igual que clientupdates.Proxy/internal/server): un RPID/RPOrigin vacíos
// harían que webauthn.New devolviera error.
type WebAuthnService struct {
	webauthn    *webauthn.WebAuthn
	credentials WebAuthnCredentialRepository
	ceremonies  WebAuthnCeremonyRepository
	users       users.Repository
}

func NewWebAuthnService(cfg config.WebAuthnConfig, credentials WebAuthnCredentialRepository, ceremonies WebAuthnCeremonyRepository, userRepo users.Repository) (*WebAuthnService, error) {
	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "NexusCloud",
		RPID:          cfg.RPID,
		RPOrigins:     []string{cfg.RPOrigin},
	})
	if err != nil {
		return nil, fmt.Errorf("configurando WebAuthn: %w", err)
	}
	return &WebAuthnService{webauthn: w, credentials: credentials, ceremonies: ceremonies, users: userRepo}, nil
}

// webAuthnUser adapta users.User + sus credenciales ya persistidas a la
// interfaz webauthn.User que exige la librería. WebAuthnID es el propio
// ID interno del usuario (idgen.New(), 36 bytes, muy por debajo del
// máximo de 64) -- no hace falta un handle aparte, y así el
// DiscoverableUserHandler del login passwordless puede resolver
// directamente el usuario a partir de lo que devuelve el navegador.
type webAuthnUser struct {
	user        *users.User
	credentials []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte                         { return []byte(u.user.ID) }
func (u *webAuthnUser) WebAuthnName() string                       { return u.user.Username }
func (u *webAuthnUser) WebAuthnDisplayName() string                { return u.user.DisplayName }
func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// loadWebAuthnUser reconstruye el adaptador para un usuario ya conocido
// (registro, login de segundo factor). decodeWebAuthnCredentials separado
// porque WebAuthnCredentials() no puede devolver error -- cualquier fallo
// de decodificación (solo posible si la fila está corrupta) se detecta
// aquí, antes de construir el adaptador, en vez de silenciarse dentro de
// la interfaz.
func (s *WebAuthnService) loadWebAuthnUser(ctx context.Context, userID string) (*webAuthnUser, error) {
	u, err := s.users.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.credentials.ListCredentialsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	creds, err := decodeWebAuthnCredentials(rows)
	if err != nil {
		return nil, err
	}
	return &webAuthnUser{user: u, credentials: creds}, nil
}

func decodeWebAuthnCredentials(rows []*WebAuthnCredential) ([]webauthn.Credential, error) {
	out := make([]webauthn.Credential, 0, len(rows))
	for _, r := range rows {
		id, err := base64.RawURLEncoding.DecodeString(r.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("decodificando credential_id de %s: %w", r.ID, err)
		}
		pub, err := base64.RawURLEncoding.DecodeString(r.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("decodificando public_key de %s: %w", r.ID, err)
		}
		out = append(out, webauthn.Credential{
			ID:            id,
			PublicKey:     pub,
			Authenticator: webauthn.Authenticator{SignCount: r.SignCount},
		})
	}
	return out, nil
}

func (s *WebAuthnService) saveCeremony(ctx context.Context, userID, purpose string, session *webauthn.SessionData) (string, error) {
	data, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("serializando estado de la ceremonia: %w", err)
	}
	now := time.Now().UTC()
	c := &WebAuthnCeremony{
		ID:          idgen.New(),
		UserID:      userID,
		Purpose:     purpose,
		SessionData: string(data),
		CreatedAt:   now,
		ExpiresAt:   now.Add(webAuthnCeremonyTTL),
	}
	if err := s.ceremonies.CreateCeremony(ctx, c); err != nil {
		return "", err
	}
	return c.ID, nil
}

// loadCeremony recupera y borra la ceremonia (uso único, igual que un
// token de invitación consumido) y comprueba que corresponde al usuario y
// paso esperados antes de devolver el SessionData ya deserializado.
func (s *WebAuthnService) loadCeremony(ctx context.Context, ceremonyID, wantUserID, wantPurpose string) (*webauthn.SessionData, error) {
	c, err := s.ceremonies.GetCeremony(ctx, ceremonyID)
	if err != nil {
		return nil, err
	}
	if err := s.ceremonies.DeleteCeremony(ctx, ceremonyID); err != nil {
		return nil, err
	}
	if c.Purpose != wantPurpose || c.UserID != wantUserID {
		return nil, ErrWebAuthnCeremonyMismatch
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(c.SessionData), &session); err != nil {
		return nil, fmt.Errorf("deserializando estado de la ceremonia: %w", err)
	}
	return &session, nil
}

// BeginRegistration inicia el alta de un passkey nuevo para un usuario ya
// autenticado (§198: la sesión de la petición ya garantiza quién es).
func (s *WebAuthnService) BeginRegistration(ctx context.Context, userID string) (*protocol.CredentialCreation, string, error) {
	wu, err := s.loadWebAuthnUser(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	creation, session, err := s.webauthn.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(wu.WebAuthnCredentials()).CredentialDescriptors()),
	)
	if err != nil {
		return nil, "", fmt.Errorf("iniciando registro WebAuthn: %w", err)
	}
	ceremonyID, err := s.saveCeremony(ctx, userID, WebAuthnCeremonyPurposeRegistration, session)
	if err != nil {
		return nil, "", err
	}
	return creation, ceremonyID, nil
}

// FinishRegistration completa el alta y persiste el nuevo passkey.
func (s *WebAuthnService) FinishRegistration(ctx context.Context, userID, ceremonyID, label string, body io.Reader) (*WebAuthnCredential, error) {
	session, err := s.loadCeremony(ctx, ceremonyID, userID, WebAuthnCeremonyPurposeRegistration)
	if err != nil {
		return nil, err
	}
	wu, err := s.loadWebAuthnUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(body)
	if err != nil {
		return nil, fmt.Errorf("interpretando la respuesta del navegador: %w", err)
	}
	credential, err := s.webauthn.CreateCredential(wu, *session, parsed)
	if err != nil {
		return nil, fmt.Errorf("validando el nuevo passkey: %w", err)
	}

	now := time.Now().UTC()
	stored := &WebAuthnCredential{
		ID:           idgen.New(),
		UserID:       userID,
		CredentialID: base64.RawURLEncoding.EncodeToString(credential.ID),
		PublicKey:    base64.RawURLEncoding.EncodeToString(credential.PublicKey),
		SignCount:    credential.Authenticator.SignCount,
		Label:        label,
		CreatedAt:    now,
	}
	if err := s.credentials.CreateCredential(ctx, stored); err != nil {
		return nil, err
	}
	return stored, nil
}

// BeginLogin inicia una comprobación de passkey como segundo factor: el
// usuario ya se conoce (verificó contraseña), igual que TOTP.
func (s *WebAuthnService) BeginLogin(ctx context.Context, userID string) (*protocol.CredentialAssertion, string, error) {
	wu, err := s.loadWebAuthnUser(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	assertion, session, err := s.webauthn.BeginLogin(wu)
	if err != nil {
		return nil, "", fmt.Errorf("iniciando login WebAuthn: %w", err)
	}
	ceremonyID, err := s.saveCeremony(ctx, userID, WebAuthnCeremonyPurposeLogin, session)
	if err != nil {
		return nil, "", err
	}
	return assertion, ceremonyID, nil
}

// FinishLogin completa la comprobación de segundo factor y actualiza el
// sign_count del passkey usado -- la propia protección contra replay que
// exige el estándar, nunca opcional (ver doc de WebAuthnCredentialRepository).
func (s *WebAuthnService) FinishLogin(ctx context.Context, userID, ceremonyID string, body io.Reader) (*WebAuthnCredential, error) {
	session, err := s.loadCeremony(ctx, ceremonyID, userID, WebAuthnCeremonyPurposeLogin)
	if err != nil {
		return nil, err
	}
	wu, err := s.loadWebAuthnUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(body)
	if err != nil {
		return nil, fmt.Errorf("interpretando la respuesta del navegador: %w", err)
	}
	credential, err := s.webauthn.ValidateLogin(wu, *session, parsed)
	if err != nil {
		return nil, fmt.Errorf("validando el passkey: %w", err)
	}
	return s.persistLoginResult(ctx, credential)
}

// BeginDiscoverableLogin inicia un login sin contraseña: el servidor
// todavía no sabe qué usuario está entrando (por eso no hay userID aquí),
// eso lo resuelve FinishDiscoverableLogin a partir de lo que el propio
// navegador presenta.
func (s *WebAuthnService) BeginDiscoverableLogin(ctx context.Context) (*protocol.CredentialAssertion, string, error) {
	assertion, session, err := s.webauthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", fmt.Errorf("iniciando login passwordless: %w", err)
	}
	ceremonyID, err := s.saveCeremony(ctx, "", WebAuthnCeremonyPurposeLogin, session)
	if err != nil {
		return nil, "", err
	}
	return assertion, ceremonyID, nil
}

// FinishDiscoverableLogin completa el login sin contraseña y devuelve
// tanto el usuario resuelto como el passkey usado (con el sign_count ya
// actualizado).
func (s *WebAuthnService) FinishDiscoverableLogin(ctx context.Context, ceremonyID string, body io.Reader) (*users.User, *WebAuthnCredential, error) {
	session, err := s.loadCeremony(ctx, ceremonyID, "", WebAuthnCeremonyPurposeLogin)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(body)
	if err != nil {
		return nil, nil, fmt.Errorf("interpretando la respuesta del navegador: %w", err)
	}

	var resolved *webAuthnUser
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		wu, err := s.loadWebAuthnUser(ctx, string(userHandle))
		if err != nil {
			return nil, err
		}
		resolved = wu
		return wu, nil
	}

	_, credential, err := s.webauthn.ValidatePasskeyLogin(handler, *session, parsed)
	if err != nil {
		return nil, nil, fmt.Errorf("validando el passkey: %w", err)
	}
	if resolved == nil {
		return nil, nil, ErrWebAuthnCeremonyMismatch
	}
	stored, err := s.persistLoginResult(ctx, credential)
	if err != nil {
		return nil, nil, err
	}
	return resolved.user, stored, nil
}

// persistLoginResult guarda el sign_count/last_used_at devueltos por una
// validación de login correcta -- ver el comentario de
// WebAuthnCredentialRepository.UpdateSignCount sobre por qué esta
// escritura es obligatoria, no opcional.
func (s *WebAuthnService) persistLoginResult(ctx context.Context, credential *webauthn.Credential) (*WebAuthnCredential, error) {
	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)
	stored, err := s.credentials.GetCredentialByCredentialID(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := s.credentials.UpdateSignCount(ctx, stored.ID, credential.Authenticator.SignCount, now); err != nil {
		return nil, err
	}
	stored.SignCount = credential.Authenticator.SignCount
	stored.LastUsedAt = &now
	return stored, nil
}

// ListCredentials devuelve los passkeys del propio usuario (para que los
// gestione desde su cuenta).
func (s *WebAuthnService) ListCredentials(ctx context.Context, userID string) ([]*WebAuthnCredential, error) {
	return s.credentials.ListCredentialsForUser(ctx, userID)
}

// RevokeCredential exige coincidencia de userID (comprobado también en el
// repositorio, defensa en profundidad contra IDOR, §198).
func (s *WebAuthnService) RevokeCredential(ctx context.Context, id, userID string) error {
	return s.credentials.DeleteCredential(ctx, id, userID)
}
