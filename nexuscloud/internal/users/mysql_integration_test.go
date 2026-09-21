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

// TestQuotaRoundTripOnRealEngines (ADR-036) confirma contra MySQL/PostgreSQL
// reales lo que sqlite no puede ver: las cuotas de más de 2 GiB caben (la
// migración 0011 ensanchó quota_bytes a BIGINT), UpdateGroupQuota escribe en
// la tabla `groups` con el nombre propio de cada dialecto, y repetir el mismo
// valor no se confunde con «grupo inexistente» (MySQL informa 0 filas
// cambiadas, no 0 filas encontradas).
func TestQuotaRoundTripOnRealEngines(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			conn := dbtest.OpenReal(t, driver)
			repo := NewSQLRepository(conn)
			svc := NewService(repo, WithDefaultQuota(50<<30))
			suffix := idgen.New()[:8]

			hundredGiB := int64(100) << 30
			u, err := svc.CreateUser(ctx, CreateUserInput{Username: "q-" + suffix, PasswordHash: "h", QuotaBytes: &hundredGiB})
			if err != nil {
				t.Fatalf("CreateUser con 100 GiB: %v", err)
			}
			stored, err := repo.GetUserByID(ctx, u.ID)
			if err != nil || stored.QuotaBytes == nil || *stored.QuotaBytes != hundredGiB {
				t.Fatalf("cuota guardada = %v (%v), esperado 100 GiB", stored.QuotaBytes, err)
			}

			grp := &Group{ID: idgen.New(), Name: "g-" + suffix, CreatedAt: time.Now().UTC()}
			if err := repo.CreateGroup(ctx, grp); err != nil {
				t.Fatalf("CreateGroup: %v", err)
			}
			if err := repo.AddUserToGroup(ctx, u.ID, grp.ID); err != nil {
				t.Fatalf("AddUserToGroup: %v", err)
			}
			twoHundredGiB := int64(200) << 30
			for i := 0; i < 2; i++ { // la segunda vez, mismo valor
				if err := svc.SetGroupQuota(ctx, grp.ID, &twoHundredGiB); err != nil {
					t.Fatalf("SetGroupQuota (intento %d): %v", i+1, err)
				}
			}
			if err := svc.SetGroupQuota(ctx, "no-existe-"+suffix, &twoHundredGiB); !errors.Is(err, ErrNotFound) {
				t.Errorf("grupo inexistente = %v, esperado ErrNotFound", err)
			}

			// Con cuota propia gana la propia; al heredar, la del grupo.
			if got, _ := svc.EffectiveQuota(ctx, u.ID); got.LimitBytes != hundredGiB || got.Source != QuotaSourceUser {
				t.Errorf("con cuota propia = %+v, esperado 100 GiB del usuario", got)
			}
			if err := svc.SetUserQuota(ctx, u.ID, nil); err != nil {
				t.Fatalf("SetUserQuota(nil): %v", err)
			}
			if got, _ := svc.EffectiveQuota(ctx, u.ID); got.LimitBytes != twoHundredGiB || got.Source != QuotaSourceGroup || got.GroupName != grp.Name {
				t.Errorf("al heredar = %+v, esperado 200 GiB del grupo %s", got, grp.Name)
			}
		})
	}
}
