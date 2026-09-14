package db_test

import (
	"context"
	"testing"

	"github.com/porrii/nexuscloud/internal/dbtest"
)

// TestMySQLMigrationsApplyFullSchema (ADR-031) confirma que las 7
// migraciones mysql aplican limpias contra un servidor real -- no solo que
// compilan. package db_test (no db) a propósito: dbtest ya importa db, así
// que un test "de caja blanca" en package db que también importara dbtest
// crearía un ciclo; Open/Migrate/Status son exportados, así que un test
// externo puede ejercitarlos igual (ver TestMigrateAppliesFullSchema en
// db_test.go, cuyas mismas comprobaciones se repiten aquí contra mysql y
// postgres reales en vez de sqlite).
func TestMySQLMigrationsApplyFullSchema(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			conn := dbtest.OpenReal(t, driver)

			var count int
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles").Scan(&count); err != nil {
				t.Fatalf("consultando roles: %v", err)
			}
			if count != 4 {
				t.Errorf("roles sembrados = %d, esperado 4", count)
			}

			// Las FK explícitas (ADR-031, punto 2 -- inline se ignora en
			// silencio en MySQL 8) deben aplicarse de verdad: un INSERT con
			// una FK inexistente debe fallar, no aceptarse.
			_, err := conn.ExecContext(ctx,
				"INSERT INTO sessions (id, user_id, token_hash, created_at, last_seen_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
				"s-"+driver, "usuario-que-no-existe", "hash-de-prueba-"+driver, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
			if err == nil {
				t.Error("insertar una sesión con user_id inexistente debería fallar (FK), pero no falló")
			}
		})
	}
}

// TestMySQLMigrationsAreIdempotent confirma que volver a aplicar las
// migraciones (contenedor persistente, puede correr varias veces) no falla
// -- mismo contrato que TestMigrateIsIdempotent contra sqlite.
func TestMySQLMigrationsAreIdempotent(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			dbtest.OpenReal(t, driver)
			dbtest.OpenReal(t, driver) // segunda apertura+Migrate contra la MISMA base de datos
		})
	}
}
