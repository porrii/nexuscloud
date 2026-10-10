package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// Modo «sudo» (§126, ADR-042 Decisión 2).

func TestRecentlyReauthenticatedWindow(t *testing.T) {
	now := time.Now().UTC()
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	cases := []struct {
		name string
		at   *time.Time
		want bool
	}{
		{"nunca", nil, false},
		{"hace un minuto", at(-time.Minute), true},
		{"justo en el límite", at(-ReauthWindow), false},
		{"hace once minutos", at(-11 * time.Minute), false},
		{"en el futuro (reloj desfasado)", at(time.Minute), false},
	}
	for _, c := range cases {
		s := &Session{ReauthenticatedAt: c.at}
		if got := s.RecentlyReauthenticated(now); got != c.want {
			t.Errorf("%s: %v, esperado %v", c.name, got, c.want)
		}
	}
}

func TestReauthenticateMarksTheSessionAndPersists(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	env.createUser(t, ctx, "ivan", "contraseña-correcta")
	res, err := env.authenticator().Login(ctx, "ivan", "contraseña-correcta", "", "", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if res.Session.RecentlyReauthenticated(time.Now().UTC()) {
		t.Fatal("una sesión recién creada no debería estar reautenticada")
	}

	if err := env.authenticator().Reauthenticate(ctx, res.Session, "incorrecta", ""); !errors.Is(err, ErrAuthenticationFailed) {
		t.Errorf("contraseña incorrecta: err = %v", err)
	}
	if err := env.authenticator().Reauthenticate(ctx, res.Session, "contraseña-correcta", ""); err != nil {
		t.Fatalf("Reauthenticate: %v", err)
	}

	// La marca se guarda: ValidateToken la devuelve en la siguiente petición.
	_, sess, err := env.authenticator().ValidateToken(ctx, res.Token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if !sess.RecentlyReauthenticated(time.Now().UTC()) {
		t.Errorf("la reautenticación no se persistió: %+v", sess.ReauthenticatedAt)
	}
}

func TestReauthenticateRequiresTOTPWhenEnabled(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")
	res, err := env.authenticator().Login(ctx, "ivan", "contraseña-correcta", "", "", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	secret, _, err := GenerateTOTP("NexusCloud", u.Username)
	if err != nil {
		t.Fatal(err)
	}
	u.TOTPSecret = secret
	if err := env.users.UpdateUser(ctx, u); err != nil {
		t.Fatal(err)
	}

	if err := env.authenticator().Reauthenticate(ctx, res.Session, "contraseña-correcta", ""); !errors.Is(err, ErrTOTPRequired) {
		t.Errorf("sin código: err = %v, esperado ErrTOTPRequired", err)
	}
	if err := env.authenticator().Reauthenticate(ctx, res.Session, "contraseña-correcta", "no-es-un-código"); !errors.Is(err, ErrTOTPInvalid) {
		t.Errorf("código mal formado: err = %v, esperado ErrTOTPInvalid", err)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := env.authenticator().Reauthenticate(ctx, res.Session, "contraseña-correcta", code); err != nil {
		t.Errorf("con código válido: %v", err)
	}
}

func TestMarkReauthenticatedRefusesAnotherUsersOrRevokedSession(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	env.createUser(t, ctx, "ivan", "contraseña-correcta")
	otra := env.createUser(t, ctx, "otra", "contraseña-correcta")
	res, err := env.authenticator().Login(ctx, "ivan", "contraseña-correcta", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := env.sessions.MarkReauthenticated(ctx, res.Session.ID, otra.ID, now); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("sesión de otro usuario: err = %v", err)
	}
	if err := env.sessions.RevokeSession(ctx, res.Session.ID, res.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := env.sessions.MarkReauthenticated(ctx, res.Session.ID, res.User.ID, now); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("sesión revocada: err = %v", err)
	}
}
