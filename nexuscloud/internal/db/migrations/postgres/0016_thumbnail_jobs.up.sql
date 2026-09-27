-- Ver migrations/sqlite/0016_thumbnail_jobs.up.sql -- mismo razonamiento.
CREATE TABLE thumbnail_jobs (
    id          TEXT PRIMARY KEY,
    file_id     TEXT NOT NULL UNIQUE REFERENCES files(id) ON DELETE CASCADE,
    sha256      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    attempts    INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE INDEX idx_thumbnail_jobs_status ON thumbnail_jobs(status);
