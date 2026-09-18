-- Ver migrations/sqlite/0008_webauthn.up.sql -- mismo razonamiento.
CREATE TABLE webauthn_credentials (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,
    public_key    TEXT NOT NULL,
    sign_count    INTEGER NOT NULL DEFAULT 0,
    label         TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    last_used_at  TEXT
);
CREATE INDEX idx_webauthn_credentials_user_id ON webauthn_credentials(user_id);
