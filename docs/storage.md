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

RAID (§11-12) y detección de discos/SMART quedan para la Fase 5: NexusCloud nunca implementará su propio RAID — detectará y mostrará el estado de las tecnologías que ya ofrezca el sistema operativo (mdadm, Storage Spaces, ZFS/Btrfs).

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

## Qué falta (fases posteriores)

- **Snapshots** (§17): delegado al filesystem/SO subyacente (ZFS/Btrfs/Storage Spaces) cuando llegue.
- **Backups** (§18): Backup Manager independiente, Fase 5.
- **Compartición y enlaces públicos** (§37): Fase 2.
- **Miniaturas/previsualización/búsqueda de contenido** (§33-35): Fase 2.
- **Range requests / descargas reanudables** (§41): el endpoint de descarga transmite el contenido completo; soporte de `Range` queda pendiente sin que suponga un cambio de contrato de API cuando se añada.
