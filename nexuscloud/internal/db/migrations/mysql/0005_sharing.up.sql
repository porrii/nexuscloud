-- Ver migrations/sqlite/0005_sharing.up.sql -- mismo razonamiento de
-- negocio (usuario->usuario, usuario->grupo, enlaces públicos). Divergencias
-- de este dialecto (ADR-031):
--   - Los 4 índices "WHERE ... IS NOT NULL" de sqlite/postgres se convierten
--     en índices completos: MySQL/MariaDB no soportan índices parciales. La
--     unicidad de token_hash con múltiples NULL se preserva igual (ambos
--     motores permiten varios NULL en un índice UNIQUE); solo se pierde la
--     optimización de tamaño del índice.
--   - El CHECK se mantiene tal cual: MySQL 8.0.16+/MariaDB 10.2.1+ (suelo
--     de versión ya exigido por esto) lo aplican de verdad, no solo lo
--     parsean -- verificado contra mysql:8 y mariadb:11 reales.
--   - idx_shares_owner/target_user/target_group/file/directory de sqlite/
--     postgres se omiten: cada uno coincide 1:1 con el índice automático
--     que InnoDB crea para su FOREIGN KEY de una sola columna. Solo se
--     mantiene idx_shares_token_hash (no es columna de FK).
CREATE TABLE shares (
    id                     VARCHAR(36) PRIMARY KEY,
    owner_id               VARCHAR(36) NOT NULL,
    file_id                VARCHAR(36),
    directory_id           VARCHAR(36),
    share_type             TEXT NOT NULL,   -- 'user' | 'group' | 'link'
    target_user_id         VARCHAR(36),
    target_group_id        VARCHAR(36),
    token_hash             VARCHAR(64),     -- solo share_type='link'; SHA-256 hex del token
    label                  VARCHAR(255) NOT NULL DEFAULT '', -- "nombre personalizado" (§37)
    can_download           INT NOT NULL DEFAULT 1,
    can_upload             INT NOT NULL DEFAULT 0,
    password_hash          TEXT,            -- mismo formato que users.password_hash ($argon2id$...)
    expires_at             TEXT,
    max_downloads          INT,
    download_count         INT NOT NULL DEFAULT 0,
    max_upload_size_bytes  INT,
    revoked_at             TEXT,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    CONSTRAINT fk_shares_owner_id FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_shares_file_id FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
    CONSTRAINT fk_shares_directory_id FOREIGN KEY (directory_id) REFERENCES directories(id) ON DELETE CASCADE,
    CONSTRAINT fk_shares_target_user_id FOREIGN KEY (target_user_id) REFERENCES users(id) ON DELETE CASCADE,
    -- "groups" (comillas invertidas): palabra reservada en MySQL 8+, ver la
    -- nota en 0001_initial.up.sql donde se crea esta tabla.
    CONSTRAINT fk_shares_target_group_id FOREIGN KEY (target_group_id) REFERENCES `groups`(id) ON DELETE CASCADE,
    CONSTRAINT chk_shares_file_xor_directory CHECK ((file_id IS NOT NULL) <> (directory_id IS NOT NULL))
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE UNIQUE INDEX idx_shares_token_hash ON shares(token_hash);
