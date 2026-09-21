package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Validate comprueba invariantes de seguridad y coherencia antes de que la
// configuración se use para arrancar el servidor. Nunca debe aplicarse una
// configuración inválida (§152).
func Validate(cfg *Config) error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port fuera de rango: %d", cfg.Server.Port)
	}

	switch cfg.Database.Driver {
	case "sqlite", "postgres", "mysql":
	default:
		return fmt.Errorf("database.driver no soportado: %q (usa 'sqlite', 'postgres' o 'mysql')", cfg.Database.Driver)
	}
	if cfg.Database.Driver != "sqlite" && cfg.Database.DSN == "" {
		return fmt.Errorf("database.dsn es obligatorio cuando database.driver=%s", cfg.Database.Driver)
	}

	if cfg.Storage.DataDir == "" {
		return fmt.Errorf("storage.dataDir no puede estar vacío")
	}

	if cfg.Security.Argon2.MemoryKiB < 8*1024 {
		return fmt.Errorf("security.argon2.memoryKiB demasiado bajo (mínimo 8192, recomendado >=65536)")
	}
	if cfg.Security.Argon2.Iterations < 1 {
		return fmt.Errorf("security.argon2.iterations debe ser >= 1")
	}
	if cfg.Security.Argon2.Parallelism < 1 {
		return fmt.Errorf("security.argon2.parallelism debe ser >= 1")
	}

	if cfg.Security.RateLimit.LoginPerMinute < 1 {
		return fmt.Errorf("security.rateLimit.loginPerMinute debe ser >= 1")
	}
	if cfg.Security.RateLimit.APIPerMinute < 1 {
		return fmt.Errorf("security.rateLimit.apiPerMinute debe ser >= 1")
	}
	if cfg.Security.RateLimit.PublicLinkPerMinute < 1 {
		return fmt.Errorf("security.rateLimit.publicLinkPerMinute debe ser >= 1")
	}
	if cfg.Security.RateLimit.WebDAVPerMinute < 1 {
		return fmt.Errorf("security.rateLimit.webdavPerMinute debe ser >= 1")
	}

	for _, o := range cfg.Security.CORSAllowedOrigins {
		if o == "*" {
			return fmt.Errorf("security.corsAllowedOrigins no puede contener '*' (secure by default, §69)")
		}
	}

	if cfg.Security.WebAuthn.Enabled {
		if cfg.Security.WebAuthn.RPID == "" {
			return fmt.Errorf("security.webAuthn.rpID no puede estar vacío cuando security.webAuthn.enabled=true")
		}
		if cfg.Security.WebAuthn.RPOrigin == "" {
			return fmt.Errorf("security.webAuthn.rpOrigin no puede estar vacío cuando security.webAuthn.enabled=true")
		}
		if !strings.HasPrefix(cfg.Security.WebAuthn.RPOrigin, "https://") && !strings.HasPrefix(cfg.Security.WebAuthn.RPOrigin, "http://localhost") {
			return fmt.Errorf("security.webAuthn.rpOrigin debe usar https:// (WebAuthn lo exige salvo localhost en desarrollo)")
		}
	}

	switch cfg.Logging.Level {
	case "trace", "debug", "info", "warn", "error", "fatal":
	default:
		return fmt.Errorf("logging.level no soportado: %q", cfg.Logging.Level)
	}
	switch cfg.Logging.Format {
	case "text", "json":
	default:
		return fmt.Errorf("logging.format no soportado: %q", cfg.Logging.Format)
	}

	if (cfg.Server.TLSCertFile == "") != (cfg.Server.TLSKeyFile == "") {
		return fmt.Errorf("server.tlsCertFile y server.tlsKeyFile deben especificarse juntos")
	}

	if cfg.Trash.Enabled && cfg.Trash.RetentionDays < 1 {
		return fmt.Errorf("trash.retentionDays debe ser >= 1 cuando trash.enabled=true")
	}
	if cfg.Trash.MaxTotalSizeBytes < 0 {
		return fmt.Errorf("trash.maxTotalSizeBytes no puede ser negativo (0 = sin límite)")
	}

	if cfg.Versioning.Enabled && cfg.Versioning.MaxVersionsPerFile < 1 {
		return fmt.Errorf("versioning.maxVersionsPerFile debe ser >= 1 cuando versioning.enabled=true")
	}
	if cfg.Versioning.MaxVersionAgeDays < 0 {
		return fmt.Errorf("versioning.maxVersionAgeDays no puede ser negativo (0 = sin límite)")
	}
	if cfg.Versioning.MaxVersionsTotalSizeBytes < 0 {
		return fmt.Errorf("versioning.maxVersionsTotalSizeBytes no puede ser negativo (0 = sin límite)")
	}

	if cfg.Backup.Enabled && cfg.Backup.IntervalMinutes < 1 {
		return fmt.Errorf("backup.intervalMinutes debe ser >= 1 cuando backup.enabled=true")
	}
	if cfg.Backup.RetentionCount < 0 {
		return fmt.Errorf("backup.retentionCount no puede ser negativo (0 = sin límite)")
	}
	if cfg.Backup.RetentionDays < 0 {
		return fmt.Errorf("backup.retentionDays no puede ser negativo (0 = sin límite)")
	}
	if d := cfg.Backup.RemoteDestination; d != "" && !strings.HasPrefix(d, "http://") && !strings.HasPrefix(d, "https://") {
		return fmt.Errorf("backup.remoteDestination debe empezar por http:// o https:// (ADR-029), o dejarse vacío para un destino local")
	}

	if cfg.ClientUpdates.Enabled {
		owner, repo, ok := strings.Cut(cfg.ClientUpdates.GithubRepo, "/")
		if !ok || owner == "" || repo == "" {
			return fmt.Errorf(`clientUpdates.githubRepo debe tener el formato "propietario/repositorio" cuando clientUpdates.enabled=true`)
		}
		if cfg.ClientUpdates.Channel == "" {
			return fmt.Errorf("clientUpdates.channel no puede estar vacío cuando clientUpdates.enabled=true")
		}
	}

	if cfg.WebDAV.MaxUploadSizeBytes < 0 {
		return fmt.Errorf("webdav.maxUploadSizeBytes no puede ser negativo (0 = sin límite)")
	}
	if cfg.WebDAV.Enabled {
		if err := validateWebDAVPath(cfg.WebDAV.Path); err != nil {
			return err
		}
	}

	if err := validateStorageAreas(cfg); err != nil {
		return err
	}

	return nil
}

var webDAVPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*$`)

// validateWebDAVPath comprueba que la ruta donde se monta WebDAV es un
// prefijo de URL razonable y que no pisa las rutas propias del servidor
// (ninguna petición a /api, /health o /ready debe acabar en WebDAV).
func validateWebDAVPath(p string) error {
	if !webDAVPathPattern.MatchString(p) {
		return fmt.Errorf(`webdav.path %q no es válido: debe empezar por "/", no terminar en "/" y usar solo letras, números y "._~-" (p.ej. "/webdav")`, p)
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("webdav.path %q no puede contener segmentos '.' ni '..'", p)
		}
	}
	for _, reserved := range []string{"/api", "/health", "/ready"} {
		if p == reserved || strings.HasPrefix(p, reserved+"/") {
			return fmt.Errorf("webdav.path %q colisiona con la ruta reservada %s del servidor", p, reserved)
		}
	}
	return nil
}

// validateStorageAreas comprueba que ningún override de área de
// infraestructura (base de datos, caché, temporales, logs, etc.) caiga
// dentro de la raíz de datos de usuario: si lo hiciera, esos archivos
// internos se mezclarían con los del usuario (aparecerían en el explorador,
// entrarían en la sincronización, etc.). Casi siempre es un error de
// configuración, así que se rechaza.
func validateStorageAreas(cfg *Config) error {
	if cfg.Storage.DataDir == "" {
		return nil // ya cubierto por la comprobación anterior
	}
	userRoot := filepath.Clean(cfg.DefaultStorageDir())
	areas := map[string]string{
		"databaseDir":   cfg.DatabaseDir(),
		"cacheDir":      cfg.CacheDir(),
		"thumbnailsDir": cfg.ThumbnailsDir(),
		"versionsDir":   cfg.VersionsDir(),
		"tempDir":       cfg.TempDir(),
		"logsDir":       cfg.LogsDir(),
		"backupsDir":    cfg.BackupsDir(),
		"configDir":     cfg.ConfigDir(),
	}
	for name, dir := range areas {
		clean := filepath.Clean(dir)
		if clean == userRoot || strings.HasPrefix(clean, userRoot+string(filepath.Separator)) {
			return fmt.Errorf(
				"storage.%s (%s) está dentro de storage.storageDir (%s): mezclaría datos internos con los archivos de usuario",
				name, clean, userRoot,
			)
		}
	}
	return nil
}
