package db

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/config"
)

const (
	int32Max = int64(2147483647)
	gib5     = int64(5) << 30
	gib100   = int64(100) << 30
)

// realScratchConfig crea una base de datos vacía y exclusiva de este test en
// el servidor real de NEXUSCLOUD_TEST_<MOTOR>_DSN y devuelve una configuración
// que apunta a ella. Hace falta una propia (no la compartida de dbtest.OpenReal)
// porque el test lleva el esquema a una versión concreta y de vuelta: si lo
// hiciera sobre la base compartida, los demás paquetes, que corren en
// paralelo contra ella, verían cambiar las columnas a mitad de sus pruebas.
func realScratchConfig(t *testing.T, driver string) *config.Config {
	t.Helper()
	envVar := "NEXUSCLOUD_TEST_" + strings.ToUpper(driver) + "_DSN"
	baseDSN := os.Getenv(envVar)
	if baseDSN == "" {
		t.Skipf("%s no está puesta -- test saltado (ver scripts/dev.sh test-%s)", envVar, driver)
	}
	name := fmt.Sprintf("nc_upgrade_%d", time.Now().UnixNano())

	var scratchDSN string
	switch driver {
	case "mysql":
		i := strings.LastIndex(baseDSN, "/")
		params := ""
		if j := strings.Index(baseDSN[i+1:], "?"); j >= 0 {
			params = baseDSN[i+1:][j:]
		}
		scratchDSN = baseDSN[:i+1] + name + params
	case "postgres":
		u, err := url.Parse(baseDSN)
		if err != nil {
			t.Fatalf("DSN de postgres inválido: %v", err)
		}
		u.Path = "/" + name
		scratchDSN = u.String()
	default:
		t.Fatalf("driver sin soporte en este test: %s", driver)
	}

	adminCfg := config.Defaults()
	adminCfg.Database.Driver, adminCfg.Database.DSN = driver, baseDSN
	admin, err := Open(adminCfg)
	if err != nil {
		t.Fatalf("Open(%s) como administrador: %v", driver, err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("creando la base %s: %v", name, err)
	}
	t.Cleanup(func() {
		q := "DROP DATABASE " + name
		if driver == "postgres" {
			q += " WITH (FORCE)"
		}
		if _, err := admin.Exec(q); err != nil {
			t.Logf("no se pudo borrar la base %s: %v", name, err)
		}
	})

	cfg := config.Defaults()
	cfg.Database.Driver, cfg.Database.DSN = driver, scratchDSN
	return cfg
}

// TestRealEnginesUpgradeSizeColumnsToBigInt (migración 0011): en PostgreSQL y
// MySQL las columnas de tamaño en bytes eran de 32 bits (máx. 2 GiB), así que
// ni una cuota de 100 GiB cabía en quota_bytes ni un archivo de más de 2 GiB
// se podía registrar. El test parte de una base en la versión 10 CON datos
// (incluido el valor máximo de 32 bits), comprueba que el defecto existía
// (control negativo), aplica la 0011 y verifica que los datos siguen intactos,
// que ya caben valores de 5 y 100 GiB y que la `down` vuelve a las columnas de
// 32 bits.
func TestRealEnginesUpgradeSizeColumnsToBigInt(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			cfg := realScratchConfig(t, driver)
			conn, err := Open(cfg)
			if err != nil {
				t.Fatalf("Open(%s): %v", driver, err)
			}
			t.Cleanup(func() { conn.Close() })
			m, err := newMigrator(cfg, conn)
			if err != nil {
				t.Fatalf("newMigrator: %v", err)
			}
			if err := m.Migrate(10); err != nil {
				t.Fatalf("migrando a la versión 10: %v", err)
			}

			exec := func(q string, args ...any) error {
				_, err := conn.Exec(Rebind(driver, q), args...)
				return err
			}
			mustExec := func(q string, args ...any) {
				t.Helper()
				if err := exec(q, args...); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
			}
			scan := func(q string, args ...any) int64 {
				t.Helper()
				var v int64
				if err := conn.QueryRow(Rebind(driver, q), args...).Scan(&v); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
				return v
			}
			colType := func(table, column string) string {
				t.Helper()
				q := "SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?"
				if driver == "mysql" {
					q = "SELECT DATA_TYPE FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?"
				}
				var v string
				if err := conn.QueryRow(Rebind(driver, q), table, column).Scan(&v); err != nil {
					t.Fatalf("tipo de %s.%s: %v", table, column, err)
				}
				return strings.ToLower(v)
			}
			groupsTable := "groups"
			if driver == "mysql" {
				groupsTable = "`groups`"
			}

			// Datos reales en la versión 10, con el máximo de 32 bits.
			ts := "2026-01-01T00:00:00Z"
			mustExec(`INSERT INTO users (id, username, display_name, password_hash, status, quota_bytes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				"u1", "ana", "Ana", "x", "active", int32Max, ts, ts)
			mustExec(`INSERT INTO `+groupsTable+` (id, name, quota_bytes, created_at) VALUES (?, ?, ?, ?)`, "g1", "familia", int32Max, ts)
			mustExec(`INSERT INTO storage_pools (id, name, type, path, priority, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				"p1", "principal", "local", "/tmp/nc", 0, "active", ts)
			mustExec(`INSERT INTO files (id, pool_id, owner_id, parent_path, name, size_bytes, sha256, mime_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				"f1", "p1", "u1", "/", "a.bin", int32Max, "h", "application/octet-stream", ts, ts)
			mustExec(`INSERT INTO file_versions (id, file_id, version_num, size_bytes, sha256, mime_type, storage_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				"v1", "f1", 1, int32Max, "h", "application/octet-stream", "k", ts)
			mustExec(`INSERT INTO backup_jobs (id, status, destination_path, pool_ids_json, file_count, total_bytes, started_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				"b1", "completed", "/d", "[]", 1, int32Max, ts)

			// Control negativo: el defecto que corrige la 0011.
			if err := exec(`UPDATE files SET size_bytes = ? WHERE id = ?`, gib5, "f1"); err == nil {
				t.Fatal("en la versión 10 un archivo de 5 GiB debería ser rechazado (columna de 32 bits): sin ese fallo, este test no demuestra nada")
			}

			if err := m.Migrate(11); err != nil {
				t.Fatalf("aplicando la 0011 sobre datos existentes: %v", err)
			}
			if v, dirty, err := Status(cfg, conn); err != nil || dirty || v != 11 {
				t.Fatalf("Status tras la 0011 = (%d, dirty=%v, %v), esperado (11, false, nil)", v, dirty, err)
			}

			// Los datos previos siguen intactos.
			for q, want := range map[string]int64{
				`SELECT quota_bytes FROM users WHERE id = 'u1'`:               int32Max,
				`SELECT quota_bytes FROM ` + groupsTable + ` WHERE id = 'g1'`: int32Max,
				`SELECT size_bytes FROM files WHERE id = 'f1'`:                int32Max,
				`SELECT size_bytes FROM file_versions WHERE id = 'v1'`:        int32Max,
				`SELECT total_bytes FROM backup_jobs WHERE id = 'b1'`:         int32Max,
			} {
				if got := scan(q); got != want {
					t.Errorf("%s = %d tras la migración, esperado %d", q, got, want)
				}
			}

			// Ahora caben valores de más de 32 bits.
			mustExec(`UPDATE users SET quota_bytes = ? WHERE id = ?`, gib100, "u1")
			mustExec(`UPDATE `+groupsTable+` SET quota_bytes = ? WHERE id = ?`, gib100, "g1")
			mustExec(`UPDATE files SET size_bytes = ? WHERE id = ?`, gib5, "f1")
			mustExec(`UPDATE file_versions SET size_bytes = ? WHERE id = ?`, gib5, "v1")
			mustExec(`UPDATE backup_jobs SET total_bytes = ? WHERE id = ?`, gib5, "b1")
			for q, want := range map[string]int64{
				`SELECT quota_bytes FROM users WHERE id = 'u1'`:               gib100,
				`SELECT quota_bytes FROM ` + groupsTable + ` WHERE id = 'g1'`: gib100,
				`SELECT size_bytes FROM files WHERE id = 'f1'`:                gib5,
				`SELECT size_bytes FROM file_versions WHERE id = 'v1'`:        gib5,
				`SELECT total_bytes FROM backup_jobs WHERE id = 'b1'`:         gib5,
			} {
				if got := scan(q); got != want {
					t.Errorf("%s = %d, esperado %d", q, got, want)
				}
			}

			// Las seis columnas quedan como bigint (shares no se puede sembrar
			// sin más FK: se comprueba por el tipo).
			columns := [][2]string{
				{"users", "quota_bytes"}, {strings.Trim(groupsTable, "`"), "quota_bytes"},
				{"files", "size_bytes"}, {"file_versions", "size_bytes"},
				{"shares", "max_upload_size_bytes"}, {"backup_jobs", "total_bytes"},
			}
			for _, c := range columns {
				if got := colType(c[0], c[1]); got != "bigint" {
					t.Errorf("%s.%s es %q tras la 0011, esperado bigint", c[0], c[1], got)
				}
			}

			// El índice de cobertura del cálculo de uso existe tras la 0011.
			indexCount := func() int64 {
				t.Helper()
				q := "SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'files' AND indexname = 'idx_files_owner_usage'"
				if driver == "mysql" {
					q = "SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'files' AND index_name = 'idx_files_owner_usage'"
				}
				return scan(q)
			}
			if got := indexCount(); got != 1 {
				t.Errorf("idx_files_owner_usage: %d tras la 0011, esperado 1", got)
			}

			// La `down` es SQL válido: con valores que caben vuelve a 32 bits.
			mustExec(`UPDATE users SET quota_bytes = ? WHERE id = ?`, int32Max, "u1")
			mustExec(`UPDATE `+groupsTable+` SET quota_bytes = ? WHERE id = ?`, int32Max, "g1")
			mustExec(`UPDATE files SET size_bytes = ? WHERE id = ?`, int32Max, "f1")
			mustExec(`UPDATE file_versions SET size_bytes = ? WHERE id = ?`, int32Max, "v1")
			mustExec(`UPDATE backup_jobs SET total_bytes = ? WHERE id = ?`, int32Max, "b1")
			if err := m.Migrate(10); err != nil {
				t.Fatalf("revirtiendo la 0011: %v", err)
			}
			if got := indexCount(); got != 0 {
				t.Errorf("idx_files_owner_usage: %d tras revertir la 0011, esperado 0", got)
			}
			want32 := "int"
			if driver == "postgres" {
				want32 = "integer"
			}
			for _, c := range columns {
				if got := colType(c[0], c[1]); got != want32 {
					t.Errorf("%s.%s es %q tras revertir la 0011, esperado %s", c[0], c[1], got, want32)
				}
			}
		})
	}
}
