-- Subida anónima (§38, ADR-039): enlaces de subida sin ningún acceso al
-- resto del contenido, activados globalmente por el administrador
-- (sharing.anonymousUploadEnabled, desactivado por defecto). Modelo
-- SEPARADO de shares (migración 0005), no una variante con una bandera: el
-- modelo Share actual no garantiza "solo subida, sin ver el resto"
-- (BrowsePublicShare ignora can_download), así que aquí la garantía es
-- estructural -- no existe ningún endpoint de navegación sobre este token,
-- nunca una comprobación que se pueda olvidar.
--
-- directory_id es NOT NULL (a diferencia de favorites, migración 0014, que
-- sí es polimórfico file/directory): un enlace de subida anónima SIEMPRE
-- apunta a una carpeta, nunca a un archivo suelto. token_hash es NOT NULL
-- UNIQUE directo (a diferencia de shares.token_hash, nullable porque solo
-- lo usa uno de sus tres tipos): aquí SIEMPRE hay token, mismo criterio
-- SHA-256 que sessions/shares/tokens WebDAV/API. upload_count es solo
-- informativo para el propietario (igual papel que shares.download_count),
-- no se usa para aplicar ningún límite en esta primera versión.
CREATE TABLE anonymous_uploads (
    id                    TEXT PRIMARY KEY,
    owner_id              TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    directory_id          TEXT NOT NULL REFERENCES directories(id) ON DELETE CASCADE,
    token_hash            TEXT NOT NULL UNIQUE,
    label                 TEXT NOT NULL DEFAULT '',
    max_upload_size_bytes INTEGER,
    expires_at            TEXT,
    revoked_at            TEXT,
    upload_count          INTEGER NOT NULL DEFAULT 0,
    created_at            TEXT NOT NULL
);
CREATE INDEX idx_anonymous_uploads_owner ON anonymous_uploads(owner_id);
