package webdav

import (
	"context"
	"errors"
	"testing"

	"github.com/porrii/nexuscloud/internal/dbtest"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

// TestRealDBTokenLifecycle ejercita las consultas de tokens contra una base
// de datos REAL (mysql o postgres, vía dbtest.OpenReal, ADR-031): los tests
// de token_test.go solo usan sqlite y nunca prueban de verdad que Rebind
// (placeholders "?" -> "$1, $2..." de postgres) funcione con ellas. Salta el
// test si NEXUSCLOUD_TEST_<DRIVER>_DSN no está puesta.
func TestRealDBTokenLifecycle(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			conn := dbtest.OpenReal(t, driver)
			userRepo := users.NewSQLRepository(conn)
			svc := NewTokenService(NewSQLTokenRepository(conn), userRepo, nil)

			username := "u-" + idgen.New()[:8]
			u, err := users.NewService(userRepo).CreateUser(ctx, users.CreateUserInput{
				Username: username, PasswordHash: "hash-de-prueba",
			})
			if err != nil {
				t.Fatalf("CreateUser falló: %v", err)
			}

			tok, plain, err := svc.Create(ctx, u.ID, "llave de prueba")
			if err != nil {
				t.Fatalf("Create falló: %v", err)
			}
			got, err := svc.Authenticate(ctx, username, plain)
			if err != nil {
				t.Fatalf("Authenticate falló: %v", err)
			}
			if got.ID != u.ID {
				t.Errorf("usuario = %s, esperado %s", got.ID, u.ID)
			}

			list, err := svc.List(ctx, u.ID)
			if err != nil {
				t.Fatalf("List falló: %v", err)
			}
			if len(list) != 1 || list[0].LastUsedAt == nil {
				t.Errorf("list = %+v, esperado 1 token con last_used_at puesto por Authenticate", list)
			}

			if err := svc.Revoke(ctx, tok.ID, "otro-usuario"); !errors.Is(err, ErrTokenNotFound) {
				t.Errorf("Revoke con userID ajeno: err = %v, esperado ErrTokenNotFound", err)
			}
			if err := svc.Revoke(ctx, tok.ID, u.ID); err != nil {
				t.Fatalf("Revoke falló: %v", err)
			}
			if _, err := svc.Authenticate(ctx, username, plain); !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("tras revocar: err = %v, esperado ErrInvalidCredentials", err)
			}
		})
	}
}
