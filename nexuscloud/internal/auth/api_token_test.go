package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/users"
)

// api_tokens (§78, ADR-037): mismo patrón que el token de acceso WebDAV
// (webdav.TokenService), pero de propósito general -- autentica Bearer
// contra la API REST completa, no solo WebDAV -- y con expiración opcional,
// que el token WebDAV ni siquiera tiene. Alcance todo-o-nada: el token
// actúa exactamente como el usuario, sin permisos más finos.

func TestAPITokenCreateReturnsPlaintextOnceAndPersistsOnlyTheHash(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-larga-123")

	tok, plain, err := env.apiTokenService().Create(ctx, u.ID, "script de backup", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(plain, "nat_") {
		t.Errorf("el token en claro debería empezar por 'nat_', got %q", plain)
	}
	if tok.TokenHash == plain || tok.TokenHash != HashToken(plain) {
		t.Error("solo debe persistirse el hash del token, nunca el valor en claro")
	}
	if tok.Label != "script de backup" {
		t.Errorf("Label = %q, esperado 'script de backup'", tok.Label)
	}
	if tok.ExpiresAt != nil {
		t.Error("sin expiración pedida, ExpiresAt debe quedar nil (nunca expira)")
	}
}

func TestAPITokenCreateDefaultsAndValidatesLabel(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-larga-123")

	tok, _, err := env.apiTokenService().Create(ctx, u.ID, "", nil)
	if err != nil {
		t.Fatalf("Create con label vacío: %v", err)
	}
	if tok.Label == "" {
		t.Error("un label vacío debe sustituirse por uno por defecto, no quedar vacío")
	}

	tooLong := strings.Repeat("a", 101)
	if _, _, err := env.apiTokenService().Create(ctx, u.ID, tooLong, nil); !errors.Is(err, ErrInvalidAPITokenLabel) {
		t.Errorf("label de 101 runas = %v, esperado ErrInvalidAPITokenLabel", err)
	}
}

func TestAPITokenAuthenticateSucceedsWithValidToken(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-larga-123")
	_, plain, err := env.apiTokenService().Create(ctx, u.ID, "mi token", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, tok, err := env.apiTokenService().Authenticate(ctx, plain)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("Authenticate devolvió el usuario %q, esperado %q", got.ID, u.ID)
	}
	if tok.LastUsedAt == nil {
		t.Error("tras autenticar, LastUsedAt debería quedar poblado")
	}
}

func TestAPITokenAuthenticateRejectsGarbageAndWrongPrefix(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)

	for _, bad := range []string{"", "cualquier-cosa", "nwd_" + strings.Repeat("a", 40), "nat_no-existe-de-verdad"} {
		if _, _, err := env.apiTokenService().Authenticate(ctx, bad); !errors.Is(err, ErrInvalidAPICredentials) {
			t.Errorf("Authenticate(%q) = %v, esperado ErrInvalidAPICredentials", bad, err)
		}
	}
}

func TestAPITokenAuthenticateFailsWhenExpired(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-larga-123")
	past := time.Now().UTC().Add(-time.Hour)
	_, plain, err := env.apiTokenService().Create(ctx, u.ID, "expirado", &past)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, _, err := env.apiTokenService().Authenticate(ctx, plain); !errors.Is(err, ErrInvalidAPICredentials) {
		t.Errorf("Authenticate con token expirado = %v, esperado ErrInvalidAPICredentials", err)
	}
}

func TestAPITokenAuthenticateSucceedsWithFutureExpiry(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-larga-123")
	future := time.Now().UTC().Add(24 * time.Hour)
	_, plain, err := env.apiTokenService().Create(ctx, u.ID, "expira mañana", &future)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, _, err := env.apiTokenService().Authenticate(ctx, plain); err != nil {
		t.Errorf("Authenticate con expiración futura = %v, esperado éxito", err)
	}
}

func TestAPITokenAuthenticateFailsForDisabledUser(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-larga-123")
	_, plain, err := env.apiTokenService().Create(ctx, u.ID, "mi token", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := users.NewService(env.users).Disable(ctx, u.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, _, err := env.apiTokenService().Authenticate(ctx, plain); !errors.Is(err, ErrInvalidAPICredentials) {
		t.Errorf("Authenticate de un usuario deshabilitado = %v, esperado ErrInvalidAPICredentials", err)
	}
}

func TestAPITokenRevokeRequiresOwnerMatch(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.createUser(t, ctx, "ivan", "contraseña-larga-123")
	other := env.createUser(t, ctx, "maria", "contraseña-larga-456")
	svc := env.apiTokenService()
	tok, plain, err := svc.Create(ctx, owner.ID, "mi token", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Revoke(ctx, tok.ID, other.ID); !errors.Is(err, ErrAPITokenNotFound) {
		t.Errorf("revocar el token de otro usuario = %v, esperado ErrAPITokenNotFound", err)
	}
	if _, _, err := svc.Authenticate(ctx, plain); err != nil {
		t.Errorf("tras el intento fallido, el token debería seguir funcionando: %v", err)
	}

	if err := svc.Revoke(ctx, tok.ID, owner.ID); err != nil {
		t.Fatalf("Revoke por el propietario: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, plain); !errors.Is(err, ErrInvalidAPICredentials) {
		t.Errorf("tras revocar, Authenticate = %v, esperado ErrInvalidAPICredentials", err)
	}
}

func TestAPITokenListForUserOnlyReturnsOwnTokens(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	ana := env.createUser(t, ctx, "ana", "contraseña-larga-123")
	bea := env.createUser(t, ctx, "bea", "contraseña-larga-456")
	svc := env.apiTokenService()
	if _, _, err := svc.Create(ctx, ana.ID, "de ana", nil); err != nil {
		t.Fatalf("Create ana: %v", err)
	}
	if _, _, err := svc.Create(ctx, bea.ID, "de bea", nil); err != nil {
		t.Fatalf("Create bea: %v", err)
	}

	anaTokens, err := svc.List(ctx, ana.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(anaTokens) != 1 || anaTokens[0].Label != "de ana" {
		t.Errorf("List(ana) = %+v, esperado solo el token de ana", anaTokens)
	}
}
