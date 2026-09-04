// Package db gestiona la conexión a base de datos y las migraciones de
// esquema (§8). No modela ningún dominio: cada paquete de dominio
// (internal/users, internal/auth, internal/storage, internal/audit) define
// su propio repositorio sobre el *sql.DB que este paquete abre.
package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // driver "pgx" para database/sql
	_ "modernc.org/sqlite"             // driver "sqlite" para database/sql, sin CGO

	"github.com/porrii/nexuscloud/internal/config"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrationsFS embed.FS

//go:embed migrations/postgres/*.sql
var postgresMigrationsFS embed.FS

// Open abre la conexión al motor configurado en cfg.Database.Driver. No
// aplica migraciones (ver Migrate) para que comandos de solo-lectura como
// `migrate status` puedan inspeccionar sin arrancar el pool completo.
func Open(cfg *config.Config) (*sql.DB, error) {
	switch cfg.Database.Driver {
	case "sqlite":
		return openSQLite(cfg.SQLiteDSN())
	case "postgres":
		conn, err := sql.Open("pgx", cfg.Database.DSN)
		if err != nil {
			return nil, fmt.Errorf("abriendo postgres: %w", err)
		}
		return conn, nil
	default:
		return nil, fmt.Errorf("driver de base de datos no soportado: %q", cfg.Database.Driver)
	}
}

func openSQLite(dsn string) (*sql.DB, error) {
	if dir := filepath.Dir(dsn); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("creando directorio de base de datos %s: %w", dir, err)
		}
	}
	conn, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("abriendo sqlite %s: %w", dsn, err)
	}
	// Una única conexión física: con el driver puro Go modernc.org/sqlite
	// evita errores "database is locked" bajo escritura concurrente desde
	// varias goroutines. Prioriza correctness sobre concurrencia de lectura,
	// razonable a la escala objetivo (~100 usuarios, §133/§160).
	conn.SetMaxOpenConns(1)
	return conn, nil
}

func newMigrator(cfg *config.Config, conn *sql.DB) (*migrate.Migrate, error) {
	var (
		driver database.Driver
		mfs    embed.FS
		subdir string
		err    error
	)

	switch cfg.Database.Driver {
	case "sqlite":
		driver, err = sqlite.WithInstance(conn, &sqlite.Config{})
		mfs, subdir = sqliteMigrationsFS, "migrations/sqlite"
	case "postgres":
		driver, err = postgres.WithInstance(conn, &postgres.Config{})
		mfs, subdir = postgresMigrationsFS, "migrations/postgres"
	default:
		return nil, fmt.Errorf("driver de base de datos no soportado: %q", cfg.Database.Driver)
	}
	if err != nil {
		return nil, fmt.Errorf("preparando driver de migración: %w", err)
	}

	sub, err := fs.Sub(mfs, subdir)
	if err != nil {
		return nil, fmt.Errorf("preparando migraciones embebidas: %w", err)
	}
	src, err := iofs.New(sub, ".")
	if err != nil {
		return nil, fmt.Errorf("preparando fuente de migraciones: %w", err)
	}

	return migrate.NewWithInstance("iofs", src, cfg.Database.Driver, driver)
}

// Migrate aplica todas las migraciones pendientes. Los ficheros *.up.sql son
// responsables de no ser destructivos (§8): este runner nunca borra datos
// por sí mismo.
func Migrate(cfg *config.Config, conn *sql.DB) error {
	m, err := newMigrator(cfg, conn)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("aplicando migraciones: %w", err)
	}
	return nil
}

// Status devuelve la versión de esquema actualmente aplicada y si quedó en
// estado "dirty" (una migración falló a mitad de aplicarse).
func Status(cfg *config.Config, conn *sql.DB) (version uint, dirty bool, err error) {
	m, err := newMigrator(cfg, conn)
	if err != nil {
		return 0, false, err
	}
	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return version, dirty, err
}
