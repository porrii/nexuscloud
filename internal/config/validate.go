package config

import "fmt"

// Validate comprueba invariantes de seguridad y coherencia antes de que la
// configuración se use para arrancar el servidor. Nunca debe aplicarse una
// configuración inválida (§152).
func Validate(cfg *Config) error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port fuera de rango: %d", cfg.Server.Port)
	}

	switch cfg.Database.Driver {
	case "sqlite", "postgres":
	default:
		return fmt.Errorf("database.driver no soportado: %q (usa 'sqlite' o 'postgres')", cfg.Database.Driver)
	}
	if cfg.Database.Driver == "postgres" && cfg.Database.DSN == "" {
		return fmt.Errorf("database.dsn es obligatorio cuando database.driver=postgres")
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

	for _, o := range cfg.Security.CORSAllowedOrigins {
		if o == "*" {
			return fmt.Errorf("security.corsAllowedOrigins no puede contener '*' (secure by default, §69)")
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

	return nil
}
