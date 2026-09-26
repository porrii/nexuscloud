package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

type testEnv struct {
	users            users.Repository
	sessions         SessionRepository
	invitations      InvitationRepository
	webauthnCreds    WebAuthnCredentialRepository
	webauthnCeremony WebAuthnCeremonyRepository
	apiTokens        APITokenRepository
	hasher           *Hasher
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "auth-test.db")

	sqlDB, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open falló: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(cfg, sqlDB); err != nil {
		t.Fatalf("db.Migrate falló: %v", err)
	}
	conn := db.Wrap(cfg.Database.Driver, sqlDB)

	return &testEnv{
		users:            users.NewSQLRepository(conn),
		sessions:         NewSQLSessionRepository(conn),
		invitations:      NewSQLInvitationRepository(conn),
		webauthnCreds:    NewSQLWebAuthnCredentialRepository(conn),
		webauthnCeremony: NewSQLWebAuthnCeremonyRepository(conn),
		apiTokens:        NewSQLAPITokenRepository(conn),
		hasher:           NewHasher(config.Argon2Config{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}),
	}
}

func (e *testEnv) authenticator() *Authenticator {
	return NewAuthenticator(e.users, e.sessions, e.webauthnCreds, e.hasher, 24, nil)
}

func (e *testEnv) apiTokenService() *APITokenService {
	return NewAPITokenService(e.apiTokens, e.users, nil)
}

func (e *testEnv) webAuthnService(t *testing.T) *WebAuthnService {
	t.Helper()
	svc, err := NewWebAuthnService(
		config.WebAuthnConfig{Enabled: true, RPID: "localhost", RPOrigin: "https://localhost"},
		e.webauthnCreds, e.webauthnCeremony, e.users,
	)
	if err != nil {
		t.Fatalf("NewWebAuthnService falló: %v", err)
	}
	return svc
}

func (e *testEnv) createUser(t *testing.T, ctx context.Context, username, password string) *users.User {
	t.Helper()
	hash, err := e.hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash falló: %v", err)
	}
	u, err := users.NewService(e.users).CreateUser(ctx, users.CreateUserInput{
		Username: username, PasswordHash: hash,
	})
	if err != nil {
		t.Fatalf("CreateUser falló: %v", err)
	}
	return u
}

func TestLoginSucceedsWithCorrectCredentials(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	env.createUser(t, ctx, "ivan", "contraseña-correcta")

	res, err := env.authenticator().Login(ctx, "ivan", "contraseña-correcta", "", "test-device", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login falló: %v", err)
	}
	if res.Token == "" {
		t.Error("Login debería devolver un token en claro")
	}
	if res.Session.UserID != res.User.ID {
		t.Error("la sesión debería pertenecer al usuario autenticado")
	}
}

func TestLoginFailsWithSameErrorForUnknownUserAndWrongPassword(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	env.createUser(t, ctx, "ivan", "contraseña-correcta")

	_, err1 := env.authenticator().Login(ctx, "ivan", "contraseña-incorrecta", "", "", "")
	_, err2 := env.authenticator().Login(ctx, "usuario-que-no-existe", "cualquiera", "", "", "")

	if !errors.Is(err1, ErrAuthenticationFailed) || !errors.Is(err2, ErrAuthenticationFailed) {
		t.Errorf("ambos casos deben devolver ErrAuthenticationFailed (evitar enumeración de usuarios, §27): got %v / %v", err1, err2)
	}
}

func TestLoginRejectsDisabledUser(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")
	if err := users.NewService(env.users).Disable(ctx, u.ID); err != nil {
		t.Fatalf("Disable falló: %v", err)
	}

	_, err := env.authenticator().Login(ctx, "ivan", "contraseña-correcta", "", "", "")
	if !errors.Is(err, ErrUserDisabled) {
		t.Errorf("err = %v, esperado ErrUserDisabled", err)
	}
}

func TestValidateTokenReturnsUserForActiveSession(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	env.createUser(t, ctx, "ivan", "contraseña-correcta")
	a := env.authenticator()

	res, err := a.Login(ctx, "ivan", "contraseña-correcta", "", "", "")
	if err != nil {
		t.Fatalf("Login falló: %v", err)
	}

	u, sess, err := a.ValidateToken(ctx, res.Token)
	if err != nil {
		t.Fatalf("ValidateToken falló: %v", err)
	}
	if u.ID != res.User.ID || sess.ID != res.Session.ID {
		t.Error("ValidateToken debería devolver el mismo usuario/sesión creados en Login")
	}
}

func TestValidateTokenRejectsRevokedSession(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")
	a := env.authenticator()

	res, err := a.Login(ctx, "ivan", "contraseña-correcta", "", "", "")
	if err != nil {
		t.Fatalf("Login falló: %v", err)
	}
	if err := a.Logout(ctx, res.Session.ID, u.ID); err != nil {
		t.Fatalf("Logout falló: %v", err)
	}

	if _, _, err := a.ValidateToken(ctx, res.Token); err == nil {
		t.Error("ValidateToken debería fallar tras revocar la sesión (logout)")
	}
}

func TestLogoutRefusesToRevokeAnotherUsersSession(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	env.createUser(t, ctx, "victima", "contraseña-1")
	atacante := env.createUser(t, ctx, "atacante", "contraseña-2")
	a := env.authenticator()

	victimaRes, err := a.Login(ctx, "victima", "contraseña-1", "", "", "")
	if err != nil {
		t.Fatalf("Login falló: %v", err)
	}

	// El atacante intenta revocar la sesión de la víctima adivinando su ID
	// (comprobación de propiedad = defensa contra IDOR, §198).
	if err := a.Logout(ctx, victimaRes.Session.ID, atacante.ID); err == nil {
		t.Error("Logout no debería permitir revocar la sesión de otro usuario")
	}

	if _, _, err := a.ValidateToken(ctx, victimaRes.Token); err != nil {
		t.Errorf("la sesión de la víctima debería seguir siendo válida: %v", err)
	}
}

func TestLoginRequiresTOTPWhenEnabled(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")

	secret, _, err := GenerateTOTP("NexusCloud", u.Username)
	if err != nil {
		t.Fatalf("GenerateTOTP falló: %v", err)
	}
	u.TOTPSecret = secret
	if err := env.users.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser falló: %v", err)
	}

	_, err = env.authenticator().Login(ctx, "ivan", "contraseña-correcta", "", "", "")
	if !errors.Is(err, ErrTOTPRequired) {
		t.Errorf("err = %v, esperado ErrTOTPRequired", err)
	}
}

func TestInvitationRedeemCreatesUserAndConsumesUse(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	admin := env.createUser(t, ctx, "admin", "contraseña-admin")

	invSvc := NewInvitationService(env.invitations, users.NewService(env.users), env.hasher)
	inv, token, err := invSvc.Create(ctx, CreateInvitationInput{CreatedBy: admin.ID, MaxUses: 1})
	if err != nil {
		t.Fatalf("Create invitación falló: %v", err)
	}
	if token == "" {
		t.Fatal("el token en claro no debería estar vacío")
	}

	u, err := invSvc.Redeem(ctx, RedeemInvitationInput{Token: token, Username: "invitado", Password: "contraseña-invitado"})
	if err != nil {
		t.Fatalf("Redeem falló: %v", err)
	}
	if u.Username != "invitado" {
		t.Errorf("username = %q, esperado invitado", u.Username)
	}

	// Segundo canje debe fallar: max_uses=1 ya consumido.
	_, err = invSvc.Redeem(ctx, RedeemInvitationInput{Token: token, Username: "otro", Password: "otra-contraseña"})
	if !errors.Is(err, ErrInvitationExhausted) {
		t.Errorf("err = %v, esperado ErrInvitationExhausted", err)
	}
	_ = inv
}

func TestInvitationRedeemRejectsExpired(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	admin := env.createUser(t, ctx, "admin", "contraseña-admin")

	// InvitationService.Create trata TTL<=0 como "no especificado" y aplica
	// el default de 7 días (a propósito: una invitación nunca debería
	// nacer ya caducada por un cero accidental) — por eso esta prueba
	// inserta la invitación ya expirada directamente vía el repositorio,
	// saltándose ese defaulting, para ejercitar solo la comprobación de
	// expiración de Redeem/IsUsable.
	token, err := idgen.Token()
	if err != nil {
		t.Fatalf("generando token: %v", err)
	}
	inv := &Invitation{
		ID:        idgen.New(),
		TokenHash: HashToken(token),
		CreatedBy: admin.ID,
		MaxUses:   1,
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
	}
	if err := env.invitations.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("CreateInvitation falló: %v", err)
	}

	invSvc := NewInvitationService(env.invitations, users.NewService(env.users), env.hasher)
	_, err = invSvc.Redeem(ctx, RedeemInvitationInput{Token: token, Username: "tarde", Password: "contraseña"})
	if !errors.Is(err, ErrInvitationExpired) {
		t.Errorf("err = %v, esperado ErrInvitationExpired", err)
	}
}

func TestInvitationRevokedCannotBeRedeemed(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	admin := env.createUser(t, ctx, "admin", "contraseña-admin")

	invSvc := NewInvitationService(env.invitations, users.NewService(env.users), env.hasher)
	inv, token, err := invSvc.Create(ctx, CreateInvitationInput{CreatedBy: admin.ID, MaxUses: 1})
	if err != nil {
		t.Fatalf("Create invitación falló: %v", err)
	}
	if err := env.invitations.RevokeInvitation(ctx, inv.ID); err != nil {
		t.Fatalf("RevokeInvitation falló: %v", err)
	}

	_, err = invSvc.Redeem(ctx, RedeemInvitationInput{Token: token, Username: "tarde", Password: "contraseña"})
	if !errors.Is(err, ErrInvitationRevoked) {
		t.Errorf("err = %v, esperado ErrInvitationRevoked", err)
	}
}
