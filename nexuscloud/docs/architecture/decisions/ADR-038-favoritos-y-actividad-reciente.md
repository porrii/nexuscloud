# ADR-038: Favoritos y actividad reciente

## Estado

Aceptado.

## Contexto

§143 (dashboard de usuario) pide "Favoritos" y "Recientes" junto a Mis
archivos/Compartido conmigo/Compartido por mí/Papelera/Cuota, las cinco
últimas ya implementadas. §87 pide poder marcar archivos y carpetas como
favoritos. §88 ("ACTIVIDAD") lo describe con un ejemplo concreto: *"Ivan
subió: documento.pdf. Hace 5 minutos"* — una acción con verbo, no solo una
fecha de modificación.

No había ningún scaffolding (ni esquema, ni API, ni UI) para ninguna de las
dos cosas.

## Decisión

1. **"Recientes" es el feed de actividad de §88, construido sobre el log de
   auditoría** (`internal/audit`) que ya existe para otros fines (seguridad,
   cumplimiento), no un simple "ordenar por fecha de modificación". Se
   descartó la alternativa más simple (`ORDER BY updated_at`) porque no
   distingue QUÉ pasó (subir de mover, por ejemplo) y el propio ejemplo del
   spec es, literalmente, una acción con verbo.
   - Nuevo `audit.Repository.ListEventsForActor(actorUserID, eventTypes,
     limit, offset)`, con el filtro de tipos decidido por quien llama, no
     hardcodeado en el paquete de auditoría.
   - Tipos que cuentan como "tu actividad" (decisión de contenido, ajustable
     sin tocar esquema): `upload`, `download`, `delete`, `move`,
     `share_create`, `share_revoke`. Quedan fuera login/sesión, cuotas,
     tokens, WebAuthn, administración — no son la clase de actividad que
     describe §88.
   - El texto humano ("Ivan subió documento.pdf, hace 5 minutos") lo
     construye el CLIENTE a partir de `event_type` + `metadata`, no el
     servidor (mismo criterio que el resto de la web, que ya formatea
     fechas/tamaños del lado del cliente). Limitación conocida: los
     handlers de `download`/`delete`/`share_revoke` no siempre guardan el
     nombre del recurso en `metadata` hoy — esos eventos se muestran con una
     descripción genérica ("descargó un archivo") en vez del nombre. Se deja
     así a propósito: enriquecerlos exigiría una consulta extra antes de
     borrar/revocar solo para el registro de auditoría, y no se optimiza
     especulativamente sin una necesidad real.
   - `GET /api/v1/activity`: es un widget de dashboard (límite por defecto
     20, tope 100), no el listado administrativo completo (`GET /audit`).

2. **Favoritos solo sobre el árbol PROPIO del usuario**, nunca sobre lo que
   otros comparten con él — decisión de producto explícita. Evita el caso de
   un favorito que queda apuntando a un recurso ya inaccesible si el
   propietario revoca el acceso compartido después, y no exige decidir cómo
   se vería un favorito dentro de "Compartido conmigo".

3. **Mismo patrón polimórfico que `shares`** (migración 0005): tabla
   `favorites(id, user_id, file_id NULL, directory_id NULL, created_at)` con
   `file_id`/`directory_id` como columnas de recurso separadas (no un par
   `resource_type`/`resource_id` sin FK real) para que `ON DELETE CASCADE`
   limpie favoritos huérfanos automáticamente al borrar un archivo o carpeta
   para siempre, más un `CHECK` que exige exactamente una de las dos y dos
   `UNIQUE(user_id, file_id)`/`UNIQUE(user_id, directory_id)` que evitan
   duplicar el mismo favorito. Migración `0014_favorites`.

4. **Favoritar el mismo recurso dos veces no es un error: es idempotente.**
   El repositorio traduce la violación de `UNIQUE` a devolver el favorito ya
   existente. Un botón de favorito debe poder pulsarse dos veces sin drama,
   sobre todo si dos pestañas o dispositivos favoritan lo mismo casi a la
   vez.

5. **`favorite_id` es un campo aditivo en `GET /files`** (nunca en
   `GET /trash`, `GET /shared-directories/{id}` ni el listado de versiones):
   presente si ese archivo/carpeta está en favoritos, con el ID a usar para
   quitarlo. Mismo criterio que `can_upload` en `sharedListingResponse`
   (ADR-035) — un campo que los clientes que no lo conocen simplemente
   ignoran.

6. **Un favorito sobre algo trasheado se oculta, no se borra.** La fila del
   favorito sigue existiendo (así que reaparece si se restaura); solo
   `ListFavorites` (la página dedicada) lo filtra mientras esté en la
   papelera. Si se borra para siempre, `ON DELETE CASCADE` limpia el
   favorito sin código adicional.

7. **Sin CLI.** Favoritos y actividad son autoservicio personal, sin caso de
   uso administrativo claro — a diferencia de los tokens WebDAV/API
   (ADR-034, ADR-037), que sí necesitaban una vía sin navegador para que un
   administrador pudiera provisionar acceso.

8. **Web: una sola entrada de navegación con pestañas**, no dos — mismo
   patrón que "Compartido conmigo"/"Compartido por mí", ya unificados en una
   sola página (`SharedPage.tsx`) con pestañas en vez de dos `NavLink`
   separados. Nueva `FavoritesPage.tsx` con pestañas "Favoritos"/"Recientes".
   El icono de favorito (☆/★) sigue el mismo criterio ya establecido en toda
   la web para 📁/📄: un emoji Unicode, sin añadir ninguna librería de
   iconos. El tiempo relativo ("hace 5 minutos") usa
   `Intl.RelativeTimeFormat`, nativo del navegador.

## Consecuencias

- Nueva tabla `favorites`, sin tocar ninguna existente. Las instalaciones
  actuales no cambian de comportamiento hasta que alguien marca su primer
  favorito.
- Sin índice nuevo en `audit_events`: el ya existente
  (`idx_audit_events_actor`) basta a la escala esperada de un feed personal
  de actividad; no se optimiza especulativamente (mismo criterio que los
  reintentos del backup remoto, ADR-029). Queda como mejora futura si algún
  día se mide lento.
- Favoritar un recurso ya trasheado no es un error (no-op inofensivo): se
  oculta de la lista hasta que se restaure.
- **Verificación:** 9 tests en `internal/storage` (favoritar archivo/carpeta,
  idempotencia, propiedad ajena rechazada, recurso inexistente, IDOR al
  quitar, ocultación/reaparición con la papelera, aislamiento por usuario,
  cascada al borrar para siempre, sin `WithFavorites` devuelve el error
  esperado), 2 en `internal/audit` (filtro por actor y tipo, lista vacía sin
  tipos), 7 de integración HTTP (CRUD completo, anotación de `GET /files`,
  idempotencia, propiedad ajena, IDOR, actividad aislada por usuario y sin
  eventos de sesión, límite). Migración 0014 (mismo patrón de `CHECK` e índices ya probado por
  `0005_sharing`) verificada aplicando el esquema completo en MySQL 8 y
  PostgreSQL 16 reales, ida y comprobación de idempotencia. Build/lint/tsc
  de la web.
