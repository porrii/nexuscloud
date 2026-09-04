-- Ver migrations/sqlite/0002_directories.up.sql — mismo razonamiento y
-- mismo esquema dialecto-neutral (ver ADR-003).
CREATE TABLE directories (
    id          TEXT PRIMARY KEY,
    pool_id     TEXT NOT NULL REFERENCES storage_pools(id) ON DELETE RESTRICT,
    owner_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_path TEXT NOT NULL,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    UNIQUE (pool_id, owner_id, parent_path, name)
);
CREATE INDEX idx_directories_owner ON directories(owner_id, parent_path);
