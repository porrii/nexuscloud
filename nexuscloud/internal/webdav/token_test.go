package webdav

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/users"
)

type tokenTestEnv struct {
	users users.Repository
	repo  *SQLTokenRepository
	svc   *TokenService
	now   time.Time // reloj controlable del servicio
}

func newTokenTestEnv(t *testing.T) *tokenTestEnv {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "webdav-token-test.db")

	sqlDB, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open falló: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(cfg, sqlDB); err != nil {
		t.Fatalf("db.Migrate falló: %v", err)
	}
	conn := db.Wrap(cfg.Database.Driver, sqlDB)

	e := &tokenTestEnv{
		users: users.NewSQLRepository(conn),
		repo:  NewSQLTokenRepository(conn),
		now:   time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
	}
	e.svc = NewTokenService(e.repo, e.users, nil)
	e.svc.now = func() time.Time { return e.now }
	return e
}

func (e *tokenTestEnv) createUser(t *testing.T, username string) *users.User {
	t.Helper()
	u, err := users.NewService(e.users).CreateUser(context.Background(), users.CreateUserInput{
		Username: username, PasswordHash: "hash-de-prueba",
	})
	if err != nil {
		t.Fatalf("CreateUser falló: %v", err)
	}
	return u
}

func TestTokenCreateReturnsPrefixedSecretAndStoresOnlyItsHash(t *testing.T) {
	ctx := context.Background()
	e := newTokenTestEnv(t)
	u := e.createUser(t, "maria")

	tok, plain, err := e.svc.Create(ctx, u.ID, "portátil de casa")
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}
	if !strings.HasPrefix(plain, tokenPrefix) || len(plain) < len(tokenPrefix)+40 {
		t.Errorf("token en claro = %q, esperado prefijo %q + ≥40 caracteres de entropía", plain, tokenPrefix)
	}
	if tok.TokenHash != auth.HashToken(plain) {
		t.Error("TokenHash debería ser el SHA-256 del token en claro")
	}
	if strings.Contains(tok.TokenHash, plain) {
		t.Error("el hash almacenado no debe contener el token en claro")
	}
	if tok.Label != "portátil de casa" || !tok.CreatedAt.Equal(e.now) {
		t.Errorf("token = %+v", tok)
	}
}

func TestTokenCreateDefaultsLabelAndRejectsTooLongOne(t *testing.T) {
	ctx := context.Background()
	e := newTokenTestEnv(t)
	u := e.createUser(t, "maria")

	tok, _, err := e.svc.Create(ctx, u.ID, "   ")
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}
	if tok.Label != defaultTokenLabel {
		t.Errorf("Label = %q, esperado %q", tok.Label, defaultTokenLabel)
	}

	if _, _, err := e.svc.Create(ctx, u.ID, strings.Repeat("x", maxTokenLabelLen+1)); !errors.Is(err, ErrInvalidLabel) {
		t.Errorf("err = %v, esperado ErrInvalidLabel", err)
	}
}

func TestAuthenticateSucceedsAndThrottlesLastUsedWrites(t *testing.T) {
	ctx := context.Background()
	e := newTokenTestEnv(t)
	u := e.createUser(t, "maria")
	_, plain, err := e.svc.Create(ctx, u.ID, "portátil")
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}

	got, err := e.svc.Authenticate(ctx, "maria", plain)
	if err != nil {
		t.Fatalf("Authenticate falló: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("usuario = %s, esperado %s", got.ID, u.ID)
	}
	first := lastUsed(t, e, u.ID)
	if first == nil || !first.Equal(e.now) {
		t.Fatalf("last_used_at tras el primer uso = %v, esperado %v", first, e.now)
	}

	// Dentro del minuto no se vuelve a escribir (un cliente WebDAV dispara
	// decenas de peticiones por segundo).
	e.now = e.now.Add(30 * time.Second)
	if _, err := e.svc.Authenticate(ctx, "maria", plain); err != nil {
		t.Fatalf("Authenticate falló: %v", err)
	}
	if again := lastUsed(t, e, u.ID); !again.Equal(*first) {
		t.Errorf("last_used_at cambió dentro del minuto: %v -> %v", first, again)
	}

	e.now = e.now.Add(2 * time.Minute)
	if _, err := e.svc.Authenticate(ctx, "maria", plain); err != nil {
		t.Fatalf("Authenticate falló: %v", err)
	}
	if later := lastUsed(t, e, u.ID); !later.Equal(e.now) {
		t.Errorf("last_used_at pasado el minuto = %v, esperado %v", later, e.now)
	}
}

func lastUsed(t *testing.T, e *tokenTestEnv, userID string) *time.Time {
	t.Helper()
	list, err := e.repo.ListTokensForUser(context.Background(), userID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListTokensForUser = %v, %v", list, err)
	}
	return list[0].LastUsedAt
}

func TestAuthenticateRejectsEveryBadCredentialWithTheSameError(t *testing.T) {
	ctx := context.Background()
	e := newTokenTestEnv(t)
	maria := e.createUser(t, "maria")
	e.createUser(t, "otro")
	_, plain, err := e.svc.Create(ctx, maria.ID, "portátil")
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}

	_, revokedPlain, _ := e.svc.Create(ctx, maria.ID, "perdido")
	list, _ := e.svc.List(ctx, maria.ID)
	for _, tok := range list {
		if tok.TokenHash == auth.HashToken(revokedPlain) {
			if err := e.svc.Revoke(ctx, tok.ID, maria.ID); err != nil {
				t.Fatalf("Revoke falló: %v", err)
			}
		}
	}

	disabled := e.createUser(t, "baja")
	_, disabledPlain, _ := e.svc.Create(ctx, disabled.ID, "x")
	if err := users.NewService(e.users).Disable(ctx, disabled.ID); err != nil {
		t.Fatalf("Disable falló: %v", err)
	}

	cases := []struct{ name, username, token string }{
		{"token desconocido", "maria", tokenPrefix + "no-existe"},
		{"username de otro usuario", "otro", plain},
		{"username vacío", "", plain},
		{"sin prefijo (p.ej. una contraseña de cuenta)", "maria", "contraseña-de-la-cuenta"},
		{"token revocado", "maria", revokedPlain},
		{"usuario deshabilitado", "baja", disabledPlain},
	}
	for _, tc := range cases {
		if _, err := e.svc.Authenticate(ctx, tc.username, tc.token); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: err = %v, esperado ErrInvalidCredentials", tc.name, err)
		}
	}
}

func TestRevokeRefusesAnotherUsersToken(t *testing.T) {
	ctx := context.Background()
	e := newTokenTestEnv(t)
	victima := e.createUser(t, "victima")
	atacante := e.createUser(t, "atacante")
	tok, plain, err := e.svc.Create(ctx, victima.ID, "portátil")
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}

	// El atacante adivina el ID del token de la víctima (IDOR, §198).
	if err := e.svc.Revoke(ctx, tok.ID, atacante.ID); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("Revoke con userID ajeno: err = %v, esperado ErrTokenNotFound", err)
	}
	if _, err := e.svc.Authenticate(ctx, "victima", plain); err != nil {
		t.Errorf("el token de la víctima debería seguir siendo válido: %v", err)
	}

	if err := e.svc.Revoke(ctx, tok.ID, victima.ID); err != nil {
		t.Fatalf("Revoke con el propietario falló: %v", err)
	}
	if _, err := e.svc.Authenticate(ctx, "victima", plain); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("tras revocar: err = %v, esperado ErrInvalidCredentials", err)
	}
}

func TestListOnlyReturnsTheUsersOwnTokens(t *testing.T) {
	ctx := context.Background()
	e := newTokenTestEnv(t)
	ana := e.createUser(t, "ana")
	beto := e.createUser(t, "beto")
	if _, _, err := e.svc.Create(ctx, ana.ID, "de ana"); err != nil {
		t.Fatalf("Create falló: %v", err)
	}
	if _, _, err := e.svc.Create(ctx, beto.ID, "de beto"); err != nil {
		t.Fatalf("Create falló: %v", err)
	}

	list, err := e.svc.List(ctx, ana.ID)
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(list) != 1 || list[0].Label != "de ana" {
		t.Errorf("list = %+v, esperado solo el token de ana", list)
	}
}

func TestDeletingTheUserRemovesTheirTokens(t *testing.T) {
	ctx := context.Background()
	e := newTokenTestEnv(t)
	u := e.createUser(t, "maria")
	_, plain, err := e.svc.Create(ctx, u.ID, "portátil")
	if err != nil {
		t.Fatalf("Create falló: %v", err)
	}

	if err := e.users.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser falló: %v", err)
	}
	if _, err := e.repo.GetTokenByHash(ctx, auth.HashToken(plain)); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("err = %v, esperado ErrTokenNotFound (ON DELETE CASCADE)", err)
	}
}
