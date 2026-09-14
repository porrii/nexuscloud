-- Ver migrations/sqlite/0003_trash.up.sql -- mismo razonamiento (papelera
-- vía soft-delete). deleted_at es VARCHAR(32) en vez de TEXT porque queda
-- indexada a continuación (MySQL exige longitud de clave explícita en
-- columnas TEXT, ver ADR-031); 32 basta de sobra para un RFC3339Nano UTC
-- completo (30 caracteres como máximo).
ALTER TABLE files ADD COLUMN deleted_at VARCHAR(32);
ALTER TABLE directories ADD COLUMN deleted_at VARCHAR(32);

CREATE INDEX idx_files_deleted_at ON files(owner_id, deleted_at);
CREATE INDEX idx_directories_deleted_at ON directories(owner_id, deleted_at);
