package db

import (
	"path/filepath"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "nexuscloud-test.db")
	return cfg
}

// currentSchemaVersion es la versión de esquema más alta esperada tras
// aplicar todas las migraciones embebidas. Actualízala al añadir una nueva
// migración (§8: cada una suma, nunca se reescribe una ya aplicada).
const currentSchemaVersion = 4

func TestMigrateAppliesFullSchema(t *testing.T) {
	cfg := testConfig(t)
	conn, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open falló: %v", err)
	}
	defer conn.Close()

	if err := Migrate(cfg, conn); err != nil {
		t.Fatalf("Migrate falló: %v", err)
	}

	version, dirty, err := Status(cfg, conn)
	if err != nil {
		t.Fatalf("Status falló: %v", err)
	}
	if dirty {
		t.Error("el esquema no debería quedar dirty tras una migración limpia")
	}
	if version != currentSchemaVersion {
		t.Errorf("version = %d, esperado %d", version, currentSchemaVersion)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM roles").Scan(&count); err != nil {
		t.Fatalf("consultando roles: %v", err)
	}
	if count != 4 {
		t.Errorf("roles sembrados = %d, esperado 4 (super_admin/administrator/user/read_only)", count)
	}

	// directories (0002) debe existir y estar vacía tras un esquema recién
	// migrado.
	if err := conn.QueryRow("SELECT COUNT(*) FROM directories").Scan(&count); err != nil {
		t.Fatalf("consultando directories: %v", err)
	}
	if count != 0 {
		t.Errorf("directories = %d filas, esperado 0 en un esquema recién migrado", count)
	}

	// deleted_at (0003) debe existir en files y directories.
	if err := conn.QueryRow("SELECT COUNT(*) FROM files WHERE deleted_at IS NOT NULL").Scan(&count); err != nil {
		t.Fatalf("la columna files.deleted_at debería existir tras la migración 0003: %v", err)
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM directories WHERE deleted_at IS NOT NULL").Scan(&count); err != nil {
		t.Fatalf("la columna directories.deleted_at debería existir tras la migración 0003: %v", err)
	}

	// file_versions (0004) debe existir y estar vacía tras un esquema
	// recién migrado.
	if err := conn.QueryRow("SELECT COUNT(*) FROM file_versions").Scan(&count); err != nil {
		t.Fatalf("consultando file_versions: %v", err)
	}
	if count != 0 {
		t.Errorf("file_versions = %d filas, esperado 0 en un esquema recién migrado", count)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	cfg := testConfig(t)
	conn, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open falló: %v", err)
	}
	defer conn.Close()

	if err := Migrate(cfg, conn); err != nil {
		t.Fatalf("primera Migrate falló: %v", err)
	}
	if err := Migrate(cfg, conn); err != nil {
		t.Fatalf("segunda Migrate (sin cambios pendientes) no debería fallar: %v", err)
	}
}

func TestStatusOnFreshDatabaseHasNoVersion(t *testing.T) {
	cfg := testConfig(t)
	conn, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open falló: %v", err)
	}
	defer conn.Close()

	version, dirty, err := Status(cfg, conn)
	if err != nil {
		t.Fatalf("Status en base de datos nueva no debería fallar: %v", err)
	}
	if version != 0 || dirty {
		t.Errorf("version=%d dirty=%v, esperado 0/false antes de migrar", version, dirty)
	}
}
