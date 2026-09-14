-- Ver migrations/sqlite/0006_pool_policies.up.sql -- mismo razonamiento
-- (políticas por Storage Pool, Fase D). VARCHAR(32) en vez de TEXT porque
-- MySQL/MariaDB no permiten DEFAULT sobre una columna TEXT (ver ADR-031);
-- los valores válidos siguen validándose en Go (internal/storage), no aquí.
ALTER TABLE storage_pools ADD COLUMN utilization_policy VARCHAR(32) NOT NULL DEFAULT 'fill';
ALTER TABLE storage_pools ADD COLUMN backup_policy      VARCHAR(32) NOT NULL DEFAULT 'inherit';
ALTER TABLE storage_pools ADD COLUMN versioning_policy  VARCHAR(32) NOT NULL DEFAULT 'inherit';
ALTER TABLE storage_pools ADD COLUMN snapshot_policy    VARCHAR(32) NOT NULL DEFAULT 'none';
