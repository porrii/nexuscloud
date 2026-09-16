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
| GET | `/api/v1/users/me` | sesión | Usuario autenticado |
| GET | `/api/v1/users` | admin | Lista usuarios |
| POST | `/api/v1/users` | admin | Crea un usuario directamente |
| PATCH | `/api/v1/users/{id}` | admin | `{display_name?, email?, status?}` |
| DELETE | `/api/v1/users/{id}` | admin | Elimina un usuario (no a sí mismo) |
| POST | `/api/v1/invitations` | admin | `{role?, max_uses?, ttl_hours?}` → invitación + token (una vez) |
| GET | `/api/v1/invitations` | admin | Lista invitaciones |
| DELETE | `/api/v1/invitations/{id}` | admin | Revoca una invitación |
| POST | `/api/v1/invitations/redeem` | — | `{token, username, password, display_name?, email?}` → crea cuenta (único auto-alta permitido, §21) |
| GET | `/api/v1/files?path=/Documentos` | sesión | `{directories: [...], files: [...]}` en esa ruta lógica (solo activos) |
| POST | `/api/v1/files?name=...&path=...` | sesión | Sube contenido (cuerpo crudo, streaming); 409 si el nombre está ocupado por algo en la papelera |
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
| POST | `/api/v1/groups` | admin | Crea un grupo (`{name}`); 409 si ya existe uno con ese nombre |
| POST | `/api/v1/groups/{id}/members` | admin | Añade un usuario a un grupo (`{user_id}`); 404 si el grupo o el usuario no existen |
| POST | `/api/v1/shares` | sesión | Crea una compartición usuario/grupo/enlace (§37); la respuesta incluye `token` una única vez si es un enlace |
| GET | `/api/v1/shares?direction=by-me\|with-me` | sesión | "Compartido por mí" (por defecto) o "compartido conmigo" |
| DELETE | `/api/v1/shares/{id}` | sesión, propietario | Revoca una compartición (soft, `revoked_at`) |
| GET | `/api/v1/shared-directories/{id}` | sesión | Navega una carpeta a la que se accede vía share, no por propiedad |
| GET | `/api/v1/public/shares/{token}` | — | Metadata de un enlace público; con contraseña, exige `X-Share-Password` para revelar nombre/tamaño |
| GET | `/api/v1/public/shares/{token}/download?path=` | — | Descarga vía enlace (streaming); incrementa el contador de descargas de forma atómica |
| GET | `/api/v1/public/shares/{token}/browse?path=` | — | Lista el contenido de un enlace de carpeta (o una subcarpeta suya) |
| POST | `/api/v1/public/shares/{token}/upload?path=&name=` | — | Sube a un enlace de carpeta con permiso de subida |
| GET | `/api/v1/audit?limit=&offset=` | admin | Eventos de auditoría, paginado |
| GET | `/api/v1/public/client-updates/releases.json` | — | Feed de actualizaciones del cliente de escritorio (Velopack), reenviado desde GitHub Releases; solo si `clientUpdates.enabled=true` (§ADR-032) |
| GET | `/api/v1/public/client-updates/download/{assetName}` | — | Descarga un asset exacto de esa misma release (paquete/instalador); 404 si el nombre no coincide con ningún asset real |

Las rutas marcadas "propietario" comprueban la propiedad del recurso en el propio handler/repositorio, no solo la autenticación — acceder a un archivo ajeno por ID adivinado devuelve `403`, nunca el contenido (§198 IDOR).

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
