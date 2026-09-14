DROP INDEX IF EXISTS idx_directories_deleted_at;
DROP INDEX IF EXISTS idx_files_deleted_at;
ALTER TABLE directories DROP COLUMN deleted_at;
ALTER TABLE files DROP COLUMN deleted_at;
