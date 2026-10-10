-- Ver migrations/sqlite/0012_version_owner.up.sql -- mismo razonamiento.
-- Se añade la columna, se rellena desde files y solo entonces se declara NOT NULL:
-- si alguna versión no tuviera archivo (no puede: la FK lo impide) la migración
-- fallaría en vez de dejar un propietario vacío.
ALTER TABLE file_versions ADD COLUMN owner_id TEXT;
UPDATE file_versions v SET owner_id = f.owner_id FROM files f WHERE f.id = v.file_id;
ALTER TABLE file_versions ALTER COLUMN owner_id SET NOT NULL;
CREATE INDEX idx_file_versions_owner_usage ON file_versions(owner_id, size_bytes);
