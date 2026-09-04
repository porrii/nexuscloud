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
| GET | `/api/v1/files?path=/Documentos` | sesión | Lista archivos en esa ruta lógica |
| POST | `/api/v1/files?name=...&path=...` | sesión | Sube contenido (cuerpo crudo, streaming) |
| GET | `/api/v1/files/{id}` | sesión, propietario | Descarga (streaming, sin soporte `Range` todavía) |
| DELETE | `/api/v1/files/{id}` | sesión, propietario | Elimina contenido + metadatos |
| POST | `/api/v1/directories` | sesión | `{parent_path, name}` → crea carpeta |
| GET | `/api/v1/audit?limit=&offset=` | admin | Eventos de auditoría, paginado |

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
