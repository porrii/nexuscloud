package auth

import (
	"context"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/dbtest"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

// newRealTestEnv monta el mismo testEnv que newTestEnv (ver
// authenticator_test.go) pero contra una base de datos REAL (mysql o
// postgres, vía dbtest.OpenReal) en vez de sqlite -- ADR-031. Salta el
// test si la variable NEXUSCLOUD_TEST_<DRIVER>_DSN correspondiente no
// está puesta. Existe sobre todo para probar de verdad que Rebind (los
// placeholders "?" reescritos a "$1, $2..." para postgres) funciona con
// las consultas nuevas de WebAuthn: newTestEnv, igual que el resto de
// internal/auth, solo usa sqlite -- nunca ejercita eso.
func newRealTestEnv(t *testing.T, driver string) *testEnv {
	t.Helper()
	conn := dbtest.OpenReal(t, driver)
	return &testEnv{
		users:            users.NewSQLRepository(conn),
		sessions:         NewSQLSessionRepository(conn),
		invitations:      NewSQLInvitationRepository(conn),
		webauthnCreds:    NewSQLWebAuthnCredentialRepository(conn),
		webauthnCeremony: NewSQLWebAuthnCeremonyRepository(conn),
		hasher:           NewHasher(config.Argon2Config{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}),
	}
}

func TestRealDBWebAuthnCredentialAndCeremonyRoundTrip(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			u := env.createUser(t, ctx, "u-"+idgen.New()[:8], "contraseña-correcta")

			cred := &WebAuthnCredential{
				ID: idgen.New(), UserID: u.ID, CredentialID: "cred-" + idgen.New(),
				PublicKey: "pubkey-base64", SignCount: 0, Label: "llave de prueba",
				CreatedAt: time.Now().UTC(),
			}
			if err := env.webauthnCreds.CreateCredential(ctx, cred); err != nil {
				t.Fatalf("CreateCredential: %v", err)
			}
			got, err := env.webauthnCreds.GetCredentialByCredentialID(ctx, cred.CredentialID)
			if err != nil {
				t.Fatalf("GetCredentialByCredentialID: %v", err)
			}
			if got.ID != cred.ID || got.Label != cred.Label {
				t.Errorf("got = %+v", got)
			}

			now := time.Now().UTC()
			if err := env.webauthnCreds.UpdateSignCount(ctx, cred.ID, 3, now); err != nil {
				t.Fatalf("UpdateSignCount: %v", err)
			}
			got, err = env.webauthnCreds.GetCredentialByCredentialID(ctx, cred.CredentialID)
			if err != nil {
				t.Fatalf("GetCredentialByCredentialID tras update: %v", err)
			}
			if got.SignCount != 3 || got.LastUsedAt == nil {
				t.Errorf("got = %+v, esperado sign_count=3 y last_used_at puesto", got)
			}

			// Ceremonia anónima (login discoverable, user_id NULL): el caso que
			// más depende de que Rebind trate bien un parámetro NULL.
			c := &WebAuthnCeremony{
				ID: idgen.New(), UserID: "", Purpose: WebAuthnCeremonyPurposeLogin,
				SessionData: `{"challenge":"abc"}`, CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
			}
			if err := env.webauthnCeremony.CreateCeremony(ctx, c); err != nil {
				t.Fatalf("CreateCeremony: %v", err)
			}
			gotCeremony, err := env.webauthnCeremony.GetCeremony(ctx, c.ID)
			if err != nil {
				t.Fatalf("GetCeremony: %v", err)
			}
			if gotCeremony.UserID != "" || gotCeremony.SessionData != c.SessionData {
				t.Errorf("gotCeremony = %+v", gotCeremony)
			}

			if err := env.webauthnCreds.DeleteCredential(ctx, cred.ID, u.ID); err != nil {
				t.Fatalf("DeleteCredential: %v", err)
			}
			if err := env.webauthnCeremony.DeleteCeremony(ctx, c.ID); err != nil {
				t.Fatalf("DeleteCeremony: %v", err)
			}
		})
	}
}
