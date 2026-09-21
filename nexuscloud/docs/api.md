# API REST

Base: `/api/v1`. Versionada (§42, §187): cambios incompatibles llegarán como `/api/v2`, nunca rompiendo `/api/v1` en sitio. `/health` y `/ready` viven fuera de `/api/v1`, en la raíz, y responden con independencia de `api.enabled`.

## Autenticación

Bearer token (recomendado para clientes) o cookie `nexuscloud_session` (`HttpOnly`, `SameSite=Lax`, `Secure` cuando la conexión es TLS) — ambas formas aceptan el mismo token, obtenido de `POST /auth/login`. El token se muestra una única vez en la respuesta de login (§78); solo se persiste su hash.

```
Authorization: Bearer <token>
```

## Formato de error

```json
{"error": {"code": "unauthorized", "message": "Sesión inválida o expirada."}}
```

Los mensajes son siempre genéricos (§170); nunca incluyen detalles internos (SQL, rutas del servidor, trazas).

## Endpoints

| Método | Ruta | Auth | Descripción |
|---|---|---|---|
| GET | `/health` | — | Estado del proceso |
| GET | `/ready` | — | Estado + dependencias (BD) |
| POST | `/api/v1/auth/login` | — | `{username, password, totp_code?}` → token + usuario + sesión |
| POST | `/api/v1/auth/logout` | sesión | Revoca la sesión actual |
| GET | `/api/v1/auth/sessions` | sesión | Lista las sesiones activas del usuario |
| DELETE | `/api/v1/auth/sessions/{id}` | sesión | Revoca una sesión propia (§26) |
| POST | `/api/v1/auth/totp/enroll` | sesión | Genera secreto + `otpauth://` URL |
| POST | `/api/v1/auth/totp/verify` | sesión | `{secret, code}` → activa 2FA si el código coincide |
| POST | `/api/v1/auth/webauthn/register/begin` | sesión | Inicia el alta de un passkey nuevo (§25, ADR-033); devuelve `{ceremony_id, publicKey}` para `navigator.credentials.create()` |
| POST | `/api/v1/auth/webauthn/register/finish?ceremony_id=&label=` | sesión | Cuerpo = respuesta cruda de `create()`; confirma el alta y devuelve el passkey guardado |
| POST | `/api/v1/auth/webauthn/login/begin` | — | Con `{username, password}` (ya verificados) inicia el segundo factor de esa cuenta; sin ellos, login passwordless discoverable |
| POST | `/api/v1/auth/webauthn/login/finish?ceremony_id=&username=` | — | Cuerpo = respuesta cruda de `navigator.credentials.get()`; `username` solo si venía en el begin (segundo factor) |
| GET | `/api/v1/auth/webauthn/credentials` | sesión | Lista los passkeys propios (nunca expone `credential_id`/`public_key`, §172) |
| DELETE | `/api/v1/auth/webauthn/credentials/{id}` | sesión, propietario | Revoca un passkey propio |
| POST | `/api/v1/auth/webdav/tokens` | sesión | `{label?}` → crea un token de acceso WebDAV (§43, ADR-034). La respuesta incluye `token` **una única vez** y `webdav_path` (donde está montado WebDAV); 400 si `label` pasa de 100 caracteres. Solo con `webdav.enabled=true`: con WebDAV desactivado estas tres rutas responden 404 |
| GET | `/api/v1/auth/webdav/tokens` | sesión | Lista los tokens propios: `id`, `label`, `created_at`, `last_used_at` (nunca el token ni su hash, §172) |
| DELETE | `/api/v1/auth/webdav/tokens/{id}` | sesión, propietario | Revoca un token propio (404 si no existe o no es tuyo, §198) |
| GET | `/api/v1/users/me` | sesión | Usuario autenticado |
| GET | `/api/v1/users/me/quota` | sesión | Uso y límite de almacenamiento de quien pregunta (§24, [ADR-036](architecture/decisions/ADR-036-cuotas-de-almacenamiento.md)): `{used_bytes, files_bytes, trash_bytes, versions_bytes, limit_bytes?, source, group_name?}`. El uso es la huella real (archivos + papelera + versiones); `limit_bytes` se omite si no hay límite; `source` es `user`, `group`, `global` o `none`. Endpoint aparte de `/users/me` porque calcula sumas sobre los archivos |
| GET | `/api/v1/users` | admin | Lista usuarios |
| POST | `/api/v1/users` | admin | Crea un usuario directamente; admite `quota_bytes` (cuota propia; ausente o `null` = hereda) |
| PATCH | `/api/v1/users/{id}` | admin | `{display_name?, email?, status?, quota_bytes?}`. `quota_bytes` es tri-estado: ausente = no se toca, `null` = sin cuota propia (hereda del grupo o la global), `0` = ilimitada, un número > 0 = límite en bytes; 400 si es negativo, decimal o no es un número. Cambiarla se audita (`quota_changed`). Las respuestas de usuario incluyen `quota_bytes` si tiene cuota propia |
| DELETE | `/api/v1/users/{id}` | admin | Elimina un usuario (no a sí mismo) |
| POST | `/api/v1/invitations` | admin | `{role?, max_uses?, ttl_hours?}` → invitación + token (una vez) |
| GET | `/api/v1/invitations` | admin | Lista invitaciones |
| DELETE | `/api/v1/invitations/{id}` | admin | Revoca una invitación |
| POST | `/api/v1/invitations/redeem` | — | `{token, username, password, display_name?, email?}` → crea cuenta (único auto-alta permitido, §21) |
| GET | `/api/v1/files?path=/Documentos` | sesión | `{directories: [...], files: [...]}` en esa ruta lógica (solo activos) |
| POST | `/api/v1/files?name=...&path=...` | sesión | Sube contenido (cuerpo crudo, streaming); 409 si el nombre está ocupado por algo en la papelera; `507 quota_exceeded` si no cabe en tu cuota (ver «Cuotas» más abajo) |
| GET | `/api/v1/files/{id}` | sesión, propietario | Descarga (streaming, sin soporte `Range` todavía); funciona también si está en la papelera |
| DELETE | `/api/v1/files/{id}` | sesión, propietario | Mueve a la papelera (§16); `?permanent=true` borra directamente para siempre |
| PATCH | `/api/v1/files/{id}` | sesión, propietario | `{parent_path?, name?}` → mueve y/o renombra de verdad (ADR-030, §85); mismo id, historial de versiones y comparticiones intactos; 409 si el destino ya está ocupado |
| POST | `/api/v1/files/{id}/restore` | sesión, propietario | Saca un archivo de la papelera |
| GET | `/api/v1/files/{id}/versions` | sesión, propietario | Historial de versiones, más reciente primero (§15) |
| GET | `/api/v1/files/{id}/versions/{n}` | sesión, propietario | Descarga el contenido de esa versión concreta |
| POST | `/api/v1/files/{id}/versions/{n}/restore` | sesión, propietario | Restaura esa versión como contenido actual (la actual pasa al historial) |
| POST | `/api/v1/directories` | sesión | `{parent_path, name}` → crea carpeta (idempotente); 409 si el nombre está ocupado por algo en la papelera |
| DELETE | `/api/v1/directories/{id}` | sesión, propietario | Mueve a la papelera una carpeta **vacía** (409 si contiene algo activo); `?permanent=true` borra para siempre |
| PATCH | `/api/v1/directories/{id}` | sesión, propietario | `{parent_path?, name?}` → mueve y/o renombra de verdad, con TODO su árbol de descendientes (ADR-030, §85); rechaza moverla dentro de sí misma o de una subcarpeta suya |
| POST | `/api/v1/directories/{id}/restore` | sesión, propietario | Saca una carpeta de la papelera (recrea su marcador físico) |
| GET | `/api/v1/trash` | sesión | `{directories: [...], files: [...]}` con todo lo eliminado del usuario (vista plana) |
| GET | `/api/v1/groups` | sesión | Lista de grupos (para elegir destino al compartir, §37) |
| POST | `/api/v1/groups` | admin | Crea un grupo (`{name, quota_bytes?}`); 409 si ya existe uno con ese nombre |
| PATCH | `/api/v1/groups/{id}` | admin | `{quota_bytes}` (obligatorio, tri-estado como en los usuarios): fija la cuota **por miembro** del grupo (§24). 404 si el grupo no existe, 400 si el valor no es válido. Se audita (`quota_changed`) |
| POST | `/api/v1/groups/{id}/members` | admin | Añade un usuario a un grupo (`{user_id}`); 404 si el grupo o el usuario no existen |
| POST | `/api/v1/shares` | sesión | Crea una compartición usuario/grupo/enlace (§37); la respuesta incluye `token` una única vez si es un enlace. `can_upload` (solo sobre carpetas) da a un usuario o grupo permiso de «lectura y subida» y exige `can_download` ([ADR-035](architecture/decisions/ADR-035-subida-a-carpeta-compartida.md)); admite `max_upload_size_bytes` |
| GET | `/api/v1/shares?direction=by-me\|with-me` | sesión | "Compartido por mí" (por defecto) o "compartido conmigo" |
| DELETE | `/api/v1/shares/{id}` | sesión, propietario | Revoca una compartición (soft, `revoked_at`) |
| GET | `/api/v1/shared-directories/{id}` | sesión | Navega una carpeta a la que se accede vía share, no por propiedad. Además del listado devuelve `can_upload` y, si hay un límite por archivo, `max_upload_size_bytes` |
| POST | `/api/v1/shared-directories/{id}/files?name=` | sesión | Sube un archivo (cuerpo crudo, en streaming) a una carpeta compartida contigo con permiso de subida, o a una subcarpeta suya. El archivo queda en el árbol del propietario y **no sobrescribe** uno existente. `201` con el archivo; `403 upload_not_allowed` (solo lectura) o `forbidden` (sin acceso), `409 destination_occupied` (también si el nombre lo ocupa algo de la papelera del propietario), `413 upload_too_large`, `507 quota_exceeded` (la cuota que se agota es la del **propietario** de la carpeta, con un mensaje genérico), `404`, `400`. Se audita como `upload` con `via: shared_directory` |
| GET | `/api/v1/public/shares/{token}` | — | Metadata de un enlace público; con contraseña, exige `X-Share-Password` para revelar nombre/tamaño |
| GET | `/api/v1/public/shares/{token}/download?path=` | — | Descarga vía enlace (streaming); incrementa el contador de descargas de forma atómica |
| GET | `/api/v1/public/shares/{token}/browse?path=` | — | Lista el contenido de un enlace de carpeta (o una subcarpeta suya) |
| POST | `/api/v1/public/shares/{token}/upload?path=&name=` | — | Sube a un enlace de carpeta con permiso de subida. No sobrescribe un archivo existente: `409 destination_occupied` (también si el nombre lo ocupa algo de la papelera del propietario). `507 quota_exceeded` si no cabe en la cuota del propietario del enlace (mensaje genérico) |
| GET | `/api/v1/audit?limit=&offset=` | admin | Eventos de auditoría, paginado |
| GET | `/api/v1/public/client-updates/releases.json` | — | Feed de actualizaciones del cliente de escritorio (Velopack), reenviado desde GitHub Releases; solo si `clientUpdates.enabled=true` (§ADR-032) |
| GET | `/api/v1/public/client-updates/download/{assetName}` | — | Descarga un asset exacto de esa misma release (paquete/instalador); 404 si el nombre no coincide con ningún asset real |

Las rutas marcadas "propietario" comprueban la propiedad del recurso en el propio handler/repositorio, no solo la autenticación — acceder a un archivo ajeno por ID adivinado devuelve `403`, nunca el contenido (§198 IDOR).

## Cuotas (507)

Una subida que no cabe en la cuota del propietario de los datos se rechaza con **`507 Insufficient Storage`** y `{"error": {"code": "quota_exceeded", "message": "..."}}` ([ADR-036](architecture/decisions/ADR-036-cuotas-de-almacenamiento.md)); `413 upload_too_large` sigue siendo el límite de tamaño **por archivo**. Vale para `POST /files`, para la subida a una carpeta compartida y a un enlace público, y para el `PUT`/`COPY` de WebDAV.

- **Sin cuota configurada no hay ninguna comprobación**: el comportamiento por defecto no cambia.
- La cuota cuenta la huella real: archivos + papelera + versiones anteriores (`GET /users/me/quota` da el desglose).
- Con `Content-Length` conocido, una subida que ya no cabe se rechaza **sin leer el cuerpo**; sin él, la lectura se corta al pasarse. Nunca queda un archivo a medias.
- En una carpeta compartida o un enlace público la cuota que cuenta es la del **propietario** de la carpeta (los archivos son suyos) y el mensaje es genérico: no revela su uso ni su límite.
- Estando por encima de la cuota (p. ej. porque se bajó) siguen funcionando leer, descargar, mover y borrar: solo se bloquean las subidas nuevas.

## WebDAV (fuera de `/api/v1`)

El protocolo WebDAV en sí no es parte de la API REST: vive en `webdav.path` (por defecto `/webdav/`), con su propia autenticación —HTTP Basic con el usuario y un token de acceso WebDAV, nunca la contraseña de la cuenta— y su propio límite de tasa. Los tokens se gestionan con las tres rutas de arriba. Ver [webdav.md](webdav.md) y [ADR-034](architecture/decisions/ADR-034-webdav.md).

## Ejemplo: subir y descargar un archivo

```bash
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/login \
  -d '{"username":"tu-usuario","password":"tu-contraseña"}' \
  -H 'Content-Type: application/json' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

curl -X POST "localhost:8080/api/v1/files?name=informe.pdf&path=/Documentos" \
  -H "Authorization: Bearer $TOKEN" --data-binary @informe.pdf

curl "localhost:8080/api/v1/files/<id>" -H "Authorization: Bearer $TOKEN" -o informe.pdf
```

## Documentación OpenAPI

El spec formal (§42, §112) vive en [`openapi.yaml`](../openapi.yaml) en la raíz del repositorio, con los esquemas de request/response de cada endpoint. Se mantiene a mano en esta fase (sin codegen) para poder iterar rápido sin arrastrar herramientas adicionales.
