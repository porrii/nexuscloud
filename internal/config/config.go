// Package config carga y valida la configuración de NexusCloud a partir de
// (en orden creciente de prioridad) valores por defecto, config.yaml,
// variables de entorno NEXUSCLOUD_* y, en el CLI, flags explícitos.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// CurrentConfigVersion es la versión de esquema que este binario entiende.
// Cuando el esquema cambie de forma incompatible, incrementar esta constante
// y añadir la migración correspondiente en migrateSchema (§151).
const CurrentConfigVersion = 1

// Config es el árbol completo de configuración de una instancia NexusCloud.
type Config struct {
	ConfigVersion int              `yaml:"configVersion"`
	General       GeneralConfig    `yaml:"general"`
	Server        ServerConfig     `yaml:"server"`
	Database      DatabaseConfig   `yaml:"database"`
	Storage       StorageConfig    `yaml:"storage"`
	Security      SecurityConfig   `yaml:"security"`
	Trash         TrashConfig      `yaml:"trash"`
	Versioning    VersioningConfig `yaml:"versioning"`
	API           APIConfig        `yaml:"api"`
	Web           WebConfig        `yaml:"web"`
	Logging       LoggingConfig    `yaml:"logging"`
}

type GeneralConfig struct {
	Name     string `yaml:"name"`
	Language string `yaml:"language"`
	Timezone string `yaml:"timezone"`
}

// ServerConfig controla el listener HTTP(S) único de esta instancia. API y
// Web comparten puerto por defecto (§51: una app Nexus = un puerto); cada
// superficie se activa o no de forma independiente vía APIConfig/WebConfig.
type ServerConfig struct {
	Host           string   `yaml:"host"`
	Port           int      `yaml:"port"`
	TLSCertFile    string   `yaml:"tlsCertFile"`
	TLSKeyFile     string   `yaml:"tlsKeyFile"`
	TrustedProxies []string `yaml:"trustedProxies"`
}

// DatabaseConfig selecciona el motor de base de datos. NexusCloud nunca debe
// requerir cambios de código para cambiar de motor (§8).
type DatabaseConfig struct {
	Driver string `yaml:"driver"` // "sqlite" | "postgres"
	DSN    string `yaml:"dsn"`    // sqlite: ruta de fichero; postgres: connection string
}

// StorageConfig separa el directorio de datos general de la raíz de
// almacenamiento de archivos de usuario (§154-156); ambos son reubicables
// de forma independiente (p.ej. a discos distintos).
type StorageConfig struct {
	DataDir    string `yaml:"dataDir"`
	StorageDir string `yaml:"storageDir"`
}

type SecurityConfig struct {
	SessionTTLHours           int             `yaml:"sessionTTLHours"`
	Argon2                    Argon2Config    `yaml:"argon2"`
	RateLimit                 RateLimitConfig `yaml:"rateLimit"`
	CORSAllowedOrigins        []string        `yaml:"corsAllowedOrigins"`
	PublicRegistrationEnabled bool            `yaml:"publicRegistrationEnabled"`
}

// Argon2Config son los parámetros de coste de Argon2id (§25). Los valores
// por defecto (64 MiB, t=3, p=4) igualan a los ya validados en NexusKeys
// para mantener consistencia de ecosistema; un administrador con hardware
// limitado (p.ej. Raspberry Pi) puede reducirlos.
type Argon2Config struct {
	MemoryKiB   uint32 `yaml:"memoryKiB"`
	Iterations  uint32 `yaml:"iterations"`
	Parallelism uint8  `yaml:"parallelism"`
}

// TrashConfig gobierna la papelera (§16), la primera línea de defensa
// contra un borrado accidental o ransomware (§128). Con Enabled=false,
// eliminar un archivo/carpeta es inmediato y permanente -- sin red de
// seguridad -- así que el valor por defecto es true.
type TrashConfig struct {
	Enabled       bool `yaml:"enabled"`
	RetentionDays int  `yaml:"retentionDays"`
}

// VersioningConfig gobierna el historial de versiones (§15). Con
// Enabled=false, subir a un path existente sigue sobrescribiendo sin dejar
// rastro, como en la Fase 1. MaxVersionsPerFile acota el espacio: al
// superarse, se purga la versión más antigua (§15 "política automática de
// limpieza"); no hay todavía límite por antigüedad ni por espacio total.
type VersioningConfig struct {
	Enabled            bool `yaml:"enabled"`
	MaxVersionsPerFile int  `yaml:"maxVersionsPerFile"`
}

type RateLimitConfig struct {
	LoginPerMinute int `yaml:"loginPerMinute"`
	APIPerMinute   int `yaml:"apiPerMinute"`
}

type APIConfig struct {
	Enabled bool `yaml:"enabled"`
}

// WebConfig activa o no la interfaz web. Desactivada por defecto (§3, §47):
// cuando está en false, el servidor no debe montar ninguna ruta de la UI.
type WebConfig struct {
	Enabled bool `yaml:"enabled"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`  // trace|debug|info|warn|error|fatal
	Format string `yaml:"format"` // text|json
	Output string `yaml:"output"` // "stdout" o ruta de fichero
}

// Defaults devuelve la configuración segura por defecto (§3, §169): sin web
// pública, sin registro público, rate limiting activo, Argon2id robusto.
func Defaults() *Config {
	return &Config{
		ConfigVersion: CurrentConfigVersion,
		General: GeneralConfig{
			Name:     "NexusCloud",
			Language: "es",
			Timezone: "UTC",
		},
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
		},
		Storage: StorageConfig{
			DataDir: defaultDataDir(),
		},
		Security: SecurityConfig{
			SessionTTLHours: 24 * 30,
			Argon2: Argon2Config{
				MemoryKiB:   64 * 1024,
				Iterations:  3,
				Parallelism: 4,
			},
			RateLimit: RateLimitConfig{
				LoginPerMinute: 5,
				APIPerMinute:   300,
			},
			CORSAllowedOrigins:        []string{},
			PublicRegistrationEnabled: false,
		},
		Trash:      TrashConfig{Enabled: true, RetentionDays: 30},
		Versioning: VersioningConfig{Enabled: true, MaxVersionsPerFile: 10},
		API:        APIConfig{Enabled: true},
		Web:        WebConfig{Enabled: false},
		Logging:    LoggingConfig{Level: "info", Format: "text", Output: "stdout"},
	}
}

func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "NexusCloud")
		}
		return `C:\NexusCloud`
	}
	return "/var/lib/nexuscloud"
}

// Load lee la configuración desde path (si existe), aplica overrides de
// entorno, migra el esquema si hace falta y valida el resultado. path puede
// estar vacío: en ese caso se usan solo defaults + entorno.
func Load(path string) (*Config, error) {
	cfg := Defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parseando %s: %w", path, err)
			}
		case os.IsNotExist(err):
			// Sin fichero de configuración: seguimos solo con defaults + entorno.
		default:
			return nil, fmt.Errorf("leyendo %s: %w", path, err)
		}
	}

	applyEnvOverrides(cfg)

	if err := migrateSchema(cfg); err != nil {
		return nil, err
	}

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("configuración inválida: %w", err)
	}

	return cfg, nil
}

func migrateSchema(cfg *Config) error {
	if cfg.ConfigVersion == 0 {
		cfg.ConfigVersion = CurrentConfigVersion
		return nil
	}
	if cfg.ConfigVersion > CurrentConfigVersion {
		return fmt.Errorf(
			"configVersion %d no soportado por este binario (máximo soportado: %d); actualiza NexusCloud",
			cfg.ConfigVersion, CurrentConfigVersion,
		)
	}
	// Futuras migraciones 1→2, 2→3, ... se añaden aquí de forma incremental.
	return nil
}

// SQLiteDSN resuelve la ruta del fichero SQLite: usa database.dsn si se
// especificó explícitamente, o <dataDir>/database/nexuscloud.db si no.
func (c *Config) SQLiteDSN() string {
	if c.Database.DSN != "" {
		return c.Database.DSN
	}
	return filepath.Join(c.Storage.DataDir, "database", "nexuscloud.db")
}

// DefaultStorageDir resuelve la raíz del pool de almacenamiento por defecto.
func (c *Config) DefaultStorageDir() string {
	if c.Storage.StorageDir != "" {
		return c.Storage.StorageDir
	}
	return filepath.Join(c.Storage.DataDir, "storage")
}

func (c *Config) DatabaseDir() string { return filepath.Join(c.Storage.DataDir, "database") }
func (c *Config) CacheDir() string    { return filepath.Join(c.Storage.DataDir, "cache") }
func (c *Config) LogsDir() string     { return filepath.Join(c.Storage.DataDir, "logs") }
func (c *Config) BackupsDir() string  { return filepath.Join(c.Storage.DataDir, "backups") }
func (c *Config) ConfigDir() string   { return filepath.Join(c.Storage.DataDir, "config") }

// EnsureDataDirs crea el árbol de directorios de datos (§155-156) si no
// existe. No crea el StorageDir de pools adicionales, solo la estructura base.
func (c *Config) EnsureDataDirs() error {
	dirs := []string{
		c.Storage.DataDir,
		c.DatabaseDir(),
		c.DefaultStorageDir(),
		c.CacheDir(),
		c.LogsDir(),
		c.BackupsDir(),
		c.ConfigDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("creando directorio %s: %w", d, err)
		}
	}
	return nil
}
