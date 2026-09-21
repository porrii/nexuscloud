-- Tamaños en bytes de 64 bits (cuotas, ADR-036). En PostgreSQL y MySQL las
-- columnas de tamaño (users/groups.quota_bytes, files/file_versions.size_bytes,
-- shares.max_upload_size_bytes, backup_jobs.total_bytes) eran de 32 bits -- un
-- máximo de 2 GiB -- y esta migración las ensancha a BIGINT: ver
-- migrations/postgres y migrations/mysql. En SQLite no hay nada que ensanchar:
-- INTEGER es una afinidad dinámica que ya almacena enteros de hasta 64 bits. Se
-- mantiene este fichero para que las tres bases lleven la misma numeración de
-- versiones de esquema; aquí solo se crea el índice de uso.

-- Índice de cobertura para el cálculo de uso de cada propietario (ADR-036):
-- SUM(size_bytes) por propietario y estado (activo/papelera) se resuelve solo con
-- el índice, sin leer las filas de files. Medido con 200 000 archivos de un mismo
-- propietario: PostgreSQL 45 -> 23 ms, SQLite 228 -> 92 ms y MySQL 650 -> 73 ms.
CREATE INDEX idx_files_owner_usage ON files(owner_id, deleted_at, size_bytes);
