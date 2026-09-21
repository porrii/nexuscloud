-- Ver migrations/sqlite/0009_webauthn_ceremonies.up.sql -- mismo razonamiento.
-- idx_webauthn_ceremonies_user_id se omite a propósito, igual que en
-- sessions/webauthn_credentials: InnoDB ya crea un índice automático para
-- el FK sobre la misma columna (aplica igual aunque sea NULLABLE).
CREATE TABLE webauthn_ceremonies (
    id           VARCHAR(36) PRIMARY KEY,
    user_id      VARCHAR(36),
    purpose      TEXT NOT NULL,
    session_data TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   VARCHAR(32) NOT NULL,
    CONSTRAINT fk_webauthn_ceremonies_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE INDEX idx_webauthn_ceremonies_expires_at ON webauthn_ceremonies(expires_at);
