# Seguridad

Este documento resume el modelo de seguridad implementado en las Fases 1 y 2. Para el análisis de amenazas por actor, ver [threat-model.md](security/threat-model.md); para las decisiones y su porqué, ver los [ADRs](architecture/decisions/).

## Secure by default (§3)

Una instalación recién hecha (`nexuscloud config init` + `nexuscloud admin create-user`) tiene, sin tocar nada más:

- Interfaz web desactivada (`web.enabled: false`)
- Registro público desactivado (`security.publicRegistrationEnabled: false`) — el único alta es vía CLI o invitación
- `CORS` sin orígenes permitidos (lista vacía; nunca `*`, aplicado tanto en `config.Validate` como en tiempo de ejecución)
- Rate limiting activo (login: 5/min; API general: 300/min, configurable)
- Sin credenciales por defecto: `admin create-user` nunca permite una contraseña de menos de 8 caracteres, y no existe ningún usuario `admin/admin` preexistente

## Autenticación

- **Contraseñas**: Argon2id (`golang.org/x/crypto/argon2`), parámetros por defecto 64 MiB / t=3 / p=4 — los mismos que NexusKeys, por consistencia de ecosistema. Los parámetros de coste se guardan junto al hash (formato tipo PHC) para poder endurecerlos sin invalidar contraseñas existentes.
- **Sesiones**: tokens opacos de 256 bits (`crypto/rand`), nunca JWT. Solo se persiste su hash SHA-256; el token en claro se devuelve una única vez en la respuesta de login. Esto hace que "listar sesiones activas" y "cerrar sesión remotamente" (§26) sean triviales — con JWT stateless habría que reconstruir un mecanismo de revocación aparte.
- **TOTP** (RFC 6238) vía `pquerna/otp`. El secreto no se persiste hasta que el usuario confirma un código válido en `/auth/totp/verify`.
- **Passkeys/WebAuthn** (§25, [ADR-033](architecture/decisions/ADR-033-webauthn-passkeys.md)) vía `go-webauthn/webauthn`; desactivado por defecto (`security.webAuthn.enabled: false`), exige `rpID`/`rpOrigin` reales para activarse. Tiene prioridad sobre TOTP como segundo factor si el usuario tiene algún passkey registrado, y el mismo passkey sirve también para login sin contraseña. El registro solo es posible desde la web (exige el navegador); la recuperación de acceso (revocar un passkey) sí está disponible por CLI.
- **Enumeración de usuarios**: `Login` devuelve siempre el mismo error genérico (`unauthorized`) ante usuario inexistente o contraseña incorrecta.

## Autorización

- RBAC con 4 roles semilla (`super_admin`, `administrator`, `user`, `read_only`); `RequireAdmin` comprueba el rol en cada petición contra la base de datos, nunca confía en un claim cacheado.
- **Propiedad de archivos**: cada archivo pertenece a exactamente un usuario. `FileService.Download`/`Delete` comparan `OwnerID` contra el usuario autenticado antes de tocar el filesystem — comprobado también en `SessionRepository.RevokeSession`, que exige `user_id` en el propio `WHERE` de la query (defensa en profundidad contra IDOR, §198, incluso si una capa superior olvidara comprobar la propiedad).
- **Compartición (§37)**: además de la propiedad, un archivo/carpeta es accesible si existe un share activo (usuario, grupo o enlace público) — decisiones en [ADR-008](architecture/decisions/ADR-008-sharing.md), amenazas detalladas en [threat-model.md](security/threat-model.md#enlaces-públicos-de-compartición-§37). Enlaces públicos desactivados por defecto (`sharing.publicLinksEnabled: false`); cuando se activan: token de 256 bits de entropía, contraseña opcional transmitida por cabecera `X-Share-Password` (nunca en la URL), rate limit dedicado (`publicLinkPerMinute`, 20/min por defecto) y metadata (nombre/tamaño) oculta hasta que llega la contraseña correcta.

## Protección de archivos

- **Path traversal (§73)**: `internal/storage.SafeJoin` limpia cualquier ruta anclándola primero a una raíz virtual (`filepath.Clean("/" + ruta)`), lo que neutraliza `../` antes de unirla a la raíz real, más una comprobación de prefijo final como cinturón y tirantes. Se aplica tanto en `FileService` como, de forma independiente, dentro de `LocalFilesystemProvider` — ningún módulo confía ciegamente en que la capa de arriba ya validó.
- **Aislamiento por propietario**: la ruta física en disco es `<owner_id>/<parent_path>/<name>`, así que dos usuarios nunca pueden colisionar ni alcanzar el árbol del otro a través del filesystem, incluso antes de que exista compartición.
- **Escrituras atómicas**: toda subida escribe primero a un fichero temporal en el mismo directorio y hace `rename` al final — un fallo a mitad de subida nunca deja un archivo corrupto visible en su ruta final (§94, §95).
- **Nombres de archivo**: se rechazan caracteres inválidos en Windows, separadores y caracteres de control (§179).

## Superficie HTTP

- **Cabeceras**: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Permissions-Policy` restrictivo; `Strict-Transport-Security` solo cuando la petición ya llegó por TLS (para no romper LAN sin HTTPS, §66).
- **CORS**: allowlist explícita de orígenes; nunca se refleja un origen no listado.
- **Rate limiting**: token bucket por IP (`golang.org/x/time/rate`), límites separados para login y para el resto de la API.
- **IP de cliente consciente de proxies de confianza**: `X-Forwarded-For`/`X-Real-IP` solo se honran si la conexión TCP inmediata proviene de una IP/CIDR en `server.trustedProxies`; en cualquier otro caso se usa la IP de la conexión TCP directa (§50, §118).
- **Errores**: los mensajes al cliente son siempre genéricos (`"No se pudo completar la operación."`); el detalle real (incluida cualquier traza) se registra solo internamente vía `slog` (§170, §172).
- **Health/Ready**: `/health` y `/ready` no revelan información sensible — solo un estado agregado por dependencia (§111).

## Auditoría

`internal/audit` registra login/logout/login fallido/creación y baja de usuarios/subidas/descargas/borrados/invitaciones creadas o revocadas, con actor, IP y metadata estructurada. Un fallo al escribir el log de auditoría se registra como warning pero nunca aborta la operación de negocio que lo originó (una subida de archivo no debe fallar por un problema transitorio del audit log) — pero tampoco se oculta (§94).

## Lo que NO está implementado todavía

- Passkeys/WebAuthn (arquitectura de auth ya preparada para añadirlo sin romper el modelo de sesiones actual)
- Content Security Policy: la Web UI (Fase 2) ya se sirve desde este mismo binario pero todavía sin cabecera `Content-Security-Policy` — gap real, no solo ausencia de superficie
- Escaneo antivirus de subidas (§76) — Fase 5/6
- Cifrado de datos en reposo a nivel de aplicación (§29) — el disco/filesystem subyacente es responsabilidad del administrador en esta fase
