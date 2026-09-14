-- Políticas por Storage Pool (Fase D del plan de almacenamiento,
-- docs/architecture/storage-phase-d-plan.md). Solo se añaden columnas con
-- DEFAULT = comportamiento actual, así que es una migración no destructiva
-- y los pools existentes quedan con valores coherentes sin migración de
-- datos. Los valores válidos de cada política se validan en Go
-- (internal/storage), no aquí.
--
--   utilization_policy: fill | round-robin | manual   (en la Fase D solo se
--     honra "fill": los ficheros nuevos van al pool activo de mayor
--     prioridad; el balanceo real llega en una fase posterior)
--   backup_policy / versioning_policy: inherit | on | off
--   snapshot_policy: none   (único soportado por ahora)
ALTER TABLE storage_pools ADD COLUMN utilization_policy TEXT NOT NULL DEFAULT 'fill';
ALTER TABLE storage_pools ADD COLUMN backup_policy      TEXT NOT NULL DEFAULT 'inherit';
ALTER TABLE storage_pools ADD COLUMN versioning_policy  TEXT NOT NULL DEFAULT 'inherit';
ALTER TABLE storage_pools ADD COLUMN snapshot_policy    TEXT NOT NULL DEFAULT 'none';
