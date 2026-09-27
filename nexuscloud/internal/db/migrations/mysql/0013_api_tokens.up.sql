-- Ver migrations/sqlite/0013_api_tokens.up.sql -- mismo razonamiento.
-- token_hash VARCHAR(64) UNIQUE igual que sessions/webdav_tokens (SHA-256 en
-- hex, longitud fija). idx_api_tokens_user_id se omite a propósito, igual
-- que en sessions/webdav_tokens: InnoDB ya crea un índice automático para el
-- FK sobre la misma columna.
CREATE TABLE api_tokens (
    id           VARCHAR(36) PRIMARY KEY,
    user_id      VARCHAR(36) NOT NULL,
    token_hash   VARCHAR(64) NOT NULL UNIQUE,
    label        TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT,
    CONSTRAINT fk_api_tokens_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
