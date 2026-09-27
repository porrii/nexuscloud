DROP INDEX idx_files_owner_usage ON files;

-- Con MySQL en modo estricto (el predeterminado) falla, sin perder datos, si
-- alguna fila ya supera los 2 GiB: "Out of range value".
ALTER TABLE users         MODIFY quota_bytes           INT NULL;
ALTER TABLE `groups`      MODIFY quota_bytes           INT NULL;
ALTER TABLE files         MODIFY size_bytes            INT NOT NULL;
ALTER TABLE file_versions MODIFY size_bytes            INT NOT NULL;
ALTER TABLE shares        MODIFY max_upload_size_bytes INT NULL;
ALTER TABLE backup_jobs   MODIFY total_bytes           INT NOT NULL DEFAULT 0;
