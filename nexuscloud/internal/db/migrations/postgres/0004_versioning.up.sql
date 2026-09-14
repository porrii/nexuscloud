-- Ver migrations/sqlite/0004_versioning.up.sql -- mismo razonamiento.
CREATE TABLE file_versions (
    id           TEXT PRIMARY KEY,
    file_id      TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    version_num  INTEGER NOT NULL,
    size_bytes   INTEGER NOT NULL,
    sha256       TEXT NOT NULL,
    mime_type    TEXT NOT NULL,
    storage_key  TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    UNIQUE (file_id, version_num)
);
CREATE INDEX idx_file_versions_file_id ON file_versions(file_id, version_num DESC);
