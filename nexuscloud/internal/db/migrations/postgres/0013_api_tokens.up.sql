-- Ver migrations/sqlite/0013_api_tokens.up.sql -- mismo razonamiento.
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    label        TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT
);
CREATE INDEX idx_api_tokens_user_id ON api_tokens(user_id);
