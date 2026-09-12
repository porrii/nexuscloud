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
	if cfg.Sharing.PublicLinksEnabled {
		t.Error("sharing.publicLinksEnabled debe ser false por defecto (§3, §47): es la única superficie sin sesión que añade Sharing")
	}
	if !cfg.Sharing.Enabled {
		t.Error("sharing.enabled (compartición interna usuario/grupo, siempre autenticada) debe ser true por defecto, igual que trash/versioning")
	}
	if cfg.Backup.Enabled {
		t.Error("backup.enabled debe ser false por defecto: copia datos reales, por defecto al mismo disco (§19), el admin debe activarlo a propósito (ADR-016)")
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

func TestValidateRejectsZeroIntervalWhenBackupEnabled(t *testing.T) {
	cfg := Defaults()
	cfg.Backup.Enabled = true
	cfg.Backup.IntervalMinutes = 0
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe exigir backup.intervalMinutes >= 1 cuando backup.enabled=true")
	}
}

func TestValidateAllowsZeroIntervalWhenBackupDisabled(t *testing.T) {
	cfg := Defaults()
	cfg.Backup.Enabled = false
	cfg.Backup.IntervalMinutes = 0
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate no debe exigir intervalMinutes cuando backup.enabled=false: %v", err)
	}
}

func TestDefaultsBackupRetentionCountIsUnlimited(t *testing.T) {
	if got := Defaults().Backup.RetentionCount; got != 0 {
		t.Errorf("Backup.RetentionCount = %d, esperado 0 (sin límite) por defecto", got)
	}
}

func TestValidateRejectsNegativeBackupRetentionCount(t *testing.T) {
	cfg := Defaults()
	cfg.Backup.RetentionCount = -1
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar backup.retentionCount negativo")
	}
}

func TestValidateRejectsNegativeBackupRetentionDays(t *testing.T) {
	cfg := Defaults()
	cfg.Backup.RetentionDays = -1
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar backup.retentionDays negativo")
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

func TestStorageAreasDefaultUnderDataDir(t *testing.T) {
	cfg := Defaults()
	cfg.Storage.DataDir = filepath.Join("tmp", "nexuscloud-data")
	cases := map[string]struct {
		got  string
		name string
	}{
		"database":   {cfg.DatabaseDir(), "database"},
		"cache":      {cfg.CacheDir(), "cache"},
		"thumbnails": {cfg.ThumbnailsDir(), "thumbnails"},
		"versions":   {cfg.VersionsDir(), "versions"},
		"temp":       {cfg.TempDir(), "tmp"},
		"logs":       {cfg.LogsDir(), "logs"},
		"backups":    {cfg.BackupsDir(), "backups"},
		"config":     {cfg.ConfigDir(), "config"},
	}
	for area, c := range cases {
		want := filepath.Join(cfg.Storage.DataDir, c.name)
		if c.got != want {
			t.Errorf("área %q sin override = %q, esperado %q", area, c.got, want)
		}
	}
}

func TestStorageAreaOverridesAreRespected(t *testing.T) {
	cfg := Defaults()
	cfg.Storage.DataDir = filepath.Join("tmp", "nexuscloud-data")
	cfg.Storage.CacheDir = filepath.Join("mnt", "ssd", "nx-cache")
	cfg.Storage.LogsDir = filepath.Join("var", "log", "nexuscloud")

	if got := cfg.CacheDir(); got != cfg.Storage.CacheDir {
		t.Errorf("CacheDir() = %q, esperado el override %q", got, cfg.Storage.CacheDir)
	}
	if got := cfg.LogsDir(); got != cfg.Storage.LogsDir {
		t.Errorf("LogsDir() = %q, esperado el override %q", got, cfg.Storage.LogsDir)
	}
	// Un área sin override sigue colgando de DataDir.
	if got, want := cfg.BackupsDir(), filepath.Join(cfg.Storage.DataDir, "backups"); got != want {
		t.Errorf("BackupsDir() sin override = %q, esperado %q", got, want)
	}
}

func TestStorageAreaEnvOverride(t *testing.T) {
	custom := filepath.Join("mnt", "disk2", "nx-cache")
	t.Setenv("NEXUSCLOUD_CACHE_DIR", custom)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load falló: %v", err)
	}
	if cfg.Storage.CacheDir != custom {
		t.Errorf("storage.cacheDir vía env = %q, esperado %q", cfg.Storage.CacheDir, custom)
	}
	if got := cfg.CacheDir(); got != custom {
		t.Errorf("CacheDir() con env = %q, esperado %q", got, custom)
	}
}

func TestSQLiteDSNRespectsDatabaseDirOverride(t *testing.T) {
	cfg := Defaults()
	cfg.Storage.DataDir = filepath.Join("tmp", "nexuscloud-data")
	cfg.Storage.DatabaseDir = filepath.Join("mnt", "nvme", "nx-db")

	want := filepath.Join(cfg.Storage.DatabaseDir, "nexuscloud.db")
	if got := cfg.SQLiteDSN(); got != want {
		t.Errorf("SQLiteDSN() con databaseDir override = %q, esperado %q", got, want)
	}
}

func TestValidateRejectsAreaInsideStorageDir(t *testing.T) {
	cfg := Defaults()
	cfg.Storage.DataDir = filepath.FromSlash("/srv/nexuscloud")
	// cacheDir apuntando dentro de la raíz de datos de usuario: footgun.
	cfg.Storage.CacheDir = filepath.Join(cfg.DefaultStorageDir(), "cache")

	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar un área de infraestructura dentro de storageDir")
	}
}

func TestValidateAllowsAreasOutsideStorageDir(t *testing.T) {
	cfg := Defaults()
	cfg.Storage.DataDir = filepath.FromSlash("/srv/nexuscloud")
	cfg.Storage.CacheDir = filepath.FromSlash("/mnt/ssd/nx-cache")
	cfg.Storage.DatabaseDir = filepath.FromSlash("/mnt/nvme/nx-db")
	cfg.Storage.LogsDir = filepath.FromSlash("/var/log/nexuscloud")

	if err := Validate(cfg); err != nil {
		t.Errorf("Validate debe aceptar áreas reubicadas fuera de storageDir: %v", err)
	}
}
