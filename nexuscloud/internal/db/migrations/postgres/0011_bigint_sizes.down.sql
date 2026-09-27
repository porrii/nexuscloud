DROP INDEX idx_files_owner_usage;

-- Falla (a propósito, sin perder datos) si alguna fila ya supera los 2 GiB:
-- PostgreSQL rechaza el estrechamiento con "integer out of range".
ALTER TABLE users         ALTER COLUMN quota_bytes           TYPE INTEGER;
ALTER TABLE groups        ALTER COLUMN quota_bytes           TYPE INTEGER;
ALTER TABLE files         ALTER COLUMN size_bytes            TYPE INTEGER;
ALTER TABLE file_versions ALTER COLUMN size_bytes            TYPE INTEGER;
ALTER TABLE shares        ALTER COLUMN max_upload_size_bytes TYPE INTEGER;
ALTER TABLE backup_jobs   ALTER COLUMN total_bytes           TYPE INTEGER;
