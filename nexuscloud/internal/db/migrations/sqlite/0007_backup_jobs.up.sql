-- Backup Manager v1 (Fase 5 slice 1, ADR-015): una fila por invocación de
-- "nexuscloud backup run", nunca una fila por fichero -- el detalle por
-- fichero (propietario/ruta/nombre/tamaño/sha256) vive en un manifest.json
-- autocontenido dentro de la propia carpeta de destino, para no multiplicar
-- filas de BD en un backup grande y para que el backup se pueda inspeccionar/
-- restaurar sin depender de que la base de datos siga disponible.
CREATE TABLE backup_jobs (
    id               TEXT PRIMARY KEY,
    status           TEXT NOT NULL,       -- running | completed | failed
    destination_path TEXT NOT NULL,
    pool_ids_json    TEXT NOT NULL,       -- ["pool-a","pool-b"]
    file_count       INTEGER NOT NULL DEFAULT 0,
    total_bytes      INTEGER NOT NULL DEFAULT 0,
    started_at       TEXT NOT NULL,
    finished_at      TEXT,
    error_message    TEXT
);
CREATE INDEX idx_backup_jobs_started_at ON backup_jobs(started_at);
