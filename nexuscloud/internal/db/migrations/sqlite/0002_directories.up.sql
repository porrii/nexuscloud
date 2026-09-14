-- Registro explícito de carpetas (§13): necesario para poder listar
-- subcarpetas (incluidas las vacías) igual que se listan archivos.
-- Migración separada de 0001 en vez de modificarla, siguiendo la propia
-- regla del proyecto de nunca alterar una migración ya aplicada (§8).
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
