# ADR-035: Subida a una carpeta compartida con un usuario o un grupo

## Estado

Aceptado. Sustituye el punto de alcance de [ADR-008](ADR-008-sharing.md) que dejaba `can_upload` forzado a `false` en los shares de usuario y de grupo.

## Contexto

Hasta ahora solo un enlace público podía llevar permiso de subida. ADR-008 lo dejó fuera de la primera pasada de Sharing porque §37 lista las opciones de permiso bajo «Enlaces», no bajo «Usuarios»/«Grupos», y `CreateShare` forzaba `can_upload=false` para esos dos tipos. El resultado: no existía ningún camino de autorización para que un usuario autenticado que no es el propietario subiera un archivo a una carpeta compartida con él; la única salida era abrir un enlace público de subida, que es justo la superficie sin autenticar que se quiere evitar entre gente que ya tiene cuenta.

El caso de uso es el de siempre: una persona comparte una carpeta («Entregas») con un equipo y cada miembro deja ahí sus archivos, que siguen viviendo en el árbol del propietario.

Las columnas `shares.can_upload` y `shares.max_upload_size_bytes` ya existían y se guardaban para todos los tipos de share, así que **no hace falta migración**.

## Decisión

1. **El permiso es «lectura y subida», no un buzón.** Un share de usuario o de grupo con `can_upload` exige además `can_download` y una carpeta como recurso (`CreateShare` responde `ErrInvalidShare`, 400, si no). Subir sin poder ver lo subido es el «buzón» de solo subida: eso es cosa de los enlaces y de la subida anónima (§38), donde tiene sentido, no de dos personas con cuenta.

2. **Alcance: la carpeta y sus subcarpetas**, con la misma regla de ancestros que ya usan navegar y descargar (ADR-008, punto 4). Un share sobre una carpeta ancestro que esté en la papelera no concede subida, y una carpeta en la papelera no admite subidas (404).

3. **La autorización se re-deriva de la base de datos en cada petición a partir del ID de la carpeta**: `POST /api/v1/shared-directories/{id}/files?name=`. No hay subruta que el cliente pueda manipular para salirse de la carpeta compartida, igual que en `ListSharedDirectory`. El cuerpo va crudo y en streaming, como `POST /files`.

4. **No sobrescribe un archivo existente.** La opción nueva `UploadInput.NoOverwrite` hace que `Upload` falle con `ErrDestinationOccupied` (409 `destination_occupied`) si ya hay un archivo activo con ese nombre, y lo comprueba **antes** de escribir el temporal. Un nombre ocupado por algo de la papelera del propietario (regla del §128) da el mismo 409 `destination_occupied`: la API no distingue, para que quien sube no averigüe qué ha borrado el propietario (el error del núcleo invita a «restaurarlo», algo que solo le corresponde a él). Quien sube no puede renombrar, borrar ni crear subcarpetas: lo que ya hay lo controla el propietario.

5. **Titularidad: el archivo es del propietario de la carpeta.** Se guarda en su árbol (`<propietario>/…`), con su papelera, su versionado y su espacio. Quién lo subió queda **solo en la auditoría**: evento `upload` con el usuario que sube como actor y `via: shared_directory`, `share_id`, `owner_id`, `name` y `size_bytes` en los metadatos. No se añade una columna `uploaded_by`: la auditoría da el rastro sin migración, y si la interfaz llega a necesitar un «subido por» se revisa entonces.

6. **Varios permisos aplicables se unen** (un share directo, uno de grupo y otro de un ancestro pueden coincidir): gana el límite de tamaño **más permisivo**, y «sin límite» (`NULL`) gana a cualquier otro. Ningún permiso recorta a otro. Ese mismo cálculo lo expone `GET /api/v1/shared-directories/{id}` como `can_upload` y `max_upload_size_bytes` (campos nuevos, aditivos) para que la interfaz sepa si ofrecer el botón y qué tamaño anunciar.

7. **Sin interruptor de configuración nuevo.** Lo gobierna `sharing.enabled` (con la compartición desactivada, 403 `sharing_disabled`) y es el propietario quien concede la subida share a share.

Errores de la ruta: 403 `upload_not_allowed` (tiene acceso de lectura pero el share no permite subir), 403 `forbidden` (sin acceso), 413 `upload_too_large`, 409 `destination_occupied` (nombre ocupado, también por algo de la papelera), 404 (carpeta inexistente o en la papelera) y 400 (nombre inválido).

## Consecuencias

- El caso «un equipo entrega archivos en una carpeta» ya no necesita un enlace público. Los enlaces siguen siendo la única vía para quien no tiene cuenta.
- La web ofrece la casilla «Permitir subir archivos a esta carpeta» también en las pestañas Usuario y Grupo del diálogo de compartir, y un botón «Subir archivo» en «Compartido» dentro de las carpetas con permiso. El CLI acepta `shares create --can-upload` con `--share-type user|group`, y `shares list` muestra una columna de permisos.
- Cuando el límite de tamaño corta una subida a medias, el servidor responde 413 sin haber leído el resto del cuerpo, y algunos navegadores muestran entonces un fallo de red en vez del 413. Por eso la web compara el tamaño con `max_upload_size_bytes` antes de enviar nada.
- **Límites conocidos, fuera de esta pasada**: el cliente de escritorio (Flutter) todavía no ofrece subir a carpetas compartidas —servidor, web y CLI sí—; el destinatario no puede crear subcarpetas, renombrar ni borrar; no hay «buzón» de solo subida para usuarios y grupos; y no se guarda quién subió cada archivo más allá de la auditoría.
- **La comprobación de nombre no es atómica.** Protege siempre lo que ya existe, pero dos subidas simultáneas con el mismo nombre *nuevo* pueden cruzarse y la segunda gana, igual que con dos subidas concurrentes por la API. Cerrarlo del todo exigiría reservar el nombre en la base de datos antes de mover el archivo; no compensa a la escala objetivo (§160).
- **Enlaces públicos alineados.** La subida por enlace (`UploadViaPublicShare`) sobrescribía en silencio un archivo existente con el mismo nombre: creaba una versión con el versionado activo y, sin él, destruía el contenido anterior sin dejar rastro. Con el permiso de subida entre personas ya protegido, quedaba como la incoherencia obvia, así que usa también `NoOverwrite` y responde el mismo 409 `destination_occupied` (antes, un nombre ocupado por la papelera daba un 500 genérico en los enlaces). Es un cambio de comportamiento: quien dependiera de que un enlace de subida reemplazara archivos ahora recibe un 409.
- **Un defecto de PostgreSQL destapado por esta verificación**: al probar la consulta nueva contra motores reales, crear cualquier share fallaba en PostgreSQL (`unable to encode true into binary format for int4`), porque el repositorio enlazaba un `bool` de Go a las columnas `INTEGER` `can_download`/`can_upload` y el driver pgx no lo codifica. sqlite y MySQL lo aceptaban, y ninguna prueba anterior creaba shares contra un motor real. Se corrige pasando el 0/1 explícito (`boolToInt`), y `TestMySQLShareFlagsRoundTrip` lo cubre en MySQL 8 y PostgreSQL 16.
