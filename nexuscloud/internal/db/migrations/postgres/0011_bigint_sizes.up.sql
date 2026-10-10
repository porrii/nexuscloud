-- Ver migrations/sqlite/0011_bigint_sizes.up.sql -- mismo razonamiento.
-- En PostgreSQL INTEGER es de 32 bits (máx. 2 147 483 647): un archivo de más
-- de 2 GiB no se podía registrar en files.size_bytes y una cuota de 100 GiB no
-- cabía en quota_bytes. Cada ALTER reescribe su tabla (files/file_versions son
-- las grandes) con un bloqueo exclusivo, lo que en una instalación con muchos
-- millones de archivos puede tardar; ver docs/mantenimiento.md.
ALTER TABLE users         ALTER COLUMN quota_bytes           TYPE BIGINT;
ALTER TABLE groups        ALTER COLUMN quota_bytes           TYPE BIGINT;
ALTER TABLE files         ALTER COLUMN size_bytes            TYPE BIGINT;
ALTER TABLE file_versions ALTER COLUMN size_bytes            TYPE BIGINT;
ALTER TABLE shares        ALTER COLUMN max_upload_size_bytes TYPE BIGINT;
ALTER TABLE backup_jobs   ALTER COLUMN total_bytes           TYPE BIGINT;

-- Índice de cobertura para el cálculo de uso de cada propietario (ADR-036):
-- SUM(size_bytes) por propietario y estado (activo/papelera) se resuelve solo con
-- el índice, sin leer las filas de files. Medido con 200 000 archivos de un mismo
-- propietario: PostgreSQL 45 -> 23 ms, SQLite 228 -> 92 ms y MySQL 650 -> 73 ms.
-- Se crea DESPUÉS de ensanchar size_bytes para no reconstruirlo en el ALTER.
CREATE INDEX idx_files_owner_usage ON files(owner_id, deleted_at, size_bytes);
