-- Ver migrations/sqlite/0011_bigint_sizes.up.sql -- mismo razonamiento.
-- En MySQL INT es de 32 bits con signo (máx. 2 147 483 647): un archivo de más
-- de 2 GiB no se podía registrar en files.size_bytes y una cuota de 100 GiB no
-- cabía en quota_bytes. MODIFY exige repetir la definición completa de la
-- columna, así que se conservan NULL / NOT NULL / DEFAULT tal cual estaban.
-- La tabla de grupos se llama `groups` (palabra reservada desde MySQL 8.0.2,
-- de ahí las comillas invertidas; ver la migración 0001). Cada ALTER reescribe
-- su tabla (files/file_versions son las grandes); ver docs/mantenimiento.md.
ALTER TABLE users         MODIFY quota_bytes           BIGINT NULL;
ALTER TABLE `groups`      MODIFY quota_bytes           BIGINT NULL;
ALTER TABLE files         MODIFY size_bytes            BIGINT NOT NULL;
ALTER TABLE file_versions MODIFY size_bytes            BIGINT NOT NULL;
ALTER TABLE shares        MODIFY max_upload_size_bytes BIGINT NULL;
ALTER TABLE backup_jobs   MODIFY total_bytes           BIGINT NOT NULL DEFAULT 0;

-- Índice de cobertura para el cálculo de uso de cada propietario (ADR-036):
-- SUM(size_bytes) por propietario y estado (activo/papelera) se resuelve solo con
-- el índice, sin leer las filas de files. Medido con 200 000 archivos de un mismo
-- propietario: PostgreSQL 45 -> 23 ms, SQLite 228 -> 92 ms y MySQL 650 -> 73 ms.
-- OJO: el optimizador de MySQL, por sí solo, sigue eligiendo idx_files_owner
-- (650 ms); por eso las consultas de uso (internal/storage/usage.go) lo piden con
-- FORCE INDEX. Se crea DESPUÉS de ensanchar size_bytes para no reconstruirlo.
CREATE INDEX idx_files_owner_usage ON files(owner_id, deleted_at, size_bytes);
