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

func TestDefaultsTrashMaxTotalSizeBytesIsUnlimited(t *testing.T) {
	if got := Defaults().Trash.MaxTotalSizeBytes; got != 0 {
		t.Errorf("Trash.MaxTotalSizeBytes = %d, esperado 0 (sin límite) por defecto", got)
	}
}

func TestValidateRejectsNegativeTrashMaxTotalSizeBytes(t *testing.T) {
	cfg := Defaults()
	cfg.Trash.MaxTotalSizeBytes = -1
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar trash.maxTotalSizeBytes negativo")
	}
}

func TestDefaultsVersioningAgeAndSizeLimitsAreUnlimited(t *testing.T) {
	d := Defaults()
	if d.Versioning.MaxVersionAgeDays != 0 {
		t.Errorf("Versioning.MaxVersionAgeDays = %d, esperado 0 (sin límite) por defecto", d.Versioning.MaxVersionAgeDays)
	}
	if d.Versioning.MaxVersionsTotalSizeBytes != 0 {
		t.Errorf("Versioning.MaxVersionsTotalSizeBytes = %d, esperado 0 (sin límite) por defecto", d.Versioning.MaxVersionsTotalSizeBytes)
	}
}

func TestValidateRejectsNegativeVersioningMaxVersionAgeDays(t *testing.T) {
	cfg := Defaults()
	cfg.Versioning.MaxVersionAgeDays = -1
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar versioning.maxVersionAgeDays negativo")
	}
}

func TestValidateRejectsNegativeVersioningMaxVersionsTotalSizeBytes(t *testing.T) {
	cfg := Defaults()
	cfg.Versioning.MaxVersionsTotalSizeBytes = -1
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar versioning.maxVersionsTotalSizeBytes negativo")
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

func TestDefaultsKeepWebDAVAndWebAuthnOff(t *testing.T) {
	cfg := Defaults()
	if cfg.WebDAV.Enabled {
		t.Error("webdav.enabled debe ser false por defecto (§3, §169): añade un protocolo más con su propia autenticación")
	}
	if cfg.Security.WebAuthn.Enabled {
		t.Error("security.webAuthn.enabled debe ser false por defecto: sin RPID/RPOrigin reales el navegador rechaza cualquier passkey")
	}
	if cfg.WebDAV.Path != "/webdav" || cfg.WebDAV.ReadOnly || cfg.WebDAV.MaxUploadSizeBytes != 0 {
		t.Errorf("valores por defecto de webdav inesperados: %+v", cfg.WebDAV)
	}
	if cfg.Security.RateLimit.WebDAVPerMinute < 1 {
		t.Errorf("security.rateLimit.webdavPerMinute = %d, debe tener un valor por defecto útil", cfg.Security.RateLimit.WebDAVPerMinute)
	}
}

func TestValidateWebDAVPath(t *testing.T) {
	valid := []string{"/webdav", "/dav", "/files/dav", "/nc_dav-1.0"}
	invalid := []string{
		"", "webdav", "/", "/webdav/", "//x", "/a//b", "/a/../b", "/./a", "/con espacio", "/ñ",
		"/api", "/api/v1/dav", "/health", "/ready", "/ready/x",
	}
	for _, p := range valid {
		cfg := Defaults()
		cfg.WebDAV.Enabled, cfg.WebDAV.Path = true, p
		if err := Validate(cfg); err != nil {
			t.Errorf("webdav.path %q debería ser válido: %v", p, err)
		}
	}
	for _, p := range invalid {
		cfg := Defaults()
		cfg.WebDAV.Enabled, cfg.WebDAV.Path = true, p
		if err := Validate(cfg); err == nil {
			t.Errorf("webdav.path %q debería rechazarse", p)
		}
	}

	// Con WebDAV desactivado la ruta no se usa: no debe impedir arrancar.
	cfg := Defaults()
	cfg.WebDAV.Enabled, cfg.WebDAV.Path = false, "no-es-una-ruta"
	if err := Validate(cfg); err != nil {
		t.Errorf("con webdav.enabled=false no se valida la ruta: %v", err)
	}
}

func TestValidateRejectsNegativeWebDAVMaxUploadSize(t *testing.T) {
	cfg := Defaults()
	cfg.WebDAV.MaxUploadSizeBytes = -1
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar webdav.maxUploadSizeBytes negativo (0 = sin límite)")
	}
}

func TestValidateRejectsZeroWebDAVRateLimit(t *testing.T) {
	cfg := Defaults()
	cfg.Security.RateLimit.WebDAVPerMinute = 0
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar security.rateLimit.webdavPerMinute < 1")
	}
}

func TestWebDAVEnvOverrides(t *testing.T) {
	t.Setenv("NEXUSCLOUD_WEBDAV_ENABLED", "true")
	t.Setenv("NEXUSCLOUD_WEBDAV_PATH", "/dav")
	t.Setenv("NEXUSCLOUD_WEBDAV_READ_ONLY", "true")
	t.Setenv("NEXUSCLOUD_WEBDAV_MAX_UPLOAD_SIZE_BYTES", "1048576")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load falló: %v", err)
	}
	want := WebDAVConfig{Enabled: true, Path: "/dav", ReadOnly: true, MaxUploadSizeBytes: 1048576}
	if cfg.WebDAV != want {
		t.Errorf("webdav = %+v, esperado %+v", cfg.WebDAV, want)
	}
}

func TestValidateWebAuthnRequiresRealRelyingPartySettings(t *testing.T) {
	cases := []struct {
		name         string
		rpID, origin string
		wantValid    bool
	}{
		{"https con dominio", "nexuscloud.example.com", "https://nexuscloud.example.com", true},
		{"localhost en desarrollo", "localhost", "http://localhost:5173", true},
		{"sin rpID", "", "https://nexuscloud.example.com", false},
		{"sin rpOrigin", "nexuscloud.example.com", "", false},
		{"http en un dominio real", "nexuscloud.example.com", "http://nexuscloud.example.com", false},
	}
	for _, tc := range cases {
		cfg := Defaults()
		cfg.Security.WebAuthn.Enabled = true
		cfg.Security.WebAuthn.RPID, cfg.Security.WebAuthn.RPOrigin = tc.rpID, tc.origin
		err := Validate(cfg)
		if tc.wantValid && err != nil {
			t.Errorf("%s: debería ser válido: %v", tc.name, err)
		}
		if !tc.wantValid && err == nil {
			t.Errorf("%s: debería rechazarse", tc.name)
		}
	}

	// Desactivado, nada de esto se exige (es el estado por defecto).
	if err := Validate(Defaults()); err != nil {
		t.Errorf("la configuración por defecto (WebAuthn desactivado, sin RPID) debe ser válida: %v", err)
	}
}

// config.example.yaml documenta `dataDir: ""` como "valor por SO", pero el YAML
// pisaba el valor por defecto con la cadena vacía y Validate lo rechazaba: quien
// copiaba el ejemplo tal cual (como pide su cabecera) recibía un error.
func TestLoadTreatsAnEmptyDataDirAsTheOSDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("storage:\n  dataDir: \"\"\n"), 0o600); err != nil {
		t.Fatalf("escribiendo config de prueba: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("un dataDir vacío debe significar el valor por SO, no un error: %v", err)
	}
	if want := defaultDataDir(); cfg.Storage.DataDir != want {
		t.Errorf("dataDir = %q, esperado el de por defecto %q", cfg.Storage.DataDir, want)
	}
}

// El ejemplo es lo primero que copia un administrador: tiene que cargar y
// validar tal cual está. Así no puede volver a divergir del validador.
func TestTheExampleConfigLoadsAndValidates(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("config.example.yaml debe cargar y validar tal cual: %v", err)
	}
	if cfg.WebDAV.Enabled || cfg.Security.WebAuthn.Enabled {
		t.Error("el ejemplo debe mantener WebDAV y WebAuthn desactivados (secure by default)")
	}
}

// storage.defaultQuotaBytes (§24, ADR-036): la cuota global, la que se aplica a
// quien no tiene cuota propia ni de grupo. 0 = sin cuota (el comportamiento de
// siempre): las instalaciones existentes no cambian.
func TestDefaultQuotaIsUnlimitedByDefault(t *testing.T) {
	if got := Defaults().Storage.DefaultQuotaBytes; got != 0 {
		t.Errorf("storage.defaultQuotaBytes por defecto = %d, esperado 0 (sin límite)", got)
	}
}

func TestValidateRejectsNegativeDefaultQuota(t *testing.T) {
	cfg := Defaults()
	cfg.Storage.DefaultQuotaBytes = -1
	if err := Validate(cfg); err == nil {
		t.Error("Validate debe rechazar storage.defaultQuotaBytes negativo (0 = sin límite)")
	}
}

func TestDefaultQuotaEnvOverride(t *testing.T) {
	t.Setenv("NEXUSCLOUD_STORAGE_DEFAULT_QUOTA_BYTES", "107374182400") // 100 GiB: no cabe en 32 bits

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load falló: %v", err)
	}
	if cfg.Storage.DefaultQuotaBytes != 107374182400 {
		t.Errorf("storage.defaultQuotaBytes = %d, esperado 107374182400", cfg.Storage.DefaultQuotaBytes)
	}
}

func TestDefaultQuotaLoadsFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("storage:\n  defaultQuotaBytes: 53687091200\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load falló: %v", err)
	}
	if cfg.Storage.DefaultQuotaBytes != 53687091200 {
		t.Errorf("storage.defaultQuotaBytes = %d, esperado 53687091200", cfg.Storage.DefaultQuotaBytes)
	}
}
