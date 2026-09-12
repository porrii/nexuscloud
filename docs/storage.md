# Motor de almacenamiento

## Principio (§9)

La base de datos solo contiene metadatos (ruta, tamaño, hash, propietario, tipo MIME, timestamps). El contenido de los archivos vive siempre en el filesystem, nunca en la base de datos.

## Capas

```
API handlers (internal/api/v1)
        │  valida sesión + pertenencia
        ▼
FileService (internal/storage)     ← único punto de acceso a archivos de usuario
        │  valida nombre/ruta, resuelve pool
        ▼
Provider interface                  ← abstracción (§102)
        │
        ▼
LocalFilesystemProvider             ← única implementación en Fase 1
        │  SafeJoin + escritura atómica
        ▼
Filesystem real
```

Ningún otro paquete debe tocar `Provider` directamente ni construir rutas de archivo a mano (§195-196): todo pasa por `FileService`.

## Storage Pools (§10)

`storage_pools` modela pools con `name`, `type` (`local` en Fase 1), `path`, `priority`, `status`. Al arrancar, `storage.EnsureDefaultPool` crea un pool `default` si no existe ninguno — el repositorio en sí nunca crea pools implícitamente, para que la lógica de arranque sea explícita y auditable.

RAID (§11-12) y detección de discos/SMART siguen pendientes (ver "Qué falta" más abajo): NexusCloud nunca implementará su propio RAID — detectará y mostrará el estado de las tecnologías que ya ofrezca el sistema operativo (mdadm, Storage Spaces, ZFS/Btrfs).

## Identificadores

Los IDs de archivo son UUID v4 aleatorios (`internal/idgen`), nunca incrementales — evita enumeración e IDOR (§123, §198-199). La ruta física en disco (`<owner_id>/<parent_path>/<name>`) aísla a cada propietario incluso a nivel de filesystem.

## Integridad

Cada subida calcula su SHA-256 mientras escribe (un único `io.Copy` con `io.MultiWriter` hacia el fichero y el hasher, sin cargar el contenido completo en memoria, §136-137). La escritura es atómica: fichero temporal + `rename`, así que un fallo a mitad de subida nunca downgradea un archivo existente a un estado corrupto.

## Papelera (§16)

Activada por defecto (`trash.enabled: true`, `trash.retentionDays: 30`). `Delete`/`DeleteDirectory` son soft-delete (columna `deleted_at`, migración 0003) cuando la papelera está activada; con `trash.enabled: false` borran directamente y para siempre, como en la Fase 1.

- Un elemento en la papelera desaparece de `List`/`ListDirectories` pero sigue siendo descargable por su propietario, y aparece en `GET /api/v1/trash`.
- **Simplificación deliberada**: la restricción `UNIQUE(pool_id, owner_id, parent_path, name)` de `files`/`directories` (migración 0001) no se tocó al añadir la papelera — recrearla como índice único parcial (`WHERE deleted_at IS NULL`, lo que permitiría reutilizar un nombre mientras el original sigue en la papelera) exigiría recrear la tabla en SQLite sin poder probarlo aquí contra Postgres real. Por eso, mientras algo ocupa un nombre en la papelera, subir/crear algo nuevo con ese mismo nombre se rechaza (`ErrNameOccupiedByTrash`, HTTP 409) en vez de resucitar/sobrescribir el contenido en papelera en silencio (§128) — el usuario debe restaurar, purgar o usar otro nombre primero.
- **Purga automática**: un ticker en segundo plano (`internal/server.startTrashPurgeLoop`) revisa cada hora (y una vez al arrancar) y borra para siempre cualquier elemento que lleve más de `trash.retentionDays` en la papelera — `FileService.PurgeExpiredTrash`.
- Al mover una carpeta a la papelera se retira también su marcador físico (está garantizado vacío); al restaurarla, `RestoreDirectory` lo recrea.
- Fuera de esta pasada: purga por tamaño máximo de papelera (§16 lo menciona; solo se implementó el límite por días).

## Versionado (§15)

Activado por defecto (`versioning.enabled: true`, `versioning.maxVersionsPerFile: 10`). Subir contenido distinto a un path que ya tiene un archivo activo aparta automáticamente el contenido anterior como una versión del historial en vez de perderlo; el archivo conserva siempre su mismo ID.

- **Flujo de `Upload`**: el contenido entrante se escribe primero a una ubicación provisional (`.nexuscloud-staging/`) para poder calcular su SHA-256 sin tocar todavía el contenido existente. Si hay un archivo activo en ese path y su hash difiere del nuevo (subir contenido idéntico no crea versión, evita ruido), su contenido actual se **mueve** (no se copia) a `.nexuscloud-versions/<file_id>/<version_num>` y se registra en `file_versions` (migración 0004); solo entonces el contenido en staging se mueve a su destino final.
- `GET /api/v1/files/{id}/versions` lista el historial; `GET .../versions/{n}` descarga una versión concreta; `POST .../versions/{n}/restore` la restaura.
- **Restaurar nunca pierde datos**: la versión actualmente vigente pasa, a su vez, a formar parte del historial antes de que la versión antigua ocupe su lugar — ver [ADR-007](architecture/decisions/ADR-007-versioning.md).
- **Límite automático**: al superar `maxVersionsPerFile`, se purga la versión más antigua (contenido + fila) — política de limpieza automática de §15.
- Borrar un archivo para siempre (`?permanent=true`, o purga por retención de la papelera) purga también todo su historial de versiones: no tiene sentido conservarlo sin el archivo al que pertenece.
- Fuera de esta pasada: retención por antigüedad y por espacio total ocupado (§15 los menciona; solo se implementó el límite por número de versiones).

## Compartición (§37)

Tres modos: usuario→usuario, usuario→grupo, y enlaces públicos. Activada por defecto (`sharing.enabled: true`) para usuario/grupo -- no añaden ninguna superficie sin autenticar. Los enlaces públicos, la única superficie de Sharing que no exige sesión, están **desactivados por defecto** (`sharing.publicLinksEnabled: false`, secure-by-default §3/§47, mismo precedente que `web.enabled`).

- **Recurso compartido**: un archivo o una carpeta (nunca ambos), nunca el contenido físico -- ver [ADR-008](architecture/decisions/ADR-008-sharing.md).
- **Compartir una carpeta da acceso a todo su contenido**, incluidas subcarpetas sin share propio: descargar un archivo, o navegar una carpeta, comprueba primero un share directo y, si no lo hay, recorre las carpetas ancestro buscando uno que lo cubra.
- **Permisos**: `can_download`/`can_upload`. Las opciones de permiso solo se exponen para enlaces (§37 las lista bajo "Enlaces"); un share de usuario o grupo siempre es de solo descarga.
- **Enlaces**: token opaco de 256 bits (`idgen.Token()`, mismo generador que sesiones/invitaciones), se muestra en claro una única vez al crearlo; solo se persiste su SHA-256. Opcionalmente: contraseña (Argon2id, mismo formato que `users.password_hash`), fecha de expiración, límite de descargas (incrementado de forma atómica, a prueba de carreras concurrentes), límite de tamaño de subida, nombre personalizado (cosmético, nunca sustituye al token como secreto) y revocación (soft, vía `revoked_at`).
- **Contraseña de enlace**: siempre por cabecera `X-Share-Password`, nunca en la URL -- un enlace sin contraseña funciona como `<a href>` directo; uno con contraseña pasa por la web, que la pide y reintenta con la cabecera.
- `GET /api/v1/public/shares/{token}` (probe de metadata) no revela nombre/tamaño de un enlace con contraseña hasta que la cabecera correcta llega -- solo indica si hace falta contraseña.
- **Rate limit propio** (`security.rateLimit.publicLinkPerMinute`, 20/min por defecto) sobre todas las rutas `/api/v1/public/*`: es la otra superficie, además de login, expuesta a fuerza bruta.
- Fuera de esta pasada: permiso de subida dirigido a un usuario/grupo concreto (solo los enlaces lo soportan); notificaciones por email; §38 (Subida Anónima) sigue totalmente separado y sin implementar.

## Backup Manager (§18)

CLI para lo manual (`nexuscloud backup run|list|restore`), sin endpoint HTTP ni UI web todavía -- mismo orden que Storage Pools en la Fase D. Backup completo (nunca incremental todavía) -- ver [ADR-015](architecture/decisions/ADR-015-backup-manager.md), [ADR-016](architecture/decisions/ADR-016-backup-automatico.md) y [ADR-017](architecture/decisions/ADR-017-retencion-backups.md).

- `backup run [--dest <ruta>] [--pool <id-o-nombre>]...`: respalda todos los pools activos elegibles (o solo los indicados) a `--dest` (por defecto, la carpeta de backups de esta instancia, `cfg.BackupsDir()`). Un pool con `backup_policy=off` queda excluido siempre, incluso si se pide explícitamente por `--pool`; `inherit`/`on` se incluyen.
- Cada backup escribe `<dest>/<job-id>/data/<pool-id>/<owner-id>/<ruta>/<nombre>` (calca el aislamiento físico por propietario que ya usa `FileService`) y un `<dest>/<job-id>/manifest.json` autocontenido con propietario/ruta/nombre/tamaño/SHA-256 de cada fichero -- el manifiesto es la fuente de verdad para restaurar, no la base de datos.
- La papelera nunca se respalda (solo archivos activos). Cada fichero se verifica por SHA-256 mientras se copia; si alguno no coincide, el job entero se marca `failed` sin llegar a escribir `manifest.json` -- nunca queda un backup a medias que parezca completo.
- `backup restore <job-id> --dest <ruta>` extrae los ficheros del backup (re-verificando SHA-256) a una carpeta elegida -- no reinserta en un pool activo ni toca la base de datos. Intenta recuperar todos los ficheros que pueda: uno con bitrot en el propio disco de backup no impide restaurar el resto.
- **Automático** (`backup.enabled: true` + `backup.intervalMinutes`, `false` por defecto): un bucle en segundo plano del propio servidor (`internal/server.startBackupScheduleLoop`, mismo patrón que la purga de papelera) ejecuta el equivalente a `backup run` sin flags cada N minutos. Desactivado por defecto porque copia datos reales, normalmente al mismo disco (§19, sin cumplir la regla 3-2-1 todavía) -- el administrador debe activarlo a propósito. No se ejecuta al arrancar el servidor, solo tras el primer intervalo completo.
- `backup list` muestra el historial (estado/fecha/ficheros/tamaño/destino) leyendo `backup_jobs` (migración `0007`).
- **Retención** (`--keep-last N`/`--keep-days N` en `run`, o `backup.retentionCount`/`backup.retentionDays` para el modo automático; `0` = sin límite cada uno): tras un backup completado con éxito, conserva los backups completados que cumplan CUALQUIERA de las políticas activas en ESE MISMO destino (nunca cuenta jobs `failed` ni afecta a otros destinos) -- p.ej. con las dos activas, un backup reciente por cantidad pero antiguo por días igualmente sobrevive. Poda en mejor esfuerzo: si borrar una carpeta antigua falla, su fila en `backup_jobs` tampoco se borra, para no dejar un puntero a nada -- nunca hace fallar el backup recién completado por esto.

## Qué falta (fases posteriores)

- **RAID** (§12) y **snapshots** (§17): delegado a las tecnologías que ya ofrezca el sistema operativo (mdadm, Storage Spaces, ZFS/Btrfs) -- NexusCloud detecta y muestra su estado, nunca implementa su propio RAID/snapshots.
- **Backup incremental, cifrado y rotación/retención automática** (§18/§173): el Backup Manager de arriba es completo (con modo manual y automático); estas capacidades son slices futuros del mismo Backup Manager.
- **Restaurar directamente a un pool activo** (reinsertando metadatos): el `restore` actual solo extrae a una carpeta elegida.
- **Subida anónima** (§38): activación explícita del admin, foco anti-abuso -- modelo distinto al de un enlace normal de Sharing.
- **Miniaturas/previsualización/búsqueda de contenido** (§33-35): Fase 2.
