-- Ver migrations/sqlite/0015_anonymous_uploads.up.sql -- mismo razonamiento.
CREATE TABLE anonymous_uploads (
    id                    TEXT PRIMARY KEY,
    owner_id              TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    directory_id          TEXT NOT NULL REFERENCES directories(id) ON DELETE CASCADE,
    token_hash            TEXT NOT NULL UNIQUE,
    label                 TEXT NOT NULL DEFAULT '',
    max_upload_size_bytes INTEGER,
    expires_at            TEXT,
    revoked_at            TEXT,
    upload_count          INTEGER NOT NULL DEFAULT 0,
    created_at            TEXT NOT NULL
);
CREATE INDEX idx_anonymous_uploads_owner ON anonymous_uploads(owner_id);
