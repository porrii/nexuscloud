-- Tokens de API (§78): a diferencia del token WebDAV (ADR-034, solo HTTP
-- Basic para ese protocolo), estos autentican peticiones Bearer a la API
-- REST completa -- para scripts e integraciones que no deben tener que
-- volver a pasar por el navegador (contraseña + 2FA/passkey) cada vez que
-- expira una sesión normal. Alcance todo-o-nada (ADR-037): el token actúa
-- exactamente como el usuario, sin permisos más finos -- hoy nada en la app
-- los tiene, ni para personas ni en ningún otro sitio del código.
--
-- token_hash guarda SHA-256(token) en hexadecimal, igual que sessions y los
-- tokens WebDAV: el valor en claro solo se muestra una vez, al crearlo, y
-- nunca se persiste. expires_at es NULL = nunca expira (a diferencia del
-- token WebDAV, que ni siquiera tiene este campo; aquí sí, porque §78 lo
-- pide como opción explícita al crear el token).
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    label        TEXT NOT NULL,      -- nombre que le da el usuario, p.ej. "script de backup"
    created_at   TEXT NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT
);
CREATE INDEX idx_api_tokens_user_id ON api_tokens(user_id);
