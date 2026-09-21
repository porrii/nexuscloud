-- Tokens de acceso WebDAV (Fase 6 slice 2, ADR-034). Los clientes WebDAV
-- (Explorador de Windows, Finder, rclone, davfs2...) solo saben HTTP Basic:
-- no pueden completar un TOTP ni un passkey. Aceptar la contraseña de la
-- cuenta por Basic dejaría a cualquiera que la conozca leyendo y borrando
-- todos los ficheros SIN pasar por el segundo factor, y la enviaría en cada
-- PROPFIND. Por eso WebDAV solo acepta estos tokens: 256 bits aleatorios,
-- revocables uno a uno (un token por dispositivo) y válidos únicamente para
-- WebDAV -- filtrar uno no compromete la cuenta.
--
-- token_hash guarda SHA-256(token) en hexadecimal, igual que sessions: el
-- token en claro solo se muestra una vez, al crearlo, y nunca se persiste.
CREATE TABLE webdav_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    label        TEXT NOT NULL,      -- nombre que le da el usuario, p.ej. "portátil de casa"
    created_at   TEXT NOT NULL,
    last_used_at TEXT
);
CREATE INDEX idx_webdav_tokens_user_id ON webdav_tokens(user_id);
