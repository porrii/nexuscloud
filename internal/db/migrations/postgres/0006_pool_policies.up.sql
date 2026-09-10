-- Ver migrations/sqlite/0006_pool_policies.up.sql -- mismo razonamiento.
ALTER TABLE storage_pools ADD COLUMN utilization_policy TEXT NOT NULL DEFAULT 'fill';
ALTER TABLE storage_pools ADD COLUMN backup_policy      TEXT NOT NULL DEFAULT 'inherit';
ALTER TABLE storage_pools ADD COLUMN versioning_policy  TEXT NOT NULL DEFAULT 'inherit';
ALTER TABLE storage_pools ADD COLUMN snapshot_policy    TEXT NOT NULL DEFAULT 'none';
