-- Papelera (§16): borrar pasa a ser un soft-delete (deleted_at) en vez de
-- inmediato, con purga automática por retención en internal/storage.
--
-- Simplificación deliberada: la restricción UNIQUE(pool_id, owner_id,
-- parent_path, name) ya existente en files/directories (migración 0001)
-- se mantiene sin cambios, así que mientras un elemento está en la
-- papelera su nombre sigue "ocupado" (no se puede subir/crear otro con el
-- mismo nombre hasta restaurarlo, purgarlo o eliminarlo para siempre).
-- La alternativa correcta -- un índice UNIQUE parcial "WHERE deleted_at
-- IS NULL" que permitiría reutilizar el nombre -- exigiría recrear la
-- tabla en SQLite (no soporta DROP CONSTRAINT) sin poder verificarlo aquí
-- contra una instancia Postgres real; se documenta como mejora futura en
-- vez de arriesgar una migración no probada en ambos dialectos (§162
-- Principio de simplicidad).
ALTER TABLE files ADD COLUMN deleted_at TEXT;
ALTER TABLE directories ADD COLUMN deleted_at TEXT;

CREATE INDEX idx_files_deleted_at ON files(owner_id, deleted_at);
CREATE INDEX idx_directories_deleted_at ON directories(owner_id, deleted_at);
