-- Ver migrations/sqlite/0004_versioning.up.sql -- mismo razonamiento.
-- mime_type/storage_key no tienen DEFAULT ni están indexadas -- se quedan
-- TEXT sin cambios (ver regla de ADR-031).
CREATE TABLE file_versions (
    id           VARCHAR(36) PRIMARY KEY,
    file_id      VARCHAR(36) NOT NULL,
    version_num  INT NOT NULL,
    size_bytes   INT NOT NULL,
    sha256       TEXT NOT NULL,
    mime_type    TEXT NOT NULL,
    storage_key  TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    CONSTRAINT fk_file_versions_file_id FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
    UNIQUE (file_id, version_num)
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE INDEX idx_file_versions_file_id ON file_versions(file_id, version_num DESC);
