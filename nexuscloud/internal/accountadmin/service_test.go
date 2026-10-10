package accountadmin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/users"
	"github.com/porrii/nexuscloud/internal/webdav"
)

type env struct {
	svc      *Service
	userRepo users.Repository
	userSvc  *users.Service
	sessions auth.SessionRepository
	apiRepo  auth.APITokenRepository
	davRepo  webdav.TokenRepository
	hasher   *auth.Hasher
	authn    *auth.Authenticator
}

func newEnv(t *testing.T) *env {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "accountadmin-test.db")
	sqlDB, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(cfg, sqlDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	conn := db.Wrap(cfg.Database.Driver, sqlDB)

	e := &env{
		userRepo: users.NewSQLRepository(conn),
		sessions: auth.NewSQLSessionRepository(conn),
		apiRepo:  auth.NewSQLAPITokenRepository(conn),
		davRepo:  webdav.NewSQLTokenRepository(conn),
		hasher:   auth.NewHasher(config.Argon2Config{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}),
	}
	e.userSvc = users.NewService(e.userRepo)
	webauthnRepo := auth.NewSQLWebAuthnCredentialRepository(conn)
	e.svc = New(e.userSvc, e.userRepo, e.hasher, e.sessions, e.apiRepo, e.davRepo, webauthnRepo)
	e.authn = auth.NewAuthenticator(e.userRepo, e.sessions, webauthnRepo, e.hasher, 24, nil)
	return e
}

func (e *env) createUser(t *testing.T, username, password, role string) *users.User {
	t.Helper()
	hash, err := e.hasher.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	u, err := e.userSvc.CreateUser(context.Background(), users.CreateUserInput{Username: username, PasswordHash: hash, Role: role})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return u
}

func TestResetPasswordRevokesSessionsAndAllTokens(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	root := e.createUser(t, "raiz", "contrasena-raiz", users.RoleSuperAdmin)
	ana := e.createUser(t, "ana", "contrasena-vieja", users.RoleUser)

	login, err := e.authn.Login(ctx, "ana", "contrasena-vieja", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.NewAPITokenService(e.apiRepo, e.userRepo, nil).Create(ctx, ana.ID, "script", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := webdav.NewTokenService(e.davRepo, e.userRepo, nil).Create(ctx, ana.ID, "portátil"); err != nil {
		t.Fatal(err)
	}

	revoked, err := e.svc.ResetPassword(ctx, root.ID, ana.ID, "contrasena-nueva")
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if revoked.APITokens != 1 || revoked.WebDAVTokens != 1 {
		t.Errorf("revocados = %+v, esperado 1 y 1", revoked)
	}
	if _, _, err := e.authn.ValidateToken(ctx, login.Token); err == nil {
		t.Error("la sesión abierta con la contraseña vieja sigue valiendo")
	}
	if _, err := e.authn.Login(ctx, "ana", "contrasena-vieja", "", "", ""); err == nil {
		t.Error("la contraseña vieja sigue valiendo")
	}
	if _, err := e.authn.Login(ctx, "ana", "contrasena-nueva", "", "", ""); err != nil {
		t.Errorf("la contraseña nueva no vale: %v", err)
	}
}

func TestResetPasswordRules(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	root := e.createUser(t, "raiz", "contrasena-raiz", users.RoleSuperAdmin)
	admin := e.createUser(t, "admin", "contrasena-admin", users.RoleAdministrator)
	ana := e.createUser(t, "ana", "contrasena-vieja", users.RoleUser)

	if _, err := e.svc.ResetPassword(ctx, root.ID, ana.ID, "corta"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("contraseña corta: err = %v", err)
	}
	if _, err := e.svc.ResetPassword(ctx, root.ID, root.ID, "contrasena-nueva"); !errors.Is(err, ErrSelfAction) {
		t.Errorf("sobre la propia cuenta: err = %v", err)
	}
	if _, err := e.svc.ResetPassword(ctx, admin.ID, root.ID, "contrasena-nueva"); !errors.Is(err, users.ErrSuperAdminProtected) {
		t.Errorf("administrator sobre super_admin: err = %v", err)
	}
	if _, err := e.svc.ResetPassword(ctx, root.ID, "no-existe", "contrasena-nueva"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("cuenta inexistente: err = %v", err)
	}
	// La CLI (actor vacío) no está sujeta a las reglas entre administradores.
	if _, err := e.svc.ResetPassword(ctx, "", root.ID, "contrasena-nueva"); err != nil {
		t.Errorf("desde la CLI: %v", err)
	}
}

func TestRemoveTOTPClearsSecretAndClosesSessions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	root := e.createUser(t, "raiz", "contrasena-raiz", users.RoleSuperAdmin)
	ana := e.createUser(t, "ana", "contrasena-ana", users.RoleUser)

	if err := e.svc.RemoveTOTP(ctx, root.ID, ana.ID); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Errorf("sin 2FA: err = %v", err)
	}

	login, err := e.authn.Login(ctx, "ana", "contrasena-ana", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := auth.GenerateTOTP("NexusCloud", "ana")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := e.userRepo.GetUserByID(ctx, ana.ID)
	u.TOTPSecret = secret
	u.UpdatedAt = time.Now().UTC()
	if err := e.userRepo.UpdateUser(ctx, u); err != nil {
		t.Fatal(err)
	}

	if err := e.svc.RemoveTOTP(ctx, root.ID, ana.ID); err != nil {
		t.Fatalf("RemoveTOTP: %v", err)
	}
	if u, _ := e.userRepo.GetUserByID(ctx, ana.ID); u.HasTOTP() {
		t.Error("el secreto TOTP sigue guardado")
	}
	if _, _, err := e.authn.ValidateToken(ctx, login.Token); err == nil {
		t.Error("las sesiones abiertas siguen valiendo tras quitar el 2FA")
	}
}

func TestRemovePasskeyOnlyFromThatAccount(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	root := e.createUser(t, "raiz", "contrasena-raiz", users.RoleSuperAdmin)
	ana := e.createUser(t, "ana", "contrasena-ana", users.RoleUser)
	bea := e.createUser(t, "bea", "contrasena-bea", users.RoleUser)

	if err := e.svc.RemovePasskey(ctx, root.ID, bea.ID, "credencial-de-ana"); !errors.Is(err, auth.ErrWebAuthnCredentialNotFound) {
		t.Errorf("passkey inexistente: err = %v", err)
	}
	keys, err := e.svc.ListPasskeys(ctx, ana.ID)
	if err != nil || len(keys) != 0 {
		t.Errorf("ListPasskeys = %v, %v", keys, err)
	}
}
