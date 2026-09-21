-- Ver migrations/sqlite/0012_version_owner.up.sql -- mismo razonamiento.
-- VARCHAR(36) igual que files.owner_id. Se añade nullable, se rellena desde files y
-- solo entonces se declara NOT NULL (MODIFY exige repetir la definición completa).
ALTER TABLE file_versions ADD COLUMN owner_id VARCHAR(36) NULL;
UPDATE file_versions v JOIN files f ON f.id = v.file_id SET v.owner_id = f.owner_id;
ALTER TABLE file_versions MODIFY owner_id VARCHAR(36) NOT NULL;
CREATE INDEX idx_file_versions_owner_usage ON file_versions(owner_id, size_bytes);
