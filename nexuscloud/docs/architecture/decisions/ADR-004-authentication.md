# ADR-004: Argon2id + sesiones opacas server-side (no JWT), TOTP ahora, WebAuthn preparado

## Estado

Aceptado.

## Contexto

§25 exige hashing moderno de contraseñas y prepara la arquitectura para Passkeys/WebAuthn sin exigirlos en la primera versión. §26 exige listar y revocar sesiones remotamente — "logout remoto" real. §78 exige que los tokens (de sesión o de API) nunca se muestren completos después de crearse.

Alternativa considerada para sesiones: **JWT firmados, stateless**. Se descartó porque "logout remoto"/"revocar una sesión concreta" con JWT stateless exige mantener una lista de revocación de todos modos (o aceptar tokens válidos hasta su expiración incluso tras un "logout") — en la práctica, reintroduce el mismo estado server-side que se pretendía evitar, pero con la complejidad añadida de gestionar claves de firma/rotación sin ganar nada a cambio en un sistema que ya tiene base de datos.

## Decisión

1. **Argon2id** (`golang.org/x/crypto/argon2`) para contraseñas, parámetros por defecto 64 MiB / t=3 / p=4 — iguales a los ya validados y documentados en NexusKeys (vault de contraseñas del mismo ecosistema), por consistencia. Los parámetros se codifican junto al hash (formato tipo PHC) para poder endurecerse en el futuro sin invalidar contraseñas existentes.
2. **Sesiones**: tokens opacos aleatorios de 256 bits (`crypto/rand`), usados indistintamente como cookie `HttpOnly` (web futura) o cabecera `Authorization: Bearer` (API/clientes). Solo se persiste `SHA-256(token)`; el valor en claro se devuelve una única vez en la respuesta de login.
3. **TOTP** (RFC 6238) vía `pquerna/otp`, implementado ya en Fase 1: enrolamiento genera un secreto que no se persiste hasta que el usuario confirma un código válido.
4. **WebAuthn/Passkeys**: no implementado en Fase 1 (§115 lo permite explícitamente), pero el modelo de sesiones no depende de cómo se obtuvo la sesión inicial — añadir un segundo método de login en el futuro no debería requerir cambiar `Authenticator.ValidateToken` ni el resto de la API.

## Consecuencias

- `SessionRepository.ListSessionsForUser`/`RevokeSession` son operaciones de base de datos triviales — no hay que mantener una blacklist paralela ni gestionar rotación de claves de firma.
- `RevokeSession` exige explícitamente `user_id` en su cláusula `WHERE`, no solo el ID de sesión: defensa en profundidad contra IDOR incluso si una capa superior olvidara comprobar la propiedad (§198).
- Cada sesión válida implica una consulta a base de datos en cada petición autenticada (a diferencia de JWT, verificable sin I/O) — aceptable a la escala objetivo (~100 usuarios) y coherente con priorizar seguridad/simplicidad sobre rendimiento prematuro (§133, prioridad §2).
- `Login` devuelve siempre el mismo error genérico ante usuario inexistente o contraseña incorrecta, evitando enumeración de usuarios por temporización o mensaje (§27).
