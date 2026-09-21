-- Ver migrations/sqlite/0010_webdav_tokens.up.sql -- mismo razonamiento.
CREATE TABLE webdav_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    label        TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    last_used_at TEXT
);
CREATE INDEX idx_webdav_tokens_user_id ON webdav_tokens(user_id);
