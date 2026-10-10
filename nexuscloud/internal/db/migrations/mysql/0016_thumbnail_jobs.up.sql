-- Ver migrations/sqlite/0016_thumbnail_jobs.up.sql -- mismo razonamiento.
CREATE TABLE thumbnail_jobs (
    id          VARCHAR(36) PRIMARY KEY,
    file_id     VARCHAR(36) NOT NULL UNIQUE,
    sha256      VARCHAR(64) NOT NULL,
    kind        VARCHAR(16) NOT NULL,
    status      VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts    INT NOT NULL DEFAULT 0,
    last_error  TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    CONSTRAINT fk_thumbnail_jobs_file_id FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE INDEX idx_thumbnail_jobs_status ON thumbnail_jobs(status);
