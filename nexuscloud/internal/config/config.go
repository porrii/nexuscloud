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
	ConfigVersion int                 `yaml:"configVersion"`
	General       GeneralConfig       `yaml:"general"`
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Storage       StorageConfig       `yaml:"storage"`
	Security      SecurityConfig      `yaml:"security"`
	Trash         TrashConfig         `yaml:"trash"`
	Versioning    VersioningConfig    `yaml:"versioning"`
	Sharing       SharingConfig       `yaml:"sharing"`
	Backup        BackupConfig        `yaml:"backup"`
	ClientUpdates ClientUpdatesConfig `yaml:"clientUpdates"`
	API           APIConfig           `yaml:"api"`
	Web           WebConfig           `yaml:"web"`
	Logging       LoggingConfig       `yaml:"logging"`
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
	Driver string `yaml:"driver"` // "sqlite" | "postgres" | "mysql"
	DSN    string `yaml:"dsn"`    // sqlite: ruta de fichero; postgres/mysql: connection string
}

// StorageConfig separa el directorio de datos general de la raíz de
// almacenamiento de archivos de usuario (§154-156); ambos son reubicables
// de forma independiente (p.ej. a discos distintos).
//
// Cada área de almacenamiento (base de datos, caché, miniaturas, versionado,
// temporales, logs, backups, config) tiene además un override opcional: si
// está vacío se usa <DataDir>/<área>, el comportamiento histórico; si se
// rellena, esa área concreta puede vivir en otro disco sin mover el resto.
// Son campos nuevos opcionales: no obligan a subir configVersion.
type StorageConfig struct {
	DataDir    string `yaml:"dataDir"`
	StorageDir string `yaml:"storageDir"`

	DatabaseDir   string `yaml:"databaseDir,omitempty"`
	CacheDir      string `yaml:"cacheDir,omitempty"`
	ThumbnailsDir string `yaml:"thumbnailsDir,omitempty"`
	VersionsDir   string `yaml:"versionsDir,omitempty"`
	TempDir       string `yaml:"tempDir,omitempty"`
	LogsDir       string `yaml:"logsDir,omitempty"`
	BackupsDir    string `yaml:"backupsDir,omitempty"`
	ConfigDir     string `yaml:"configDir,omitempty"`
}

type SecurityConfig struct {
	SessionTTLHours           int             `yaml:"sessionTTLHours"`
	Argon2                    Argon2Config    `yaml:"argon2"`
	RateLimit                 RateLimitConfig `yaml:"rateLimit"`
	CORSAllowedOrigins        []string        `yaml:"corsAllowedOrigins"`
	PublicRegistrationEnabled bool            `yaml:"publicRegistrationEnabled"`
	WebAuthn                  WebAuthnConfig  `yaml:"webAuthn"`
}

// WebAuthnConfig gobierna Passkeys/WebAuthn (§25, ADR-033). Enabled=false
// por defecto -- a diferencia de TOTP (que no necesita nada del servidor
// más allá del propio secreto por usuario), WebAuthn exige que el servidor
// declare su Relying Party ID/origin de antemano: sin un RPID/RPOrigin
// reales el navegador rechaza cualquier reto, así que activarlo a ciegas
// con valores vacíos rompería el protocolo en vez de simplemente no hacer
// nada. RPID es el dominio (p.ej. "nexuscloud.example.com", nunca incluye
// esquema ni puerto -- así lo exige el estándar) y RPOrigin es el origen
// completo tal y como lo ve el navegador (p.ej.
// "https://nexuscloud.example.com").
type WebAuthnConfig struct {
	Enabled  bool   `yaml:"enabled"`
	RPID     string `yaml:"rpID"`
	RPOrigin string `yaml:"rpOrigin"`
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
// seguridad -- así que el valor por defecto es true. MaxTotalSizeBytes
// (0 = sin límite) es un límite de PROTECCIÓN DE ESPACIO, no una promesa de
// cuánto conservar -- por eso compone con RetentionDays como "el más
// restrictivo gana" (se poda si cualquiera de los dos lo pide), al revés
// que la retención de backups (ADR-017), que compone como "el más generoso
// gana". Ver ADR-023.
type TrashConfig struct {
	Enabled           bool  `yaml:"enabled"`
	RetentionDays     int   `yaml:"retentionDays"`
	MaxTotalSizeBytes int64 `yaml:"maxTotalSizeBytes"`
}

// VersioningConfig gobierna el historial de versiones (§15). Con
// Enabled=false, subir a un path existente sigue sobrescribiendo sin dejar
// rastro, como en la Fase 1. Las tres políticas (MaxVersionsPerFile,
// MaxVersionAgeDays, MaxVersionsTotalSizeBytes; 0 = sin límite cada una)
// componen como "el más restrictivo gana" -- una versión se purga si
// CUALQUIER política activa lo pide, no solo si todas coinciden. Mismo
// criterio que TrashConfig y por el mismo motivo: son límites de espacio en
// disco, no una promesa de cuánto historial conservar (al revés que la
// retención de backups, ADR-017). Ver ADR-024.
type VersioningConfig struct {
	Enabled                   bool  `yaml:"enabled"`
	MaxVersionsPerFile        int   `yaml:"maxVersionsPerFile"`
	MaxVersionAgeDays         int   `yaml:"maxVersionAgeDays"`
	MaxVersionsTotalSizeBytes int64 `yaml:"maxVersionsTotalSizeBytes"`
}

// SharingConfig gobierna la compartición (§37). Enabled cubre usuario→usuario
// y usuario→grupo: no añaden ninguna superficie sin autenticar (siguen
// exigiendo sesión igual que el resto de la API), así que su valor por
// defecto es true, igual que trash/versioning. PublicLinksEnabled cubre los
// enlaces públicos, la ÚNICA superficie que Sharing expone sin sesión --
// coherente con el precedente ya sentado por web.enabled (§3, §47), su valor
// por defecto es false: el administrador debe activarla explícitamente.
type SharingConfig struct {
	Enabled            bool `yaml:"enabled"`
	PublicLinksEnabled bool `yaml:"publicLinksEnabled"`
}

// BackupConfig gobierna el backup automático (§18 "programación"). A
// diferencia de Trash/Versioning/Sharing (que son "gratis" y por eso
// activadas por defecto), un backup automático copia datos reales a
// cfg.BackupsDir() -- por defecto en EL MISMO disco que el almacenamiento
// principal (§19: eso no cumple la regla 3-2-1, cero protección ante un
// fallo de disco) y consume espacio sin límite todavía (sin retención en
// este slice, ADR-016). Por eso Enabled es false por defecto: el
// administrador debe activarlo de forma explícita y consciente, igual que
// sharing.publicLinksEnabled y web.enabled.
type BackupConfig struct {
	Enabled         bool `yaml:"enabled"`
	IntervalMinutes int  `yaml:"intervalMinutes"`
	// RetentionCount/RetentionDays, si son > 0, podan backups completados por
	// destino tras cada backup (ADR-017): se componen como "unión de
	// motivos para conservar" -- un backup se poda solo si TODAS las
	// políticas activas votan podarlo; basta con que una activa vote
	// conservarlo para que sobreviva. Ambas en 0 (por defecto) = sin
	// límite -- no se borra nada hasta que el administrador lo pida.
	RetentionCount int `yaml:"retentionCount"`
	RetentionDays  int `yaml:"retentionDays"`
	// Incremental (false por defecto): un fichero cuyo SHA-256 no cambió
	// desde el backup anterior en el MISMO destino se enlaza (hardlink) a
	// su copia ya existente en vez de recopiarse -- ahorra tiempo/espacio
	// en instalaciones con backup automático y árboles grandes. El
	// manifiesto sigue siendo siempre completo (todos los ficheros
	// activos): cada job resultante sigue siendo independientemente
	// restaurable, Restore/RestoreToPool/Verify no necesitan saber que
	// esto existe. Ver ADR-026.
	Incremental bool `yaml:"incremental"`
	// Encrypt (false por defecto, ADR-028): cifra cada backup automático
	// con AES-256-CTR. La passphrase nunca vive en este config.yaml -- se
	// lee de la variable de entorno NEXUSCLOUD_BACKUP_PASSPHRASE al
	// arrancar el servidor (internal/server.Build), la única vía que
	// funciona igual para el modo automático (sin terminal) que para
	// "backup run --encrypt" manual. Con Encrypt=true, Incremental deja de
	// enlazar ficheros para ese run (ver ADR-028) -- ambas cosas pueden
	// activarse a la vez sin error, simplemente el ahorro de espacio de
	// Incremental no aplica mientras Encrypt esté activo.
	Encrypt bool `yaml:"encrypt"`
	// RemoteDestination (vacío por defecto, ADR-029): URL http(s):// de
	// otro servidor NexusCloud al que respaldar en vez de la carpeta local
	// de siempre (cfg.BackupsDir()). El token de autenticación tampoco
	// vive aquí -- se lee de NEXUSCLOUD_BACKUP_REMOTE_TOKEN al arrancar el
	// servidor, mismo criterio que la passphrase de cifrado.
	RemoteDestination string `yaml:"remoteDestination"`
}

// ClientUpdatesConfig gobierna el proxy de auto-actualización del cliente de
// escritorio (Velopack, ADR-032): el propio servidor NexusCloud reenvía el
// feed de versiones y los paquetes desde GitHub Releases del repositorio del
// proyecto, para que el cliente instalado nunca necesite hablar con GitHub
// ni llevar ningún token propio -- mismo motivo de fondo que
// backup.remoteDestination (secreto fuera del cliente/config.yaml). Enabled
// es false por defecto (secure-by-default, igual que web.enabled y
// sharing.publicLinksEnabled): es una superficie pública sin sesión nueva,
// el administrador debe activarla a propósito. El token de GitHub tampoco
// vive aquí -- se lee de NEXUSCLOUD_CLIENT_UPDATES_GITHUB_TOKEN al arrancar
// el servidor, mismo criterio exacto que NEXUSCLOUD_BACKUP_REMOTE_TOKEN.
type ClientUpdatesConfig struct {
	Enabled bool `yaml:"enabled"`
	// GithubRepo, formato "propietario/repositorio" (p.ej. "porrii/nexuscloud").
	GithubRepo string `yaml:"githubRepo"`
	// Channel es el canal de Velopack a servir (releases.<channel>.json).
	// "win" es el único canal que produce hoy el empaquetado de Windows.
	Channel string `yaml:"channel"`
}

type RateLimitConfig struct {
	LoginPerMinute      int `yaml:"loginPerMinute"`
	APIPerMinute        int `yaml:"apiPerMinute"`
	PublicLinkPerMinute int `yaml:"publicLinkPerMinute"`
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
				LoginPerMinute:      5,
				APIPerMinute:        300,
				PublicLinkPerMinute: 20,
			},
			CORSAllowedOrigins:        []string{},
			PublicRegistrationEnabled: false,
			WebAuthn:                  WebAuthnConfig{Enabled: false},
		},
		Trash:         TrashConfig{Enabled: true, RetentionDays: 30},
		Versioning:    VersioningConfig{Enabled: true, MaxVersionsPerFile: 10},
		Sharing:       SharingConfig{Enabled: true, PublicLinksEnabled: false},
		Backup:        BackupConfig{Enabled: false, IntervalMinutes: 1440},
		ClientUpdates: ClientUpdatesConfig{Enabled: false, Channel: "win"},
		API:           APIConfig{Enabled: true},
		Web:           WebConfig{Enabled: false},
		Logging:       LoggingConfig{Level: "info", Format: "text", Output: "stdout"},
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

// area resuelve una ruta de área de almacenamiento: devuelve el override si
// se especificó, o <DataDir>/<name> si está vacío (comportamiento histórico).
func (c *Config) area(override, name string) string {
	if override != "" {
		return override
	}
	return filepath.Join(c.Storage.DataDir, name)
}

// SQLiteDSN resuelve la ruta del fichero SQLite: usa database.dsn si se
// especificó explícitamente, o <DatabaseDir>/nexuscloud.db si no.
func (c *Config) SQLiteDSN() string {
	if c.Database.DSN != "" {
		return c.Database.DSN
	}
	return filepath.Join(c.DatabaseDir(), "nexuscloud.db")
}

// DefaultStorageDir resuelve la raíz del pool de almacenamiento por defecto.
func (c *Config) DefaultStorageDir() string {
	if c.Storage.StorageDir != "" {
		return c.Storage.StorageDir
	}
	return filepath.Join(c.Storage.DataDir, "storage")
}

func (c *Config) DatabaseDir() string   { return c.area(c.Storage.DatabaseDir, "database") }
func (c *Config) CacheDir() string      { return c.area(c.Storage.CacheDir, "cache") }
func (c *Config) ThumbnailsDir() string { return c.area(c.Storage.ThumbnailsDir, "thumbnails") }
func (c *Config) VersionsDir() string   { return c.area(c.Storage.VersionsDir, "versions") }
func (c *Config) TempDir() string       { return c.area(c.Storage.TempDir, "tmp") }
func (c *Config) LogsDir() string       { return c.area(c.Storage.LogsDir, "logs") }
func (c *Config) BackupsDir() string    { return c.area(c.Storage.BackupsDir, "backups") }
func (c *Config) ConfigDir() string     { return c.area(c.Storage.ConfigDir, "config") }

// EnsureDataDirs crea el árbol de directorios de datos (§155-156) si no
// existe. No crea el StorageDir de pools adicionales, solo la estructura base.
func (c *Config) EnsureDataDirs() error {
	dirs := []string{
		c.Storage.DataDir,
		c.DatabaseDir(),
		c.DefaultStorageDir(),
		c.CacheDir(),
		c.ThumbnailsDir(),
		c.VersionsDir(),
		c.TempDir(),
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
