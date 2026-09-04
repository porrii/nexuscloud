-- Ver migrations/sqlite/0005_sharing.up.sql -- mismo razonamiento.
CREATE TABLE shares (
    id                     TEXT PRIMARY KEY,
    owner_id               TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_id                TEXT REFERENCES files(id) ON DELETE CASCADE,
    directory_id           TEXT REFERENCES directories(id) ON DELETE CASCADE,
    share_type             TEXT NOT NULL,
    target_user_id         TEXT REFERENCES users(id) ON DELETE CASCADE,
    target_group_id        TEXT REFERENCES groups(id) ON DELETE CASCADE,
    token_hash             TEXT,
    label                  TEXT NOT NULL DEFAULT '',
    can_download           INTEGER NOT NULL DEFAULT 1,
    can_upload             INTEGER NOT NULL DEFAULT 0,
    password_hash          TEXT,
    expires_at             TEXT,
    max_downloads          INTEGER,
    download_count         INTEGER NOT NULL DEFAULT 0,
    max_upload_size_bytes  INTEGER,
    revoked_at             TEXT,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    CHECK ((file_id IS NOT NULL) <> (directory_id IS NOT NULL))
);
CREATE INDEX idx_shares_owner ON shares(owner_id);
CREATE INDEX idx_shares_target_user ON shares(target_user_id) WHERE target_user_id IS NOT NULL;
CREATE INDEX idx_shares_target_group ON shares(target_group_id) WHERE target_group_id IS NOT NULL;
CREATE UNIQUE INDEX idx_shares_token_hash ON shares(token_hash) WHERE token_hash IS NOT NULL;
CREATE INDEX idx_shares_file ON shares(file_id) WHERE file_id IS NOT NULL;
CREATE INDEX idx_shares_directory ON shares(directory_id) WHERE directory_id IS NOT NULL;
