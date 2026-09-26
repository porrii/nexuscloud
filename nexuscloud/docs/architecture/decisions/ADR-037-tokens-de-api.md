# ADR-037: Tokens de API

## Estado

Aceptado.

## Contexto

§78 pide tokens de aplicación con nombre, permisos, expiración, fecha de
creación, último uso y revocación; nunca mostrar el token completo tras
crearlo.

Hasta ahora la API REST solo se autenticaba con el token de sesión que crea
el login (`internal/auth/session.go`): exige contraseña + 2FA/passkey cada
vez que expira, no tiene nombre y no se puede crear para un uso concreto (un
script, una integración) sin volver a pasar por el navegador. El único
precedente de "token de propósito específico, revocable, mostrado una vez"
que ya existía era el token de acceso WebDAV
([ADR-034](ADR-034-webdav.md)) — deliberadamente limitado a WebDAV (HTTP
Basic, no abre la API ni la web).

## Decisión

1. **Alcance todo o nada.** El token actúa exactamente como su propietario:
   mismo acceso que su sesión, sin permisos más finos. El campo "Permisos"
   de §78 queda como una nota libre (`label`) para que quien lo crea recuerde
   para qué lo hizo, no como una comprobación real. Es una decisión de
   producto, no una limitación técnica: hoy nada en NexusCloud tiene permisos
   más finos que administrador/usuario normal, ni para personas ni en ningún
   otro sitio del código — construir una taxonomía de scopes y aplicarla a
   los 60+ endpoints de la API habría sido un proyecto en sí mismo, más
   grande que el resto de esta pieza junta. Si en el futuro hace falta acotar
   un token a menos que "todo", es un slice propio.

2. **Calca el token WebDAV, adaptado a Bearer general.** Mismo generador (256
   bits de `idgen.Token()`), mismo `HashToken` (SHA-256 en hex, igual que
   sesiones), mismo criterio de "se muestra una vez, solo se guarda el hash".
   Prefijo propio y exportado, `nat_` (el de WebDAV, `nwd_`, es privado a su
   paquete: aquí el middleware de la API necesita reconocerlo antes de saber
   en qué tabla buscar). A diferencia del token WebDAV, que ni siquiera tiene
   el campo, este SÍ admite expiración opcional (`expires_at`, `NULL` = nunca
   expira) — §78 la pide explícitamente. La revocación es un `DELETE`, no un
   borrado lógico: mismo criterio que `webdav_tokens`, distinto del de
   `sessions` (aquí no hace falta volver a consultar un token ya revocado).

3. **El middleware de autenticación distingue por prefijo, no por tabla
   aparte de rutas.** `RequireAuth` (`internal/api/v1/middleware.go`)
   comprueba si el Bearer empieza por `nat_` antes de intentar
   `ValidateToken` de sesión: si coincide, autentica contra `api_tokens`; si
   no, sigue el camino de siempre. Los tokens de API funcionan en **cualquier**
   endpoint que ya exigiera `RequireAuth` — no hay una lista separada de
   rutas permitidas, porque el alcance es todo-o-nada.

4. **Siempre disponible, sin opción de config que lo desactive** — a
   diferencia de WebDAV (`webdav.enabled`) o WebAuthn
   (`security.webAuthn.enabled`). Es un mecanismo de autenticación general
   para la API, no una feature de producto que tenga sentido apagar.

5. **Esquema (migración `0013_api_tokens`, sqlite + postgres + mysql):**
   `api_tokens(id, user_id → users ON DELETE CASCADE, token_hash UNIQUE,
   label, created_at, expires_at, last_used_at)`. Mismo criterio que
   `webdav_tokens`: MySQL sin índice redundante para la FK, `token_hash
   VARCHAR(64)`.

6. **Gestión:** `POST/GET/DELETE /api/v1/auth/api-tokens` (siempre
   registradas; el revocado comprueba la propiedad en el propio `WHERE`,
   §198), nueva sección "Tokens de API" en `AccountPage` (crea con nombre y
   una expiración discreta —nunca/30 días/90 días/1 año—, lo muestra una
   sola vez, lista y revoca) y CLI `nexuscloud users api-token
   create|list|revoke --username [--label] [--expires-in]` (acepta `30d`,
   `90d`, `1y`, horas/minutos de Go, o `never`/`nunca`).

## Consecuencias

- Nueva tabla `api_tokens`, sin tocar ninguna existente. Las instalaciones
  actuales no cambian de comportamiento hasta que alguien crea su primer
  token.
- `expires_at` no se valida como "en el futuro" al crearlo: un token creado
  ya expirado simplemente nunca autentica. No se consideró un error digno de
  rechazar la petición — es equivalente a crear y revocar en el mismo
  instante.
- Sin límite de tokens activos por usuario (mismo criterio que
  `webdav_tokens`, que tampoco lo tiene) y sin step-up auth (volver a pedir
  la contraseña) para crear o revocar uno: ningún flujo equivalente de hoy
  lo exige.
- El token de WebDAV no se toca: convive sin cambios, sirve un propósito
  distinto (HTTP Basic, solo WebDAV) y ya está en producción.
- **Verificación:** 9 tests en `internal/auth` (creación, expiración pasada
  y futura, prefijo no reconocido, usuario deshabilitado, revocación IDOR,
  listado aislado por usuario), 5 de integración HTTP contra el servidor
  real (crear, usar el propio token como Bearer contra `/users/me`, listar
  sin exponer el valor en claro, IDOR al revocar, expiración, sin sesión),
  6 de la CLI (`create`/`list`/`revoke`, `--expires-in` válido e inválido,
  `--username` obligatorio) y build/lint/tsc de la web.
