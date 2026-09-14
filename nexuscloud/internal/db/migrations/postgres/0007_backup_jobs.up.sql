-- Ver migrations/sqlite/0007_backup_jobs.up.sql -- mismo razonamiento.
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
