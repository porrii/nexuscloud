-- Esquema inicial de NexusCloud (Fase 1), dialecto MySQL/MariaDB.
-- Ver ADR-031 para el porqué de cada divergencia frente a sqlite/postgres:
-- VARCHAR en vez de TEXT en toda columna PK/FK/indexada/con DEFAULT (MySQL
-- exige longitud de clave explícita en TEXT/BLOB y prohíbe DEFAULT sobre
-- TEXT), FOREIGN KEY explícita en vez de la forma inline "col REFERENCES
-- ..." (MySQL 8 la ignora en silencio), y ENGINE/CHARACTER SET/COLLATE
-- explícitos en cada tabla (evita depender del collation por defecto del
-- servidor, que en MySQL 8/MariaDB 11 es insensible a mayúsculas y acentos).
CREATE TABLE users (
    id            VARCHAR(36) PRIMARY KEY,
    username      VARCHAR(255) NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    email         TEXT,
    password_hash TEXT NOT NULL,
    status        VARCHAR(32) NOT NULL DEFAULT 'active', -- active | disabled
    quota_bytes   INT,                                    -- NULL = usa cuota de grupo/global (§24)
    totp_secret   TEXT,                                   -- NULL = 2FA no habilitado (§25)
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    last_login_at TEXT
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE roles (
    id   VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

INSERT INTO roles (id, name) VALUES
    ('super_admin',   'Super Admin'),
    ('administrator', 'Administrator'),
    ('user',          'User'),
    ('read_only',     'Read Only');

CREATE TABLE user_roles (
    user_id VARCHAR(36) NOT NULL,
    role_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (user_id, role_id),
    CONSTRAINT fk_user_roles_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_user_roles_role_id FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

-- "groups" (comillas invertidas): palabra reservada en MySQL 8+ desde que
-- SQL:2016 introdujo la unidad de ventana ROWS|RANGE|GROUPS -- sin
-- comillas, tanto este CREATE TABLE como la FK de user_groups que la
-- referencia más abajo fallan con "ERROR 1064: ... syntax ... near
-- 'groups'" (confirmado contra un mysql:8 real). Postgres no soporta
-- comillas invertidas como delimitador de identificador (solo comillas
-- dobles), así que este entrecomillado es EXCLUSIVO de este fichero
-- mysql -- sqlite/postgres siguen con "groups" a secas. El código Go que
-- consulta esta tabla (internal/users/sql_repository.go) tiene su propia
-- rama por dialecto para lo mismo, ver groupsTable() ahí.
CREATE TABLE `groups` (
    id          VARCHAR(36) PRIMARY KEY,
    name        VARCHAR(255) NOT NULL UNIQUE,
    quota_bytes INT,
    created_at  TEXT NOT NULL
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE user_groups (
    user_id  VARCHAR(36) NOT NULL,
    group_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (user_id, group_id),
    CONSTRAINT fk_user_groups_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_user_groups_group_id FOREIGN KEY (group_id) REFERENCES `groups`(id) ON DELETE CASCADE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

-- token_hash guarda SHA-256(token) en hexadecimal (§26, §78, §172).
-- idx_sessions_user_id de sqlite/postgres se omite aquí a propósito: InnoDB
-- ya crea un índice automático para fk_sessions_user_id sobre la misma
-- única columna (verificado con SHOW INDEX contra un MySQL real) -- un
-- segundo índice idéntico solo añadiría coste de escritura sin beneficio.
CREATE TABLE sessions (
    id           VARCHAR(36) PRIMARY KEY,
    user_id      VARCHAR(36) NOT NULL,
    token_hash   VARCHAR(64) NOT NULL UNIQUE,
    device       TEXT,
    ip           TEXT,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT,
    CONSTRAINT fk_sessions_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE invitations (
    id         VARCHAR(36) PRIMARY KEY,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    created_by VARCHAR(36) NOT NULL,
    role_id    VARCHAR(36),
    max_uses   INT NOT NULL DEFAULT 1,
    use_count  INT NOT NULL DEFAULT 0,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    used_at    TEXT,
    used_by    VARCHAR(36),
    revoked_at TEXT,
    CONSTRAINT fk_invitations_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_invitations_role_id FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE SET NULL,
    CONSTRAINT fk_invitations_used_by FOREIGN KEY (used_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE storage_pools (
    id         VARCHAR(36) PRIMARY KEY,
    name       VARCHAR(255) NOT NULL UNIQUE,
    type       VARCHAR(32) NOT NULL DEFAULT 'local',
    path       TEXT NOT NULL,
    priority   INT NOT NULL DEFAULT 0,
    status     VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

-- parent_path/name acotados a VARCHAR(400)/VARCHAR(255) para que el índice
-- compuesto siguiente quepa en el límite de InnoDB de 3072 bytes por índice
-- (2*36 + 400 + 255 = 727 caracteres * 4 bytes utf8mb4 en el peor caso =
-- 2908 <= 3072; verificado con un CREATE TABLE real -- con VARCHAR(1024) en
-- parent_path falla con "Specified key was too long"). Limitación nueva
-- frente a sqlite/postgres (que no acotan la longitud en código Go hoy),
-- documentada en ADR-031 y docs/storage.md.
CREATE TABLE files (
    id          VARCHAR(36) PRIMARY KEY,
    pool_id     VARCHAR(36) NOT NULL,
    owner_id    VARCHAR(36) NOT NULL,
    parent_path VARCHAR(400) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    size_bytes  INT NOT NULL,
    sha256      TEXT NOT NULL,
    mime_type   VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    CONSTRAINT fk_files_pool_id FOREIGN KEY (pool_id) REFERENCES storage_pools(id) ON DELETE RESTRICT,
    CONSTRAINT fk_files_owner_id FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE (pool_id, owner_id, parent_path, name)
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE INDEX idx_files_owner ON files(owner_id, parent_path);

CREATE TABLE audit_events (
    id            VARCHAR(36) PRIMARY KEY,
    occurred_at   VARCHAR(32) NOT NULL,
    actor_user_id VARCHAR(36),
    event_type    TEXT NOT NULL,
    target_type   TEXT,
    target_id     TEXT,
    ip            TEXT,
    metadata_json TEXT,
    CONSTRAINT fk_audit_events_actor_user_id FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE INDEX idx_audit_events_occurred_at ON audit_events(occurred_at);
-- idx_audit_events_actor se omite: cubierto por el índice automático de
-- fk_audit_events_actor_user_id (misma columna única).
