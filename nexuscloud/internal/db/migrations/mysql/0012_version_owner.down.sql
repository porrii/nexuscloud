DROP INDEX idx_file_versions_owner_usage ON file_versions;
ALTER TABLE file_versions DROP COLUMN owner_id;
