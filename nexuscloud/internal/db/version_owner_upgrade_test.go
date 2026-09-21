package db

import (
	"path/filepath"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
)

// TestVersionOwnerMigration (migración 0012, ADR-036): file_versions guarda el
// propietario de cada versión, copiado de files.owner_id, para que el uso de
// versiones de un usuario sea una sola lectura de índice en vez de un JOIN con
// todos sus archivos. El test parte de un esquema en la versión 11 CON datos de
// dos propietarios, sube a la 12 y comprueba: el relleno, que la columna es
// NOT NULL de verdad (una versión sin propietario se rechaza, no se cuenta de
// menos en silencio), que se conserva la restricción UNIQUE, el borrado en
// cascada y los dos índices; después baja a la 11 sin perder filas y vuelve a subir.
func TestVersionOwnerMigration(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			var cfg *config.Config
			if driver == "sqlite" {
				cfg = config.Defaults()
				cfg.Database.Driver = "sqlite"
				cfg.Database.DSN = filepath.Join(t.TempDir(), "version-owner.db")
			} else {
				cfg = realScratchConfig(t, driver)
			}
			conn, err := Open(cfg)
			if err != nil {
				t.Fatalf("Open(%s): %v", driver, err)
			}
			t.Cleanup(func() { conn.Close() })
			m, err := newMigrator(cfg, conn)
			if err != nil {
				t.Fatalf("newMigrator: %v", err)
			}
			if err := m.Migrate(11); err != nil {
				t.Fatalf("migrando a la versión 11: %v", err)
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
			count := func(q string, args ...any) int64 {
				t.Helper()
				var v int64
				if err := conn.QueryRow(Rebind(driver, q), args...).Scan(&v); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
				return v
			}
			ownerOf := func(versionID string) string {
				t.Helper()
				var v string
				if err := conn.QueryRow(Rebind(driver, `SELECT owner_id FROM file_versions WHERE id = ?`), versionID).Scan(&v); err != nil {
					t.Fatalf("propietario de %s: %v", versionID, err)
				}
				return v
			}
			indexExists := func(name string) bool {
				t.Helper()
				q := `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'file_versions' AND name = ?`
				switch driver {
				case "postgres":
					q = `SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'file_versions' AND indexname = ?`
				case "mysql":
					q = `SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'file_versions' AND index_name = ?`
				}
				return count(q, name) == 1
			}

			// Datos reales en la versión 11: dos propietarios con versiones.
			ts := "2026-01-01T00:00:00Z"
			for _, u := range [][2]string{{"u1", "ana"}, {"u2", "bea"}} {
				mustExec(`INSERT INTO users (id, username, display_name, password_hash, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
					u[0], u[1], u[1], "x", "active", ts, ts)
			}
			mustExec(`INSERT INTO storage_pools (id, name, type, path, priority, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				"p1", "principal", "local", "/tmp/nc", 0, "active", ts)
			for _, f := range [][3]string{{"f1", "u1", "a.txt"}, {"f2", "u2", "b.txt"}} {
				mustExec(`INSERT INTO files (id, pool_id, owner_id, parent_path, name, size_bytes, sha256, mime_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					f[0], "p1", f[1], "/", f[2], 10, "h", "text/plain", ts, ts)
			}
			insertOld := func(id, fileID string, num int, size int64) {
				mustExec(`INSERT INTO file_versions (id, file_id, version_num, size_bytes, sha256, mime_type, storage_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					id, fileID, num, size, "h", "text/plain", "k-"+id, ts)
			}
			insertOld("v1", "f1", 1, 100)
			insertOld("v2", "f1", 2, 200)
			insertOld("v3", "f2", 1, 300)

			if err := m.Migrate(12); err != nil {
				t.Fatalf("aplicando la 0012 sobre datos existentes: %v", err)
			}
			if v, dirty, err := Status(cfg, conn); err != nil || dirty || v != 12 {
				t.Fatalf("Status tras la 0012 = (%d, dirty=%v, %v), esperado (12, false, nil)", v, dirty, err)
			}

			// Relleno: cada versión lleva el propietario de SU archivo.
			for id, want := range map[string]string{"v1": "u1", "v2": "u1", "v3": "u2"} {
				if got := ownerOf(id); got != want {
					t.Errorf("propietario de %s = %q tras la 0012, esperado %q", id, got, want)
				}
			}
			if got := count(`SELECT SUM(size_bytes) FROM file_versions WHERE owner_id = ?`, "u1"); got != 300 {
				t.Errorf("suma de versiones de u1 por la columna nueva = %d, esperado 300", got)
			}

			// NOT NULL de verdad: una versión sin propietario se rechaza.
			if err := exec(`INSERT INTO file_versions (id, file_id, version_num, size_bytes, sha256, mime_type, storage_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				"v4", "f1", 3, 1, "h", "text/plain", "k4", ts); err == nil {
				t.Error("insertar una versión SIN owner_id debería fallar (NOT NULL): si no, un olvido contaría de menos en silencio")
			}
			// La restricción UNIQUE (file_id, version_num) sigue viva.
			if err := exec(`INSERT INTO file_versions (id, file_id, owner_id, version_num, size_bytes, sha256, mime_type, storage_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				"v5", "f1", "u1", 1, 1, "h", "text/plain", "k5", ts); err == nil {
				t.Error("repetir (file_id, version_num) debería fallar (UNIQUE)")
			}
			// Una versión con propietario entra con normalidad.
			mustExec(`INSERT INTO file_versions (id, file_id, owner_id, version_num, size_bytes, sha256, mime_type, storage_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				"v6", "f1", "u1", 3, 50, "h", "text/plain", "k6", ts)

			// Borrar el archivo se lleva sus versiones (FK ON DELETE CASCADE conservada).
			mustExec(`DELETE FROM files WHERE id = ?`, "f2")
			if got := count(`SELECT COUNT(*) FROM file_versions WHERE file_id = ?`, "f2"); got != 0 {
				t.Errorf("quedan %d versiones del archivo borrado, esperado 0 (cascada)", got)
			}

			for _, name := range []string{"idx_file_versions_file_id", "idx_file_versions_owner_usage"} {
				if !indexExists(name) {
					t.Errorf("falta el índice %s tras la 0012", name)
				}
			}

			// La `down` conserva las filas y quita la columna y su índice.
			before := count(`SELECT COUNT(*) FROM file_versions`)
			if err := m.Migrate(11); err != nil {
				t.Fatalf("revirtiendo la 0012: %v", err)
			}
			if got := count(`SELECT COUNT(*) FROM file_versions`); got != before {
				t.Errorf("filas de versiones tras revertir = %d, esperado %d", got, before)
			}
			if _, err := conn.Query(`SELECT owner_id FROM file_versions`); err == nil {
				t.Error("tras revertir, la columna owner_id no debería existir")
			}
			if indexExists("idx_file_versions_owner_usage") {
				t.Error("tras revertir, el índice de uso de versiones no debería existir")
			}
			if !indexExists("idx_file_versions_file_id") {
				t.Error("tras revertir debe seguir el índice idx_file_versions_file_id")
			}

			// Y vuelve a subir: el relleno se rehace.
			if err := m.Migrate(12); err != nil {
				t.Fatalf("volviendo a aplicar la 0012: %v", err)
			}
			if got := ownerOf("v1"); got != "u1" {
				t.Errorf("propietario de v1 tras volver a subir = %q, esperado u1", got)
			}
		})
	}
}
