package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

func TestWebAuthnCredentialRepositoryCRUD(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")

	cred := &WebAuthnCredential{
		ID: idgen.New(), UserID: u.ID, CredentialID: "cred-abc123",
		PublicKey: "pubkey-base64", SignCount: 0, Label: "portátil de trabajo",
		CreatedAt: time.Now().UTC(),
	}
	if err := env.webauthnCreds.CreateCredential(ctx, cred); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	got, err := env.webauthnCreds.GetCredentialByCredentialID(ctx, "cred-abc123")
	if err != nil {
		t.Fatalf("GetCredentialByCredentialID: %v", err)
	}
	if got.ID != cred.ID || got.Label != "portátil de trabajo" || got.SignCount != 0 {
		t.Errorf("got = %+v", got)
	}
	if got.LastUsedAt != nil {
		t.Error("last_used_at debería ser nil antes del primer login")
	}

	list, err := env.webauthnCreds.ListCredentialsForUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListCredentialsForUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != cred.ID {
		t.Errorf("list = %+v, esperada 1 credencial", list)
	}

	now := time.Now().UTC()
	if err := env.webauthnCreds.UpdateSignCount(ctx, cred.ID, 7, now); err != nil {
		t.Fatalf("UpdateSignCount: %v", err)
	}
	got, err = env.webauthnCreds.GetCredentialByCredentialID(ctx, "cred-abc123")
	if err != nil {
		t.Fatalf("GetCredentialByCredentialID tras update: %v", err)
	}
	if got.SignCount != 7 || got.LastUsedAt == nil {
		t.Errorf("got = %+v, esperado sign_count=7 y last_used_at puesto", got)
	}

	// IDOR: borrar con el userID equivocado no debe afectar a la credencial.
	otro := env.createUser(t, ctx, "otro", "contraseña-otro")
	if err := env.webauthnCreds.DeleteCredential(ctx, cred.ID, otro.ID); !errors.Is(err, ErrWebAuthnCredentialNotFound) {
		t.Errorf("DeleteCredential con userID ajeno: err = %v, esperado ErrWebAuthnCredentialNotFound", err)
	}
	if _, err := env.webauthnCreds.GetCredentialByCredentialID(ctx, "cred-abc123"); err != nil {
		t.Errorf("la credencial no debería haberse borrado: %v", err)
	}

	if err := env.webauthnCreds.DeleteCredential(ctx, cred.ID, u.ID); err != nil {
		t.Fatalf("DeleteCredential con el propietario correcto: %v", err)
	}
	if _, err := env.webauthnCreds.GetCredentialByCredentialID(ctx, "cred-abc123"); !errors.Is(err, ErrWebAuthnCredentialNotFound) {
		t.Errorf("err = %v, esperado ErrWebAuthnCredentialNotFound tras borrar", err)
	}
}

func TestWebAuthnCeremonyRepositoryCRUD(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")

	now := time.Now().UTC()
	c := &WebAuthnCeremony{
		ID: idgen.New(), UserID: u.ID, Purpose: WebAuthnCeremonyPurposeRegistration,
		SessionData: `{"challenge":"abc"}`, CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
	if err := env.webauthnCeremony.CreateCeremony(ctx, c); err != nil {
		t.Fatalf("CreateCeremony: %v", err)
	}

	got, err := env.webauthnCeremony.GetCeremony(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCeremony: %v", err)
	}
	if got.UserID != u.ID || got.Purpose != WebAuthnCeremonyPurposeRegistration || got.SessionData != c.SessionData {
		t.Errorf("got = %+v", got)
	}

	if err := env.webauthnCeremony.DeleteCeremony(ctx, c.ID); err != nil {
		t.Fatalf("DeleteCeremony: %v", err)
	}
	if _, err := env.webauthnCeremony.GetCeremony(ctx, c.ID); !errors.Is(err, ErrWebAuthnCeremonyNotFound) {
		t.Errorf("err = %v, esperado ErrWebAuthnCeremonyNotFound tras borrar", err)
	}

	// Login discoverable: UserID vacío (todavía no se sabe quién es).
	anon := &WebAuthnCeremony{
		ID: idgen.New(), UserID: "", Purpose: WebAuthnCeremonyPurposeLogin,
		SessionData: `{"challenge":"xyz"}`, CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
	if err := env.webauthnCeremony.CreateCeremony(ctx, anon); err != nil {
		t.Fatalf("CreateCeremony (anónima): %v", err)
	}
	got, err = env.webauthnCeremony.GetCeremony(ctx, anon.ID)
	if err != nil {
		t.Fatalf("GetCeremony (anónima): %v", err)
	}
	if got.UserID != "" {
		t.Errorf("UserID = %q, esperado vacío en login discoverable", got.UserID)
	}

	// DeleteExpiredCeremonies: crear una ya caducada y comprobar que se purga.
	expired := &WebAuthnCeremony{
		ID: idgen.New(), UserID: u.ID, Purpose: WebAuthnCeremonyPurposeLogin,
		SessionData: `{}`, CreatedAt: now.Add(-10 * time.Minute), ExpiresAt: now.Add(-5 * time.Minute),
	}
	if err := env.webauthnCeremony.CreateCeremony(ctx, expired); err != nil {
		t.Fatalf("CreateCeremony (caducada): %v", err)
	}
	n, err := env.webauthnCeremony.DeleteExpiredCeremonies(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpiredCeremonies: %v", err)
	}
	if n != 1 {
		t.Errorf("purgadas = %d, esperada 1 (solo la caducada)", n)
	}
	if _, err := env.webauthnCeremony.GetCeremony(ctx, anon.ID); err != nil {
		t.Errorf("la ceremonia sin caducar no debería haberse purgado: %v", err)
	}
}

func TestBeginRegistrationCreatesChallengeAndCeremony(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")
	svc := env.webAuthnService(t)

	creation, ceremonyID, err := svc.BeginRegistration(ctx, u.ID)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if ceremonyID == "" {
		t.Fatal("ceremonyID no debería estar vacío")
	}
	if creation.Response.RelyingParty.ID != "localhost" {
		t.Errorf("RelyingParty.ID = %q, esperado localhost", creation.Response.RelyingParty.ID)
	}
	if creation.Response.User.Name != "ivan" {
		t.Errorf("User.Name = %q, esperado ivan", creation.Response.User.Name)
	}

	stored, err := env.webauthnCeremony.GetCeremony(ctx, ceremonyID)
	if err != nil {
		t.Fatalf("la ceremonia debería haberse persistido: %v", err)
	}
	if stored.UserID != u.ID || stored.Purpose != WebAuthnCeremonyPurposeRegistration {
		t.Errorf("ceremonia = %+v", stored)
	}
}

func TestFinishRegistrationRejectsMismatchedUser(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")
	otro := env.createUser(t, ctx, "otro", "contraseña-otro")
	svc := env.webAuthnService(t)

	_, ceremonyID, err := svc.BeginRegistration(ctx, u.ID)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	// El chequeo de propiedad de la ceremonia ocurre antes de interpretar el
	// cuerpo, así que un io.Reader vacío basta para llegar a él.
	_, err = svc.FinishRegistration(ctx, otro.ID, ceremonyID, "portátil", strings.NewReader(""))
	if !errors.Is(err, ErrWebAuthnCeremonyMismatch) {
		t.Errorf("err = %v, esperado ErrWebAuthnCeremonyMismatch", err)
	}
}

func TestFinishRegistrationRejectsUnknownCeremony(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")
	svc := env.webAuthnService(t)

	_, err := svc.FinishRegistration(ctx, u.ID, idgen.New(), "portátil", strings.NewReader(""))
	if !errors.Is(err, ErrWebAuthnCeremonyNotFound) {
		t.Errorf("err = %v, esperado ErrWebAuthnCeremonyNotFound", err)
	}
}

// Una ceremonia se consume en el primer intento, correcto o no: evita que
// alguien reintente contra el mismo ceremonyID para sondear respuestas
// distintas (§170, mismo criterio que un token de invitación consumido).
func TestCeremonyIsConsumedEvenOnMismatch(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "ivan", "contraseña-correcta")
	otro := env.createUser(t, ctx, "otro", "contraseña-otro")
	svc := env.webAuthnService(t)

	_, ceremonyID, err := svc.BeginRegistration(ctx, u.ID)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if _, err := svc.FinishRegistration(ctx, otro.ID, ceremonyID, "x", strings.NewReader("")); err == nil {
		t.Fatal("se esperaba un error en el primer intento (usuario equivocado)")
	}
	if _, err := svc.FinishRegistration(ctx, u.ID, ceremonyID, "x", strings.NewReader("")); !errors.Is(err, ErrWebAuthnCeremonyNotFound) {
		t.Errorf("el segundo intento debería fallar con ErrWebAuthnCeremonyNotFound (ya consumida): %v", err)
	}
}

func TestBeginDiscoverableLoginCreatesAnonymousCeremony(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	svc := env.webAuthnService(t)

	assertion, ceremonyID, err := svc.BeginDiscoverableLogin(ctx)
	if err != nil {
		t.Fatalf("BeginDiscoverableLogin: %v", err)
	}
	if assertion.Response.RelyingPartyID != "localhost" {
		t.Errorf("RelyingPartyID = %q, esperado localhost", assertion.Response.RelyingPartyID)
	}
	stored, err := env.webauthnCeremony.GetCeremony(ctx, ceremonyID)
	if err != nil {
		t.Fatalf("la ceremonia debería haberse persistido: %v", err)
	}
	if stored.UserID != "" {
		t.Errorf("UserID = %q, esperado vacío (todavía no se sabe quién es)", stored.UserID)
	}
}

func TestFinishDiscoverableLoginRejectsUnknownCeremony(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	svc := env.webAuthnService(t)

	_, _, err := svc.FinishDiscoverableLogin(ctx, idgen.New(), strings.NewReader(""))
	if !errors.Is(err, ErrWebAuthnCeremonyNotFound) {
		t.Errorf("err = %v, esperado ErrWebAuthnCeremonyNotFound", err)
	}
}

func TestRevokeCredentialRefusesOtherUsersCredential(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	u := env.createUser(t, ctx, "victima", "contraseña-1")
	atacante := env.createUser(t, ctx, "atacante", "contraseña-2")
	svc := env.webAuthnService(t)

	cred := &WebAuthnCredential{
		ID: idgen.New(), UserID: u.ID, CredentialID: "cred-victima",
		PublicKey: "pk", Label: "llave", CreatedAt: time.Now().UTC(),
	}
	if err := env.webauthnCreds.CreateCredential(ctx, cred); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	if err := svc.RevokeCredential(ctx, cred.ID, atacante.ID); !errors.Is(err, ErrWebAuthnCredentialNotFound) {
		t.Errorf("RevokeCredential con userID ajeno: err = %v, esperado ErrWebAuthnCredentialNotFound", err)
	}
	list, err := svc.ListCredentials(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("la credencial de la víctima no debería haberse borrado: %+v", list)
	}
}
