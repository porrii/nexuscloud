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

func TestMigrateAppliesInitialSchema(t *testing.T) {
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
	if version != 1 {
		t.Errorf("version = %d, esperado 1", version)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM roles").Scan(&count); err != nil {
		t.Fatalf("consultando roles: %v", err)
	}
	if count != 4 {
		t.Errorf("roles sembrados = %d, esperado 4 (super_admin/administrator/user/read_only)", count)
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
