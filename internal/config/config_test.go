package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsAreValid(t *testing.T) {
	if err := Validate(Defaults()); err != nil {
		t.Fatalf("la configuración por defecto debe ser válida: %v", err)
	}
}

func TestDefaultsAreSecureByDefault(t *testing.T) {
	cfg := Defaults()
	if cfg.Web.Enabled {
		t.Error("web.enabled debe ser false por defecto (§3, §47)")
	}
	if cfg.Security.PublicRegistrationEnabled {
		t.Error("security.publicRegistrationEnabled debe ser false por defecto (§21, §169)")
	}
	if len(cfg.Security.CORSAllowedOrigins) != 0 {
		t.Error("security.corsAllowedOrigins debe estar vacío por defecto")
	}
}

func TestLoadMissingFileFallsBackToDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "no-existe.yaml"))
	if err != nil {
		t.Fatalf("Load con fichero inexistente no debe fallar: %v", err)
	}
	if cfg.Server.Port != Defaults().Server.Port {
		t.Errorf("puerto por defecto inesperado: %d", cfg.Server.Port)
	}
}

func TestLoadParsesYAMLAndOverridesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yamlContent := "configVersion: 1\nserver:\n  port: 9999\ndatabase:\n  driver: postgres\n  dsn: postgres://x\n"
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load falló: %v", err)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("server.port = %d, esperado 9999", cfg.Server.Port)
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("database.driver = %q, esperado postgres", cfg.Database.Driver)
	}
}

func TestEnvOverridesTakePriorityOverYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 1111\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEXUSCLOUD_PORT", "2222")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load falló: %v", err)
	}
	if cfg.Server.Port != 2222 {
		t.Errorf("server.port = %d, esperado 2222 (env debe ganar a yaml)", cfg.Server.Port)
	}
}

func TestValidateRejectsWildcardCORS(t *testing.T) {
	cfg := Defaults()
	cfg.Security.CORSAllowedOrigins = []string{"*"}
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar CORS '*' (§69)")
	}
}

func TestValidateRejectsUnknownDatabaseDriver(t *testing.T) {
	cfg := Defaults()
	cfg.Database.Driver = "oracle"
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar un driver de base de datos no soportado")
	}
}

func TestValidateRejectsPostgresWithoutDSN(t *testing.T) {
	cfg := Defaults()
	cfg.Database.Driver = "postgres"
	cfg.Database.DSN = ""
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe exigir database.dsn cuando driver=postgres")
	}
}

func TestValidateRejectsUnsupportedConfigVersion(t *testing.T) {
	cfg := Defaults()
	cfg.ConfigVersion = CurrentConfigVersion + 1
	if err := migrateSchema(cfg); err == nil {
		t.Error("migrateSchema debe rechazar una configVersion futura no soportada")
	}
}

func TestSQLiteDSNDefaultsUnderDataDir(t *testing.T) {
	cfg := Defaults()
	cfg.Storage.DataDir = filepath.Join("tmp", "nexuscloud-data")
	got := cfg.SQLiteDSN()
	want := filepath.Join(cfg.Storage.DataDir, "database", "nexuscloud.db")
	if got != want {
		t.Errorf("SQLiteDSN() = %q, esperado %q", got, want)
	}
}

func TestSQLiteDSNRespectsExplicitOverride(t *testing.T) {
	cfg := Defaults()
	cfg.Database.DSN = "/custom/path.db"
	if got := cfg.SQLiteDSN(); got != "/custom/path.db" {
		t.Errorf("SQLiteDSN() = %q, esperado /custom/path.db", got)
	}
}
