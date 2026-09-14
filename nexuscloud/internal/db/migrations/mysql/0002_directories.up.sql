-- Ver migrations/sqlite/0002_directories.up.sql -- mismo esquema, ver
-- ADR-031 para las divergencias de tipos/FK/collation de este dialecto.
CREATE TABLE directories (
    id          VARCHAR(36) PRIMARY KEY,
    pool_id     VARCHAR(36) NOT NULL,
    owner_id    VARCHAR(36) NOT NULL,
    parent_path VARCHAR(400) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    created_at  TEXT NOT NULL,
    CONSTRAINT fk_directories_pool_id FOREIGN KEY (pool_id) REFERENCES storage_pools(id) ON DELETE RESTRICT,
    CONSTRAINT fk_directories_owner_id FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE (pool_id, owner_id, parent_path, name)
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE INDEX idx_directories_owner ON directories(owner_id, parent_path);
