// Package dbtest ayuda a los tests de integración que necesitan un motor de
// base de datos real (mysql/postgres) en vez de sqlite en un directorio
// temporal (ADR-031). Gateado por variables de entorno para no romper
// `go test ./...` en máquinas sin Docker -- ver scripts/dev.sh
// test-mysql/test-postgres.
package dbtest

import (
	"os"
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
)

// OpenReal abre una conexión real al driver dado usando la variable de
// entorno NEXUSCLOUD_TEST_<DRIVER>_DSN (p.ej. NEXUSCLOUD_TEST_MYSQL_DSN),
// aplica todas las migraciones por el mismo camino que producción
// (db.Open + db.Migrate, nunca SQL ad-hoc) y devuelve el *db.Conn ya listo
// para usar con los repositorios de dominio. Si la variable de entorno no
// está puesta, hace t.Skip -- así el resto de la suite no depende de tener
// Docker corriendo.
func OpenReal(t *testing.T, driver string) *db.Conn {
	t.Helper()

	envVar := "NEXUSCLOUD_TEST_" + strings.ToUpper(driver) + "_DSN"
	dsn := os.Getenv(envVar)
	if dsn == "" {
		t.Skipf("%s no está puesta -- test saltado (ver scripts/dev.sh test-%s)", envVar, driver)
	}

	cfg := config.Defaults()
	cfg.Database.Driver = driver
	cfg.Database.DSN = dsn

	conn, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open(%s) falló: %v", driver, err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := db.Migrate(cfg, conn); err != nil {
		t.Fatalf("db.Migrate(%s) falló: %v", driver, err)
	}

	return db.Wrap(driver, conn)
}
