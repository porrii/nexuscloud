package config

import (
	"os"
	"strconv"
	"strings"
)

// applyEnvOverrides aplica variables de entorno NEXUSCLOUD_* sobre cfg.
// Se mantiene como mapeo explícito (en vez de reflection genérica) para que
// las claves soportadas sean auditables de un vistazo (§30, §105).
func applyEnvOverrides(cfg *Config) {
	if v, ok := lookup("NEXUSCLOUD_HOST"); ok {
		cfg.Server.Host = v
	}
	if v, ok := lookupInt("NEXUSCLOUD_PORT"); ok {
		cfg.Server.Port = v
	}
	if v, ok := lookup("NEXUSCLOUD_TLS_CERT_FILE"); ok {
		cfg.Server.TLSCertFile = v
	}
	if v, ok := lookup("NEXUSCLOUD_TLS_KEY_FILE"); ok {
		cfg.Server.TLSKeyFile = v
	}
	if v, ok := lookup("NEXUSCLOUD_TRUSTED_PROXIES"); ok {
		cfg.Server.TrustedProxies = splitAndTrim(v)
	}
	if v, ok := lookup("NEXUSCLOUD_DATA_DIR"); ok {
		cfg.Storage.DataDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_STORAGE_DIR"); ok {
		cfg.Storage.StorageDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_DATABASE_DIR"); ok {
		cfg.Storage.DatabaseDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_CACHE_DIR"); ok {
		cfg.Storage.CacheDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_THUMBNAILS_DIR"); ok {
		cfg.Storage.ThumbnailsDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_VERSIONS_DIR"); ok {
		cfg.Storage.VersionsDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_TEMP_DIR"); ok {
		cfg.Storage.TempDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_LOGS_DIR"); ok {
		cfg.Storage.LogsDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_BACKUPS_DIR"); ok {
		cfg.Storage.BackupsDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_CONFIG_DIR"); ok {
		cfg.Storage.ConfigDir = v
	}
	if v, ok := lookup("NEXUSCLOUD_DB_DRIVER"); ok {
		cfg.Database.Driver = v
	}
	if v, ok := lookup("NEXUSCLOUD_DB_DSN"); ok {
		cfg.Database.DSN = v
	}
	if v, ok := lookupBool("NEXUSCLOUD_API_ENABLED"); ok {
		cfg.API.Enabled = v
	}
	if v, ok := lookupBool("NEXUSCLOUD_WEB_ENABLED"); ok {
		cfg.Web.Enabled = v
	}
	if v, ok := lookupBool("NEXUSCLOUD_CLIENT_UPDATES_ENABLED"); ok {
		cfg.ClientUpdates.Enabled = v
	}
	if v, ok := lookup("NEXUSCLOUD_CLIENT_UPDATES_GITHUB_REPO"); ok {
		cfg.ClientUpdates.GithubRepo = v
	}
	if v, ok := lookup("NEXUSCLOUD_CORS_ALLOWED_ORIGINS"); ok {
		cfg.Security.CORSAllowedOrigins = splitAndTrim(v)
	}
	if v, ok := lookupBool("NEXUSCLOUD_PUBLIC_REGISTRATION_ENABLED"); ok {
		cfg.Security.PublicRegistrationEnabled = v
	}
	if v, ok := lookupBool("NEXUSCLOUD_WEBAUTHN_ENABLED"); ok {
		cfg.Security.WebAuthn.Enabled = v
	}
	if v, ok := lookup("NEXUSCLOUD_WEBAUTHN_RP_ID"); ok {
		cfg.Security.WebAuthn.RPID = v
	}
	if v, ok := lookup("NEXUSCLOUD_WEBAUTHN_RP_ORIGIN"); ok {
		cfg.Security.WebAuthn.RPOrigin = v
	}
	if v, ok := lookup("NEXUSCLOUD_LOG_LEVEL"); ok {
		cfg.Logging.Level = v
	}
	if v, ok := lookup("NEXUSCLOUD_LOG_FORMAT"); ok {
		cfg.Logging.Format = v
	}
	if v, ok := lookup("NEXUSCLOUD_LOG_OUTPUT"); ok {
		cfg.Logging.Output = v
	}
}

func lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func lookupInt(key string) (int, bool) {
	v, ok := lookup(key)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

func lookupBool(key string) (bool, bool) {
	v, ok := lookup(key)
	if !ok {
		return false, false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, false
	}
	return b, true
}

func splitAndTrim(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
