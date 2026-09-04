-- Esquema inicial de NexusCloud (Fase 1): usuarios, RBAC, grupos, sesiones,
-- invitaciones, storage pools, metadatos de archivos y auditoría.
-- Sharing, papelera, versionado y sync se añaden en migraciones de Fase 2
-- para no mezclar alcance (§163).

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    email         TEXT,
    password_hash TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active', -- active | disabled
    quota_bytes   INTEGER,                        -- NULL = usa cuota de grupo/global (§24)
    totp_secret   TEXT,                            -- NULL = 2FA no habilitado (§25)
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    last_login_at TEXT
);

CREATE TABLE roles (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

INSERT INTO roles (id, name) VALUES
    ('super_admin',   'Super Admin'),
    ('administrator', 'Administrator'),
    ('user',          'User'),
    ('read_only',     'Read Only');

CREATE TABLE user_roles (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE groups (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    quota_bytes INTEGER,
    created_at  TEXT NOT NULL
);

CREATE TABLE user_groups (
    user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);

-- token_hash guarda SHA-256(token); el token en claro solo se devuelve una
-- vez en la respuesta de login y nunca se persiste (§26, §78, §172).
CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    device       TEXT,
    ip           TEXT,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE invitations (
    id         TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    TEXT REFERENCES roles(id) ON DELETE SET NULL,
    max_uses   INTEGER NOT NULL DEFAULT 1,
    use_count  INTEGER NOT NULL DEFAULT 0,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    used_at    TEXT,
    used_by    TEXT REFERENCES users(id) ON DELETE SET NULL,
    revoked_at TEXT
);

CREATE TABLE storage_pools (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL DEFAULT 'local',
    path       TEXT NOT NULL,
    priority   INTEGER NOT NULL DEFAULT 0,
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL
);

-- Solo metadatos: el contenido vive en el filesystem bajo storage_pools.path
-- (§9). parent_path es siempre relativo a la raíz del pool y se valida
-- contra path traversal en internal/storage, nunca aquí.
CREATE TABLE files (
    id          TEXT PRIMARY KEY,
    pool_id     TEXT NOT NULL REFERENCES storage_pools(id) ON DELETE RESTRICT,
    owner_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_path TEXT NOT NULL,
    name        TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    sha256      TEXT NOT NULL,
    mime_type   TEXT NOT NULL DEFAULT 'application/octet-stream',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (pool_id, owner_id, parent_path, name)
);
CREATE INDEX idx_files_owner ON files(owner_id, parent_path);

CREATE TABLE audit_events (
    id            TEXT PRIMARY KEY,
    occurred_at   TEXT NOT NULL,
    actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    event_type    TEXT NOT NULL,
    target_type   TEXT,
    target_id     TEXT,
    ip            TEXT,
    metadata_json TEXT
);
CREATE INDEX idx_audit_events_occurred_at ON audit_events(occurred_at);
CREATE INDEX idx_audit_events_actor ON audit_events(actor_user_id);
