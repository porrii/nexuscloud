-- Ver migrations/sqlite/0007_backup_jobs.up.sql -- mismo razonamiento (una
-- fila por invocación de "nexuscloud backup run", detalle por fichero en
-- manifest.json). Sin columnas de FK en esta tabla; started_at es
-- VARCHAR(32) en vez de TEXT porque queda indexada a continuación.
CREATE TABLE backup_jobs (
    id               VARCHAR(36) PRIMARY KEY,
    status           TEXT NOT NULL,       -- running | completed | failed
    destination_path TEXT NOT NULL,
    pool_ids_json    TEXT NOT NULL,       -- ["pool-a","pool-b"]
    file_count       INT NOT NULL DEFAULT 0,
    total_bytes      INT NOT NULL DEFAULT 0,
    started_at       VARCHAR(32) NOT NULL,
    finished_at      TEXT,
    error_message    TEXT
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE INDEX idx_backup_jobs_started_at ON backup_jobs(started_at);
