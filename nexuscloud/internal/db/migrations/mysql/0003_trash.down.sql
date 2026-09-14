-- MySQL exige nombrar la tabla en DROP INDEX (a diferencia de sqlite/
-- postgres, donde "DROP INDEX idx_name" a secas basta) y no soporta
-- "IF EXISTS" en ALTER TABLE DROP INDEX/DROP COLUMN en el suelo de versión
-- soportado (ver ADR-031) -- no hace falta: golang-migrate ya evita
-- reaplicar un down sobre un esquema que no tiene esta migración.
ALTER TABLE directories DROP INDEX idx_directories_deleted_at;
ALTER TABLE files DROP INDEX idx_files_deleted_at;
ALTER TABLE directories DROP COLUMN deleted_at;
ALTER TABLE files DROP COLUMN deleted_at;
