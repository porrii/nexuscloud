-- Ver migrations/sqlite/0014_favorites.up.sql -- mismo razonamiento.
CREATE TABLE favorites (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_id      TEXT REFERENCES files(id) ON DELETE CASCADE,
    directory_id TEXT REFERENCES directories(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    CHECK ((file_id IS NOT NULL) <> (directory_id IS NOT NULL)),
    UNIQUE (user_id, file_id),
    UNIQUE (user_id, directory_id)
);
CREATE INDEX idx_favorites_user ON favorites(user_id);
CREATE INDEX idx_favorites_file ON favorites(file_id) WHERE file_id IS NOT NULL;
CREATE INDEX idx_favorites_directory ON favorites(directory_id) WHERE directory_id IS NOT NULL;
