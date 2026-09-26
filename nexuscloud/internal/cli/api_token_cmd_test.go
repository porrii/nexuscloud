package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
)

func TestParseExpiresIn(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, never := range []string{"", "never", "NEVER", "nunca", "Nunca"} {
		got, err := parseExpiresIn(never, now)
		if err != nil || got != nil {
			t.Errorf("parseExpiresIn(%q) = (%v, %v), esperado (nil, nil)", never, got, err)
		}
	}

	cases := []struct {
		in   string
		want time.Time
	}{
		{"30d", now.Add(30 * 24 * time.Hour)},
		{"1y", now.AddDate(1, 0, 0)},
		{"2y", now.AddDate(2, 0, 0)},
		{"12h", now.Add(12 * time.Hour)},
	}
	for _, c := range cases {
		got, err := parseExpiresIn(c.in, now)
		if err != nil {
			t.Errorf("parseExpiresIn(%q): %v", c.in, err)
			continue
		}
		if got == nil || !got.Equal(c.want) {
			t.Errorf("parseExpiresIn(%q) = %v, esperado %v", c.in, got, c.want)
		}
	}

	for _, bad := range []string{"abc", "-5d", "0d", "-1y", "0h", "d", "y"} {
		if _, err := parseExpiresIn(bad, now); err == nil {
			t.Errorf("parseExpiresIn(%q) debería fallar", bad)
		}
	}
}

// parseAPIToken extrae el token que imprime "api-token create" (el único
// campo con el prefijo nat_) -- mismo criterio que parseWebDAVToken.
func parseAPIToken(t *testing.T, createOutput string) string {
	t.Helper()
	for _, field := range strings.Fields(createOutput) {
		if strings.HasPrefix(field, "nat_") {
			return field
		}
	}
	t.Fatalf("no encontré ningún token nat_ en la salida:\n%s", createOutput)
	return ""
}

func TestUsersAPITokenCreateListRevoke(t *testing.T) {
	username := newCLITestUser(t)

	out, err := runUsers(t, "api-token", "list", "--username", username)
	if err != nil || !strings.Contains(out, "no tiene tokens") {
		t.Fatalf("list inicial: err %v, salida %q; esperado el aviso de que no tiene tokens", err, out)
	}

	out, err = runUsers(t, "api-token", "create", "--username", username, "--label", "script de backup")
	if err != nil {
		t.Fatalf("create falló: %v (salida: %s)", err, out)
	}
	token := parseAPIToken(t, out)
	if !strings.Contains(out, "No expira") {
		t.Errorf("sin --expires-in, create debería avisar de que no expira:\n%s", out)
	}

	// El token impreso es un token VÁLIDO de verdad, no solo texto con pinta de token.
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	svc := openAPITokenService(cfg, sqlDB, userRepo)
	u, _, err := svc.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("el token creado por el CLI debería autenticar: %v", err)
	}
	if u.Username != username {
		t.Errorf("Authenticate devolvió %q, esperado %q", u.Username, username)
	}
	sqlDB.Close()

	out, err = runUsers(t, "api-token", "list", "--username", username)
	if err != nil || !strings.Contains(out, "script de backup") {
		t.Fatalf("list tras crear: err %v, salida %q", err, out)
	}
	if strings.Contains(out, token) {
		t.Error("list nunca debe mostrar el token en claro")
	}
	id := strings.Fields(out)[0]

	if out, err := runUsers(t, "api-token", "revoke", id, "--username", username); err != nil {
		t.Fatalf("revoke falló: %v (salida: %s)", err, out)
	}
	sqlDB, userRepo, err = openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()
	if _, _, err := openAPITokenService(cfg, sqlDB, userRepo).Authenticate(context.Background(), token); !errors.Is(err, auth.ErrInvalidAPICredentials) {
		t.Errorf("tras revocar: err = %v, esperado ErrInvalidAPICredentials", err)
	}
}

func TestUsersAPITokenCreateWithExpiresIn(t *testing.T) {
	username := newCLITestUser(t)

	out, err := runUsers(t, "api-token", "create", "--username", username, "--expires-in", "1d")
	if err != nil {
		t.Fatalf("create falló: %v (salida: %s)", err, out)
	}
	if !strings.Contains(out, "Expira el") {
		t.Errorf("con --expires-in, create debería anunciar la fecha de expiración:\n%s", out)
	}
}

func TestUsersAPITokenCreateRejectsInvalidExpiresIn(t *testing.T) {
	username := newCLITestUser(t)
	if _, err := runUsers(t, "api-token", "create", "--username", username, "--expires-in", "no-es-una-fecha"); err == nil {
		t.Fatal("esperaba error con --expires-in inválido")
	}
}

func TestUsersAPITokenRevokeRejectsUnknownToken(t *testing.T) {
	username := newCLITestUser(t)
	if _, err := runUsers(t, "api-token", "revoke", "token-que-no-existe", "--username", username); err == nil {
		t.Fatal("esperaba error al revocar un token inexistente")
	}
}

func TestUsersAPITokenCommandsRequireUsername(t *testing.T) {
	newCLITestUser(t)
	for _, sub := range [][]string{{"create"}, {"list"}, {"revoke", "x"}} {
		if _, err := runUsers(t, append([]string{"api-token"}, sub...)...); err == nil {
			t.Errorf("api-token %v sin --username debería fallar", sub)
		}
	}
}
