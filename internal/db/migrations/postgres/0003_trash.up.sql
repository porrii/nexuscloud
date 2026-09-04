-- Ver migrations/sqlite/0003_trash.up.sql -- mismo razonamiento y misma
-- simplificación deliberada (UNIQUE existente sin tocar).
ALTER TABLE files ADD COLUMN deleted_at TEXT;
ALTER TABLE directories ADD COLUMN deleted_at TEXT;

CREATE INDEX idx_files_deleted_at ON files(owner_id, deleted_at);
CREATE INDEX idx_directories_deleted_at ON directories(owner_id, deleted_at);
