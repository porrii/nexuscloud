# ADR-033: Passkeys / WebAuthn

## Estado

Aceptado.

## Contexto

§25 exige, como mínimo, usuario+contraseña, sesiones y TOTP (ya
implementado) y "preparar arquitectura para Passkeys/WebAuthn/FIDO2".
Fase 6 (§163) agrupa esto con WebDAV bajo "seguridad avanzada" -- se
abordan como dos slices separados (mismo criterio que Fase 3 y Fase 5),
este ADR cubre solo el primero: Passkeys/WebAuthn. WebDAV queda como el
slice siguiente.

Un passkey de WebAuthn sirve para dos cosas a la vez, con la misma
ceremonia criptográfica de fondo: alternativa a TOTP como segundo factor
(más fuerte, resistente a phishing) y login sin contraseña
(passwordless discoverable). Se decidió cubrir ambos usos con el mismo
credential registrado, no dos tipos de credencial distintos.

## Decisión

1. **Librería: `github.com/go-webauthn/webauthn`**, el estándar de facto
   en Go para WebAuthn/FIDO2 del lado servidor (MIT, mantenida
   activamente). Reimplementar attestation/assertion a mano sería
   reinventar criptografía de seguridad -- el riesgo no compensa.
   **Fijada en `v0.17.4`, no `@latest` (`v0.18.x`)**: la última versión
   exige `go 1.26.0` en su propio `go.mod`, y arrastraría esa exigencia
   al `go.mod` de NexusCloud -- CI sigue fijado a Go 1.25 (mismo problema
   ya evitado antes con `govulncheck@latest`, que exige 1.26 igual).

2. **Esquema (migraciones `0008`/`0009`, sqlite+postgres+mysql):**
   - `webauthn_credentials`: varios passkeys por usuario (portátil,
     móvil, llave física...), cada uno revocable por separado -- nunca
     una columna en `users` como el `totp_secret` actual, porque
     WebAuthn es de por sí multi-dispositivo. `id` es el identificador
     interno (`idgen.New()`, igual que el resto de tablas); el credential
     ID que da el navegador/authenticator es externo y de longitud
     variable (hasta 1023 bytes según CTAP2), así que vive en su propia
     columna `credential_id` (`UNIQUE`, para la búsqueda en el login) --
     nunca como PK directamente. `public_key`/`credential_id` en base64
     como `TEXT` (mismo criterio que el resto del proyecto para valores
     binarios, p.ej. `password_hash`), no `BLOB`.
   - `webauthn_ceremonies`: estado efímero (segundos/minutos, nunca
     horas) entre el "begin" y el "finish" de cada ceremonia -- la propia
     librería expone `SessionData` justo para esto, y debe guardarse en
     el servidor entre ambas llamadas. No reutiliza la tabla `sessions`
     (esa es para sesiones YA autenticadas, de horas/días) -- nombre
     distinto a propósito para no confundirlas. `user_id` es NULLABLE:
     en un login discoverable el servidor todavía no sabe qué usuario es
     hasta que el propio ceremony lo resuelve.
   - MySQL omite índices explícitos para las FK (`user_id`): InnoDB ya
     crea uno automático para el propio FK sobre la misma columna, mismo
     criterio ya establecido en `sessions`.

3. **Registro con `ResidentKeyRequirement: Required`** siempre, no
   `Preferred`: para que el mismo passkey registrado sirva también para
   login passwordless (discoverable) más adelante, no solo como segundo
   factor. Sin esto, un credential no-resident no aparecería nunca en un
   login discoverable.

4. **`Authenticator.Login` exige WebAuthn con prioridad sobre TOTP** si
   el usuario tiene algún passkey registrado (`ErrWebAuthnRequired`, un
   `sql`-level check vía `WebAuthnCredentialRepository`, inyectado como
   dependencia opcional/nil-safe). Sin esto, registrar un passkey como
   "segundo factor" no habría protegido nada de verdad: `Login` nunca
   habría llegado a comprobarlo, y contraseña+passkey se habría reducido
   a solo-contraseña en la práctica. A diferencia de TOTP (que llega en
   la misma llamada de login, un código que el usuario ya tiene de
   antemano), WebAuthn exige un reto del servidor primero -- no se puede
   resolver en la misma llamada, así que el cliente debe repetir la
   verificación de contraseña contra `/auth/webauthn/login/begin`
   (`Authenticator.VerifyPassword`, extraído para no duplicar esa lógica
   de seguridad en dos sitios) para obtener el reto.

5. **Endpoints `/api/v1/auth/webauthn/login/{begin,finish}` cubren los
   dos modos con la misma pareja de rutas**, no cuatro endpoints
   separados: con `username`+`password` (ya verificados, sin completar
   sesión todavía) es el segundo factor de esa cuenta; sin ellos es login
   passwordless discoverable, el navegador decide qué passkey ofrecer.
   `register/{begin,finish}` y `credentials` (`GET`/`DELETE`) exigen
   sesión ya iniciada. Con `security.webAuthn.enabled=false` (por
   defecto), `NewRouter` ni registra estas rutas -- secure by default,
   mismo criterio que `ClientUpdatesProxy`/`BackupReceiveToken`.

6. **CLI: solo `nexuscloud users webauthn list/revoke --username`**, sin
   comando de alta. Registrar un passkey exige `navigator.credentials.
   create()` del navegador -- no tiene sentido ni es posible por CLI, a
   diferencia de TOTP (que sí podría enrolarse por CLI en teoría, aunque
   hoy tampoco tiene UI web de alta). list/revoke existen para recuperar
   acceso si un usuario se queda bloqueado, mismo motivo que
   `users totp disable`.

7. **Frontend sin librería nueva**: `PublicKeyCredential.
   parseCreationOptionsFromJSON`/`parseRequestOptionsFromJSON` y
   `credential.toJSON()` son parte del propio estándar WebAuthn L3, ya
   disponibles en los navegadores modernos que hacen falta para que
   WebAuthn funcione de todas formas (confirmado: TypeScript 6.0 + su
   `lib.dom.d.ts` ya los tipan). Añadir `@simplewebauthn/browser` solo
   para esto sería una dependencia redundante.

## Consecuencias

- Nuevo `SecurityConfig.WebAuthn` (`enabled`/`rpID`/`rpOrigin`),
  desactivado por defecto: sin un RPID/RPOrigin reales el navegador
  rechaza cualquier reto, así que activarlo a ciegas con valores vacíos
  rompería el protocolo en vez de simplemente no hacer nada.
  `config.Validate` exige ambos no vacíos y que `rpOrigin` use `https://`
  (o `http://localhost` en desarrollo) cuando `enabled=true`.
- `AccountPage.tsx` gana una sección "Passkeys" (listar/añadir/revocar);
  se oculta sola si el backend devuelve 404 en `/auth/webauthn/
  credentials` (es decir, si `security.webAuthn.enabled=false`) -- no
  hace falta ningún flag nuevo en el frontend para eso.
  `LoginPage.tsx` gana un botón de login passwordless siempre visible
  más la continuación de segundo factor cuando el login normal devuelve
  `webauthn_required`.
- Nuevos eventos de auditoría `webauthn_credential_registered`/
  `webauthn_credential_revoked`; los intentos de login con passkey
  reutilizan `login`/`login_failed`, igual que TOTP, en vez de
  fragmentar la taxonomía por mecanismo.
- Verificado con tests reales en cada capa: repositorios + servicio
  (10 tests, sqlite) más un test de integración nuevo en `internal/auth`
  (no existía ninguno hasta ahora) contra MySQL y Postgres reales vía
  Docker, confirmando que `Rebind` (`?` → `$1, $2...`) funciona con las
  consultas nuevas; 7 tests de integración HTTP reales (rutas ausentes
  con WebAuthn desactivado, 401 sin sesión, IDOR en credentials, y sobre
  todo que `Login` exige de verdad el passkey cuando existe uno); 2 tests
  de CLI. `govulncheck` (misma versión que CI): 0 vulnerabilidades
  alcanzables desde la dependencia nueva.
- **Verificación E2E real completada, con un authenticator virtual de
  verdad, no solo la interfaz alrededor.** Las herramientas de
  automatización de navegador disponibles en esta sesión operan a nivel
  de extensión/página y no dan acceso al dominio `WebAuthn` del
  protocolo CDP de Chrome, así que se escribió un programa Go
  independiente (`chromedp` + `chromedp/cdproto/webauthn`, fuera del
  módulo del proyecto -- herramienta de verificación puntual, no una
  dependencia de NexusCloud) que: activa `WebAuthn.enable` +
  `WebAuthn.addVirtualAuthenticator` (ctap2, resident key, user
  verification, presencia automática) contra una instancia real de
  Chrome headless; inicia sesión normal con usuario+contraseña contra el
  servidor real; navega a Cuenta y registra un passkey de verdad
  (`navigator.credentials.create()` real, interceptado por el
  authenticator virtual, `FinishRegistration` real contra el backend);
  cierra sesión; y entra de nuevo con login passwordless
  (`navigator.credentials.get()` real, `FinishDiscoverableLogin` real).
  Las 3 ceremonias completaron con éxito. Confirmado además en los
  propios registros del servidor, no solo en la salida del script: la
  credencial quedó persistida de verdad
  (`users webauthn list --username admin` la muestra, con
  `last_used_at` ya puesto por el login posterior) y la auditoría real
  registró la secuencia completa (`login` → `webauthn_credential_
  registered` → `logout` → `login` con `method: webauthn`). Limpieza
  completa después (servidores parados, directorio de datos temporal
  borrado).
