-- Ver migrations/sqlite/0008_webauthn.up.sql -- mismo razonamiento.
-- credential_id a VARCHAR(255): cubre con margen amplio cualquier
-- authenticator real (los IDs típicos rondan 16-64 bytes en crudo, muy
-- por debajo de los ~191 bytes que entran en 255 caracteres base64url);
-- el límite teórico del estándar CTAP2 es 1023 bytes, pero ningún
-- authenticator mainstream se acerca a eso. Como no es la PK sino un
-- UNIQUE VARCHAR normal (igual que token_hash en sessions/invitations),
-- 255*4 bytes en utf8mb4 = 1020 bytes, dentro del límite de índice de
-- InnoDB (3072 bytes con innodb_large_prefix, activo por defecto desde
-- MySQL 5.7.7/MariaDB 10.2.2).
-- idx_webauthn_credentials_user_id se omite a propósito, igual que en
-- sessions: InnoDB ya crea un índice automático para el FK sobre la
-- misma columna.
CREATE TABLE webauthn_credentials (
    id            VARCHAR(36) PRIMARY KEY,
    user_id       VARCHAR(36) NOT NULL,
    credential_id VARCHAR(255) NOT NULL UNIQUE,
    public_key    TEXT NOT NULL,
    sign_count    INT NOT NULL DEFAULT 0,
    label         TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    last_used_at  TEXT,
    CONSTRAINT fk_webauthn_credentials_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
