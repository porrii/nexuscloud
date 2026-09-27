-- Ver migrations/sqlite/0015_anonymous_uploads.up.sql -- mismo razonamiento.
-- Mismas divergencias que 0005_sharing/0014_favorites en este dialecto:
-- owner_id/directory_id no llevan índice propio explícito -- InnoDB ya crea
-- uno automático para cada FK de una sola columna --, solo se declara el
-- índice de idx_anonymous_uploads_owner porque aquí SÍ aporta algo distinto
-- (mismo motivo que en favorites: no hay una columna compuesta que ya lo
-- cubra). token_hash VARCHAR(64) UNIQUE igual que sessions/shares/tokens.
CREATE TABLE anonymous_uploads (
    id                    VARCHAR(36) PRIMARY KEY,
    owner_id              VARCHAR(36) NOT NULL,
    directory_id          VARCHAR(36) NOT NULL,
    token_hash            VARCHAR(64) NOT NULL UNIQUE,
    label                 VARCHAR(255) NOT NULL DEFAULT '',
    max_upload_size_bytes BIGINT,
    expires_at            TEXT,
    revoked_at            TEXT,
    upload_count          INT NOT NULL DEFAULT 0,
    created_at            TEXT NOT NULL,
    CONSTRAINT fk_anonymous_uploads_owner_id FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_anonymous_uploads_directory_id FOREIGN KEY (directory_id) REFERENCES directories(id) ON DELETE CASCADE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE INDEX idx_anonymous_uploads_owner ON anonymous_uploads(owner_id);
