package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/dbtest"
	"github.com/porrii/nexuscloud/internal/idgen"
)

// TestMySQLDuplicateUsernameReturnsErrAlreadyExists (ADR-031) confirma con
// una violación UNIQUE real que isUniqueViolation reconoce el mensaje real
// de MySQL/MariaDB ("Duplicate entry ... for key ...") -- sin el fix de
// ADR-031, esta violación se habría reportado como un error 500 genérico
// en vez de ErrAlreadyExists.
func TestMySQLDuplicateUsernameReturnsErrAlreadyExists(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			conn := dbtest.OpenReal(t, driver)
			repo := NewSQLRepository(conn)

			username := "erin-" + idgen.New()[:8]
			u1 := &User{
				ID: idgen.New(), Username: username, DisplayName: "Erin",
				PasswordHash: "hash-de-prueba", Status: StatusActive,
				CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			}
			if err := repo.CreateUser(ctx, u1); err != nil {
				t.Fatalf("primer CreateUser: %v", err)
			}

			u2 := &User{
				ID: idgen.New(), Username: username, DisplayName: "Erin (impostor)",
				PasswordHash: "hash-de-prueba", Status: StatusActive,
				CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			}
			if err := repo.CreateUser(ctx, u2); !errors.Is(err, ErrAlreadyExists) {
				t.Errorf("CreateUser con username duplicado = %v, esperado ErrAlreadyExists", err)
			}
		})
	}
}
