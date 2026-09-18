-- Ver migrations/sqlite/0009_webauthn_ceremonies.up.sql -- mismo razonamiento.
CREATE TABLE webauthn_ceremonies (
    id           TEXT PRIMARY KEY,
    user_id      TEXT REFERENCES users(id) ON DELETE CASCADE,
    purpose      TEXT NOT NULL,
    session_data TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL
);
CREATE INDEX idx_webauthn_ceremonies_expires_at ON webauthn_ceremonies(expires_at);
