-- Estado efímero de una ceremonia WebAuthn (registro o login) entre el
-- Begin y el Finish (go-webauthn/webauthn expone SessionData justo para
-- esto: debe guardarse en el servidor entre ambas llamadas y descartarse
-- al terminar la ceremonia). No es la tabla sessions (esa es para
-- sesiones YA autenticadas, de horas/días) -- esta vive segundos/minutos,
-- de ahí el nombre distinto para no confundirlas.
--
-- user_id es NULLABLE a propósito: en un login passwordless (discoverable)
-- el servidor todavía no sabe qué usuario está entrando -- eso es
-- justamente lo que resuelve el propio ceremony, no algo que se pueda
-- saber de antemano.
--
-- session_data guarda el JSON tal cual lo serializa la librería (id es el
-- handle opaco que va y vuelve al cliente entre el Begin y el Finish,
-- igual que un token de sesión).
CREATE TABLE webauthn_ceremonies (
    id           TEXT PRIMARY KEY,
    user_id      TEXT REFERENCES users(id) ON DELETE CASCADE,
    purpose      TEXT NOT NULL,   -- 'registration' | 'login'
    session_data TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL
);
CREATE INDEX idx_webauthn_ceremonies_expires_at ON webauthn_ceremonies(expires_at);
