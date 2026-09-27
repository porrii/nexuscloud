# ADR-040: Búsqueda de metadatos

## Estado

Aceptado.

## Contexto

§33 pide poder filtrar por nombre/ruta, tipo, extensión, usuario, fecha y
tamaño; también pide "preparar la arquitectura para búsqueda de contenido"
sin implementarla, y advierte explícitamente de no indexar automáticamente
todo el contenido privado por razones de privacidad. Es la primera de las
tres piezas del ítem de mayor alcance del backlog restante (§33-35, §132,
§138: búsqueda, previsualización y miniaturas) — las otras dos, de mayor
riesgo (previsualización interpreta contenido de usuario en el navegador;
miniaturas exigiría decodificar/ejecutar procesadores sobre ese contenido en
el servidor), quedan fuera de este ADR.

Todo lo que pide el filtrado de metadatos ya existe: `FileMeta` guarda
`Name`, `ParentPath`, `MimeType`, `SizeBytes` y `CreatedAt`; `Directory`
guarda nombre y ruta. No hacía falta ninguna migración, solo consultarlo.

## Decisión

1. **Solo metadatos, nunca contenido.** `SearchFilters` (`internal/storage/
   search.go`) es un struct abierto (`Query`, `MimeType`, `Ext`, `DateFrom`/
   `DateTo`, `SizeMin`/`SizeMax`) al que un futuro filtro de contenido se
   añadiría como un campo más sin romper ninguna firma existente — esa es
   toda la "preparación de arquitectura" que pide el spec; no se indexa ni
   se lee el interior de ningún archivo.

2. **Coincidencia de nombre/ruta case-insensitive de forma portátil**, vía
   `LOWER(name) LIKE LOWER(?) ESCAPE '\'` (reutilizando el `escapeLikePattern`
   ya existente) en los tres motores, en vez de depender de `ILIKE`
   (específico de PostgreSQL) o del collation de cada tabla en MySQL —
   mismo resultado en SQLite/PostgreSQL/MySQL sin tres implementaciones
   distintas.

3. **Autorización explícita en la firma de cada método**, no un parámetro
   que pueda leerse como "sin filtro" por accidente:
   `SearchFiles(ctx, ownerID string, f SearchFilters)` siempre acota a un
   propietario; `SearchFilesAllOwners(ctx, f SearchFilters, ownerID string)`
   admite `ownerID` vacío (todos) y solo la llama el camino admin. Mismo
   par en `DirectoryRepository`. `FileService.Search` (personal, siempre
   `ownerID = requesterID`) y `FileService.SearchAsAdmin` (recibe ya un
   `ownerID` resuelto — la resolución de `?owner=username` ocurre en el
   handler HTTP, igual que `CreateShare` resuelve `TargetUsername` en
   `share_handlers.go`, nunca dentro de `internal/storage`) son dos
   funciones separadas en vez de una con un booleano `asAdmin` — igual
   criterio que separar rutas y handlers admin del resto en todo el
   proyecto: la autorización se ve en la firma, no en un flag interno.

4. **Activada por defecto** (`search.enabled: true`), a diferencia de
   `sharing.publicLinksEnabled`/`webdav.enabled`/
   `sharing.anonymousUploadEnabled` (que empiezan en `false` por abrir
   superficie sin sesión). Búsqueda es 100% autenticada y de solo lectura
   sobre datos que el usuario ya podía ver por `GET /files` — no añade
   superficie nueva, mismo criterio que `trash.enabled`/`versioning.enabled`.
   Con `search.enabled=false`, `GET /search` y `GET /admin/search` ni se
   registran en el router (404), mismo patrón que `ClientUpdatesProxy`/
   `BackupReceiveToken`.

5. **`GET /api/v1/admin/search`, bajo `RequireAdmin`**, separado del
   endpoint personal en vez de un único endpoint con un parámetro
   "buscar en todo". Mismo shape de respuesta que `GET /files`
   (`{directories, files}`) para reutilizar el mismo componente de listado
   en la web. Sin rate limit ni auditoría propios: mismo criterio que
   `GET /files`/`GET /favorites` (lectura sin efectos secundarios).

6. **Sin índice nuevo.** Usa `idx_files_owner(owner_id, parent_path)` ya
   existente para acotar por propietario; el resto de filtros son un scan
   dentro de ese subconjunto. A la escala objetivo (§160, ~100 usuarios;
   ADR-036 ya validó 200-250k filas por propietario) debería bastar — no se
   optimiza especulativamente, mismo criterio que favoritos/auditoría
   (ADR-038). El modo admin sin filtro de propietario sí escanea toda la
   tabla: límite conocido, aceptable a esta escala.

7. **`is_admin` nuevo en `GET /users/me`** (`meResponse`, envoltorio propio
   de ese handler): la web necesitaba saber si el usuario autenticado es
   admin para decidir si mostrar el interruptor "buscar en toda la
   instancia", y no existía ningún campo de rol en esa respuesta. Se
   reutiliza el `UserSvc.IsAdmin` ya usado por el middleware
   `RequireAdmin`, en vez de tocar `toUserResponse` (evita modificar sus
   otros seis puntos de llamada por un campo que solo necesita `/me`).

8. **Web: una barra de búsqueda en `AppShell.tsx`** (visible desde
   cualquier página, no solo dentro de "Mis archivos": busca en todo el
   árbol, no en la carpeta actual) que navega a `SearchPage.tsx`. Filtros
   en la propia página (tipo/extensión/fecha/tamaño) y, si `user.is_admin`,
   un interruptor extra con selector de usuario que llama a
   `/admin/search`. Reutiliza el patrón de listado en `<ul>` ya usado en
   `FilesPage.tsx`/`FavoritesPage.tsx` en vez de extraer un componente de
   tabla compartido — mismo criterio de preferir duplicación a abstracción
   prematura ya establecido en el resto de la web.

## Consecuencias

- Sin migración nueva, sin tabla nueva: solo dos métodos de lectura más por
  repositorio y dos endpoints. Las instalaciones existentes no cambian de
  comportamiento salvo por las dos rutas nuevas (activas por defecto).
- Búsqueda de contenido (full-text) sigue sin existir; `SearchFilters`
  queda preparado para añadirla el día que se decida indexar contenido,
  con el problema de privacidad que eso implica resuelto aparte, no aquí.
- El modo admin sin filtro de propietario escanea toda la tabla de
  archivos/carpetas de la instancia — límite conocido a la escala objetivo,
  no una regresión de esta fase.
- Previsualización (§35, §138) y Miniaturas (§34, §132, §138) del mismo
  ítem de backlog quedan fuera de este ADR — la primera no toca el
  servidor (el navegador renderiza todo), la segunda sí exige aislar
  procesadores sobre contenido no confiable y tendrá su propio ADR.
- **Verificación:** 18 tests en `internal/storage` (repositorio: cada
  filtro aislado y combinado, case-insensitive, aislamiento entre
  propietarios, `SearchFilesAllOwners` con y sin filtro de propietario;
  servicio: `Search`/`SearchAsAdmin`), 9 de integración HTTP (`is_admin` en
  `/users/me`, búsqueda por nombre en carpetas distintas, por tipo/
  extensión, anotación de `favorite_id`, 401 sin sesión, aislamiento por
  propietario, `/admin/search` cruzando usuarios y exigiendo rol admin,
  filtro por `owner`, 400 en fecha/tamaño malformados, 404 con
  `search.enabled=false`). `LOWER(...) LIKE LOWER(...) ESCAPE '\'`
  confirmado equivalente en SQLite, MySQL 8 y PostgreSQL 16 reales.
  Build/lint/tsc de la web.
