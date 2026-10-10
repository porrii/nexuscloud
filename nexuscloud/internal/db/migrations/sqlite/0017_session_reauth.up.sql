-- Reautenticación reforzada «sudo» (§126, ADR-042 Decisión 2): momento en
-- que la sesión volvió a presentar la contraseña (y el TOTP, si lo hay).
-- NULL = nunca. Las acciones administrativas destructivas exigen que hayan
-- pasado menos de 10 minutos. Migración aditiva: las sesiones existentes
-- quedan sin reautenticar, que es justo lo que se quiere.
ALTER TABLE sessions ADD COLUMN reauthenticated_at TEXT;
