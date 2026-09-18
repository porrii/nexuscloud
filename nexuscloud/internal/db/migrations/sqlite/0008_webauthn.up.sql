-- Passkeys/WebAuthn (Fase 6 slice 1, ADR-033): varios credenciales por
-- usuario (portátil, móvil, llave física...), cada uno revocable por
-- separado -- nunca una sola columna en users como el totp_secret actual,
-- porque WebAuthn es de por sí multi-dispositivo.
--
-- id es el identificador interno (idgen.New()), igual que en el resto de
-- tablas -- nunca el credential ID que da el navegador directamente como
-- PK: ese es externo, de longitud variable (hasta 1023 bytes según el
-- propio estándar CTAP2) y no lo genera NexusCloud, así que va en su
-- propia columna credential_id, con UNIQUE para la búsqueda en el login.
-- credential_id/public_key van en base64 como TEXT (mismo criterio que el
-- resto del proyecto para valores binarios, p.ej. password_hash), no BLOB
-- -- no hay ningún otro BLOB en el esquema y no vale la pena introducir
-- el tipo para esto.
CREATE TABLE webauthn_credentials (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,  -- el que da el navegador/authenticator
    public_key    TEXT NOT NULL,         -- clave pública; la privada nunca sale del authenticator
    sign_count    INTEGER NOT NULL DEFAULT 0,  -- protección replay exigida por el estándar WebAuthn
    label         TEXT NOT NULL,      -- nombre que le da el usuario, p.ej. "portátil de trabajo"
    created_at    TEXT NOT NULL,
    last_used_at  TEXT
);
CREATE INDEX idx_webauthn_credentials_user_id ON webauthn_credentials(user_id);
