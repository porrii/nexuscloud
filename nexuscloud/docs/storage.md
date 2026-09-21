# Motor de almacenamiento

## Principio (§9)

La base de datos solo contiene metadatos (ruta, tamaño, hash, propietario, tipo MIME, timestamps). El contenido de los archivos vive siempre en el filesystem, nunca en la base de datos.

## Base de datos (§8)

Tres dialectos seleccionables por `database.driver` sin tocar código
(`sqlite`/`postgres`/`mysql`): SQLite (`modernc.org/sqlite`, por defecto,
instalaciones pequeñas), PostgreSQL (`jackc/pgx/v5`, instalaciones
mayores) y MySQL/MariaDB (`go-sql-driver/mysql`, ADR-031). Ver
[ADR-003](architecture/decisions/ADR-003-database.md) para el diseño base
(sin ORM, SQL a mano, `Rebind` para postgres) y
[ADR-031](architecture/decisions/ADR-031-mysql-mariadb.md) para las
divergencias reales que MySQL/MariaDB exigieron -- ADR-003 asumía que no
haría falta ninguna, y esa suposición resultó incorrecta:

- Migraciones con `VARCHAR` explícito (no `TEXT`) en toda columna
  PK/FK/indexada/con `DEFAULT`, `FOREIGN KEY` siempre como cláusula
  explícita (la forma inline de sqlite/postgres se ignora en silencio en
  MySQL 8), y `COLLATE utf8mb4_bin` en cada tabla (MySQL/MariaDB son
  insensibles a mayúsculas/acentos por defecto).
  **Límite nuevo, solo en MySQL/MariaDB**: `parent_path` y `name` de
  `files`/`directories` quedan acotados a 400/255 caracteres (el índice
  compuesto de unicidad no cabría en el límite de InnoDB si no) -- sqlite
  y postgres no limitan esa longitud en código Go hoy.
- `CreateDirectory`/`UpsertFile` (`internal/storage`) ramifican por
  dialecto: MySQL no tiene `ON CONFLICT`, usa `INSERT IGNORE`/
  `INSERT ... ON DUPLICATE KEY UPDATE`.
- `MoveDirectoryTree` ramifica la concatenación (`CONCAT` en vez de `||`,
  que en MySQL es el operador OR) y el escape de `LIKE` (`ESCAPE '\\'`,
  doble backslash, en vez de uno solo).
- Mínimo soportado: MySQL 8.0.16+ / MariaDB 10.2.1+ (versiones anteriores
  no aplican de verdad los `CHECK` del esquema, solo los parsean).
- Verificación real contra Docker (`scripts/dev.sh test-mysql`/
  `test-mariadb`/`test-postgres`, `internal/dbtest`): el mismo arnés
  paramétrico por dialecto cierra también el hueco, previamente
  documentado en ADR-006, de que Postgres nunca se había probado contra
  una instancia real en este repo.

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

Detección de RAID (§12) implementada para Linux/mdadm -- ver más abajo. SMART/temperatura siguen pendientes (ver "Qué falta"). NexusCloud nunca implementará su propio RAID — detecta y muestra el estado de las tecnologías que ya ofrezca el sistema operativo.

## RAID (§12)

`nexuscloud storage raid` (`internal/storage/raidinfo`), solo lectura, con un adaptador por SO (mismo patrón que `storage disks`/`diskinfo`): `ErrUnsupported` en plataformas sin adaptador en vez de fallar.

- **Linux** ([ADR-018](architecture/decisions/ADR-018-raid-detection.md)): arrays mdadm vía `/proc/mdstat`. Nivel, dispositivos miembro y estado (`OK`/`DEGRADADO`/`INACTIVO`), más el progreso si hay una reconstrucción en curso. `/proc/mdstat` ausente se trata como "sin arrays", no como error.
- **Windows** ([ADR-019](architecture/decisions/ADR-019-raid-windows.md), [ADR-027](architecture/decisions/ADR-027-raid-windows-correlacion.md)): Storage Spaces vía PowerShell (`Get-VirtualDisk`) -- a diferencia de `diskinfo` (syscalls Win32 directos), Storage Spaces no tiene API Win32 clásica, es WMI/CIM puro. `Level` muestra la nomenclatura propia de Storage Spaces ("Simple"/"Mirror"/"Parity"), no "raidN". Lista de discos físicos por array (`Get-PhysicalDisk -VirtualDisk`) y porcentaje de reconstrucción (`Get-StorageJob -VirtualDisk`, solo si además `Recovering` ya es `true`) ya correlacionados en un único script PowerShell.
- **RAID hardware (controladoras dedicadas) y JBOD quedan descartados permanentemente, decisión del usuario (2026-09-13)**: cada fabricante expone su propia CLI privativa (MegaCli/storcli para Broadcom/LSI, perccli para Dell, arcconf para Adaptec...) sin un estándar portable común, y no hay ninguna controladora concreta a la que apuntar en este momento. mdadm/Storage Spaces/ZFS ya cubren el caso software, que es la inmensa mayoría de despliegues self-hosted. Si en el futuro el usuario adquiere una controladora concreta, se reconsiderará entonces contra ese modelo específico.

**Limitación de verificación, documentada en ambas ADR y reconfirmada el 2026-09-13 (Tarea #32)**: la detección *positiva* (array sano/degradado/en reconstrucción, y lo mismo para Snapshots ZFS/Btrfs) está probada con texto/JSON realista en tests unitarios, pero no contra un array/pool/subvolumen real. En Windows, crear uno exigiría modificar el Storage Pool real de la máquina de desarrollo. En Linux, se intentó de verdad con un contenedor Docker `--privileged` (autorizado por el usuario) y se confirmó la causa raíz precisa: el kernel que Docker Desktop comparte en esta máquina Windows (`...-microsoft-standard-WSL2`) no tiene los subsistemas `md`/`btrfs`/`zfs` en absoluto -- `/lib/modules/<kernel>/` vacío en cualquier contenedor, `/proc/mdstat` y el fstype `btrfs` ausentes. `--privileged` concede capacidades, no inyecta un módulo de kernel que nunca se compiló para ese kernel -- no es un problema de permisos, y no hay combinación de imagen/paquete que lo arregle desde dentro de un contenedor de Docker Desktop en Windows. La única vía real para cerrar esta verificación es un Linux nativo de verdad (no Windows+Docker Desktop) con estos módulos disponibles -- casi cualquier distribución los trae de serie para mdadm/Btrfs, y las más comunes (Ubuntu) para ZFS también. El caso "sin arrays"/"sin instantáneas" (la ruta que sí se puede probar de verdad) se verificó end-to-end en ambos sistemas operativos contra el mecanismo real, incluido un binario Windows real ejecutado nativamente en la propia máquina de desarrollo.

## Identificadores

Los IDs de archivo son UUID v4 aleatorios (`internal/idgen`), nunca incrementales — evita enumeración e IDOR (§123, §198-199). La ruta física en disco (`<owner_id>/<parent_path>/<name>`) aísla a cada propietario incluso a nivel de filesystem.

## Integridad

Cada subida calcula su SHA-256 mientras escribe (un único `io.Copy` con `io.MultiWriter` hacia el fichero y el hasher, sin cargar el contenido completo en memoria, §136-137). La escritura es atómica: fichero temporal + `rename`, así que un fallo a mitad de subida nunca downgradea un archivo existente a un estado corrupto.

## Papelera (§16)

Activada por defecto (`trash.enabled: true`, `trash.retentionDays: 30`). `Delete`/`DeleteDirectory` son soft-delete (columna `deleted_at`, migración 0003) cuando la papelera está activada; con `trash.enabled: false` borran directamente y para siempre, como en la Fase 1.

- Un elemento en la papelera desaparece de `List`/`ListDirectories` pero sigue siendo descargable por su propietario, y aparece en `GET /api/v1/trash`.
- **Simplificación deliberada**: la restricción `UNIQUE(pool_id, owner_id, parent_path, name)` de `files`/`directories` (migración 0001) no se tocó al añadir la papelera — recrearla como índice único parcial (`WHERE deleted_at IS NULL`, lo que permitiría reutilizar un nombre mientras el original sigue en la papelera) exigiría recrear la tabla en SQLite sin poder probarlo aquí contra Postgres real. Por eso, mientras algo ocupa un nombre en la papelera, subir/crear algo nuevo con ese mismo nombre se rechaza (`ErrNameOccupiedByTrash`, HTTP 409) en vez de resucitar/sobrescribir el contenido en papelera en silencio (§128) — el usuario debe restaurar, purgar o usar otro nombre primero.
- **Purga automática** ([ADR-023](architecture/decisions/ADR-023-papelera-retencion-tamano.md)): un ticker en segundo plano (`internal/server.startTrashPurgeLoop`) revisa cada hora (y una vez al arrancar) y borra para siempre cualquier elemento que lleve más de `trash.retentionDays` en la papelera, o que sobre para que el total ocupado quepa en `trash.maxTotalSizeBytes` (`0` = sin límite) — `FileService.PurgeExpiredTrash`. Las dos políticas componen como "el más restrictivo gana" (se poda si CUALQUIERA lo pide), al revés que la retención de backups (ADR-017): un límite de tamaño es protección de disco, no una promesa de cuánto conservar. Las carpetas nunca pesan (solo se puede trashear una ya vacía), así que solo se purgan por antigüedad.
- Al mover una carpeta a la papelera se retira también su marcador físico (está garantizado vacío); al restaurarla, `RestoreDirectory` lo recrea.

## Versionado (§15)

Activado por defecto (`versioning.enabled: true`, `versioning.maxVersionsPerFile: 10`). Subir contenido distinto a un path que ya tiene un archivo activo aparta automáticamente el contenido anterior como una versión del historial en vez de perderlo; el archivo conserva siempre su mismo ID.

- **Flujo de `Upload`**: el contenido entrante se escribe primero a una ubicación provisional (`.nexuscloud-staging/`) para poder calcular su SHA-256 sin tocar todavía el contenido existente. Si hay un archivo activo en ese path y su hash difiere del nuevo (subir contenido idéntico no crea versión, evita ruido), su contenido actual se **mueve** (no se copia) a `.nexuscloud-versions/<file_id>/<version_num>` y se registra en `file_versions` (migración 0004); solo entonces el contenido en staging se mueve a su destino final.
- `GET /api/v1/files/{id}/versions` lista el historial; `GET .../versions/{n}` descarga una versión concreta; `POST .../versions/{n}/restore` la restaura.
- **Restaurar nunca pierde datos**: la versión actualmente vigente pasa, a su vez, a formar parte del historial antes de que la versión antigua ocupe su lugar — ver [ADR-007](architecture/decisions/ADR-007-versioning.md).
- **Límite automático** ([ADR-024](architecture/decisions/ADR-024-versionado-retencion-antiguedad-espacio.md)): al superar `maxVersionsPerFile`, o al superar `maxVersionAgeDays` de antigüedad, o al superar `maxVersionsTotalSizeBytes` de espacio total del historial (los tres `0` = sin límite), se purgan las versiones que sobren (contenido + fila) — política de limpieza automática de §15. Las tres políticas componen como "el más restrictivo gana" (se purga si CUALQUIERA lo pide), al revés que la retención de backups (ADR-017): son límites de protección de espacio en disco, no una promesa de cuánto historial conservar.
- Borrar un archivo para siempre (`?permanent=true`, o purga por retención de la papelera) purga también todo su historial de versiones: no tiene sentido conservarlo sin el archivo al que pertenece.

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

## Snapshots (§17)

`nexuscloud storage snapshots` (`internal/storage/snapshotinfo`), solo lectura, con un adaptador por SO (mismo patrón que `storage disks`/`storage raid`): `ErrUnsupported` en plataformas sin adaptador.

- **Windows** ([ADR-020](architecture/decisions/ADR-020-snapshot-detection.md)): VSS (Volume Shadow Copy Service) vía PowerShell (`Get-CimInstance Win32_ShadowCopy`), igual criterio que Storage Spaces en RAID -- WMI/CIM puro, sin API Win32 clásica. Nunca usa `vssadmin list shadowstorage` (exige privilegios elevados) -- solo lo que ya se puede consultar sin elevar. Hallazgo real a tener en cuenta en cualquier adaptador Windows futuro que use `ConvertTo-Json`: los campos `DateTime` de WMI se serializan como `"/Date(ms-desde-epoch)/"`, no ISO-8601.
- **Linux/ZFS** ([ADR-021](architecture/decisions/ADR-021-snapshot-zfs.md)): `zfs list -H -p -t snapshot -o name,creation` (banderas de scripting: sin cabecera, campos por TAB, fechas como epoch Unix -- nunca el formato pensado para humanos). El binario `zfs` ausente del `PATH` se trata como "sin snapshots" (no `ErrUnsupported`: Linux sí tiene adaptador, solo que esta vía no encuentra nada), igual que un pool ZFS sin importar.
- **Linux/Btrfs** ([ADR-022](architecture/decisions/ADR-022-snapshot-btrfs.md)): un snapshot Btrfs es un subvolumen de solo lectura, no un tipo de objeto propio como en ZFS -- se detecta vía `btrfs subvolume list -s <mountpoint>` sobre cada punto de montaje Btrfs encontrado en `/proc/mounts` (a diferencia de `zfs list`, que opera globalmente, este comando exige la ruta de un filesystem ya montado). Sin modo "parseable": se tokeniza texto pensado para humanos, asumiendo que el token `path` es siempre el último campo. El binario `btrfs` ausente, o ningún montaje Btrfs, se trata igual que ZFS ausente: lista vacía, no `ErrUnsupported`. En Linux, `Enumerate` agrega ambas vías (ZFS + Btrfs); cada una degrada a vacío de forma independiente, así que la ausencia de una nunca oculta lo que la otra encuentre.
- NexusCloud nunca crea ni elimina snapshots -- §17 es sobre integrarse con los que ya existen, nunca gestionarlos activamente.

**Limitación de verificación, documentada en ADR-020/021/022**: la detección *positiva* (con instantáneas reales) está probada con datos realistas en tests unitarios en los tres adaptadores, pero no contra una instantánea real -- en Windows, crearla exigiría privilegios elevados; en Linux, ni el entorno de desarrollo ni la máquina de esta sesión tienen ZFS ni Btrfs instalados/montados, así que ambos formatos (`zfs list` y `btrfs subvolume list`) se verificaron solo contra su documentación estable, sin ninguna evidencia empírica propia (a diferencia de todo lo demás en esta serie) -- los dos slices con menos verificación directa de los tres pilares de Fase 5. El caso "sin instantáneas" sí se verificó end-to-end en ambos sistemas operativos contra el mecanismo real (binario Windows nativo en la máquina de desarrollo; binario Linux real sin `zfs` ni `btrfs` instalados en el contenedor de esta sesión).

## Backup Manager (§18)

CLI para lo manual (`nexuscloud backup run|list|restore|restore-to-pool|verify`), sin endpoint HTTP de gestión ni UI web todavía -- mismo orden que Storage Pools en la Fase D (el único HTTP nuevo es el de recepción de backups remotos, ver más abajo, no un CRUD para el propio administrador). Completo, incremental (`--incremental`), cifrado (`--encrypt`) o a otro servidor NexusCloud (`--dest https://...`) -- ver [ADR-015](architecture/decisions/ADR-015-backup-manager.md), [ADR-016](architecture/decisions/ADR-016-backup-automatico.md), [ADR-017](architecture/decisions/ADR-017-retencion-backups.md), [ADR-025](architecture/decisions/ADR-025-backup-restore-a-pool.md), [ADR-026](architecture/decisions/ADR-026-backup-incremental.md), [ADR-028](architecture/decisions/ADR-028-backup-cifrado.md) y [ADR-029](architecture/decisions/ADR-029-backup-destino-remoto.md).

- `backup run [--dest <ruta>] [--pool <id-o-nombre>]...`: respalda todos los pools activos elegibles (o solo los indicados) a `--dest` (por defecto, la carpeta de backups de esta instancia, `cfg.BackupsDir()`). Un pool con `backup_policy=off` queda excluido siempre, incluso si se pide explícitamente por `--pool`; `inherit`/`on` se incluyen.
- Cada backup escribe `<dest>/<job-id>/data/<pool-id>/<owner-id>/<ruta>/<nombre>` (calca el aislamiento físico por propietario que ya usa `FileService`) y un `<dest>/<job-id>/manifest.json` autocontenido con propietario/ruta/nombre/tamaño/SHA-256 de cada fichero -- el manifiesto es la fuente de verdad para restaurar, no la base de datos.
- La papelera nunca se respalda (solo archivos activos). Cada fichero se verifica por SHA-256 mientras se copia; si alguno no coincide, el job entero se marca `failed` sin llegar a escribir `manifest.json` -- nunca queda un backup a medias que parezca completo.
- **`--incremental`** ([ADR-026](architecture/decisions/ADR-026-backup-incremental.md), también `backup.incremental` para el modo automático): un fichero cuyo SHA-256 no cambió desde el backup completado más reciente en ESE MISMO destino se enlaza (hardlink) a esa copia ya existente en vez de recopiarse -- el manifiesto sigue siendo siempre completo (cada job sigue siendo independientemente restaurable, `restore`/`restore-to-pool`/`verify` no necesitan saber que esto existe). Sin backup anterior, se comporta como uno completo. Si el enlace no se puede hacer (otro filesystem, el fichero anterior ya no está), se recopia igual sin abortar el job.
- `backup restore <job-id> --dest <ruta>` extrae los ficheros del backup (re-verificando SHA-256) a una carpeta elegida -- no reinserta en un pool activo ni toca la base de datos. Intenta recuperar todos los ficheros que pueda: uno con bitrot en el propio disco de backup no impide restaurar el resto.
- **`backup restore-to-pool <job-id> --pool <id-o-nombre>`** ([ADR-025](architecture/decisions/ADR-025-backup-restore-a-pool.md)): a diferencia de `restore`, reinserta cada fichero como un archivo activo normal -- navegable, descargable, versionable -- en el pool indicado (siempre explícito, nunca el original del manifiesto: puede que ese pool ya no exista o esté inactivo, precisamente el escenario que esta feature cubre). Pasa por `FileService.Upload`/`Mkdir` (heredando versionado automático si el path ya está ocupado, y materializando carpetas intermedias nivel a nivel) en vez de escribir el filesystem/la BD a mano. Mismo criterio de mejor esfuerzo que `restore`: un propietario que ya no existe o un fichero con bitrot fallan solo ese fichero, el resto se restaura.
- **Automático** (`backup.enabled: true` + `backup.intervalMinutes`, `false` por defecto): un bucle en segundo plano del propio servidor (`internal/server.startBackupScheduleLoop`, mismo patrón que la purga de papelera) ejecuta el equivalente a `backup run` sin flags cada N minutos. Desactivado por defecto porque copia datos reales, normalmente al mismo disco (§19, sin cumplir la regla 3-2-1 todavía) -- el administrador debe activarlo a propósito. No se ejecuta al arrancar el servidor, solo tras el primer intervalo completo.
- `backup list` muestra el historial (estado/fecha/ficheros/tamaño/destino) leyendo `backup_jobs` (migración `0007`).
- **Retención** (`--keep-last N`/`--keep-days N` en `run`, o `backup.retentionCount`/`backup.retentionDays` para el modo automático; `0` = sin límite cada uno): tras un backup completado con éxito, conserva los backups completados que cumplan CUALQUIERA de las políticas activas en ESE MISMO destino (nunca cuenta jobs `failed` ni afecta a otros destinos) -- p.ej. con las dos activas, un backup reciente por cantidad pero antiguo por días igualmente sobrevive. Poda en mejor esfuerzo: si borrar una carpeta antigua falla, su fila en `backup_jobs` tampoco se borra, para no dejar un puntero a nada -- nunca hace fallar el backup recién completado por esto.
- `backup verify <job-id>` recalcula el SHA-256 de cada fichero ya copiado en un backup, sin restaurarlo a ningún sitio ni tocar nada -- útil para comprobar la salud de un backup antiguo (p.ej. antes de confiar en él para borrar el original) sin pagar el coste de una restauración completa. Revisa TODOS los ficheros aunque alguno falle (igual que `restore`); código de salida distinto de cero si algo no verificó.
- **`--encrypt`** ([ADR-028](architecture/decisions/ADR-028-backup-cifrado.md), también `backup.encrypt` para el modo automático): cifra cada fichero con AES-256-CTR, clave derivada vía Argon2id (mismos parámetros de coste que el hash de contraseñas, `security.argon2`) de `NEXUSCLOUD_BACKUP_PASSPHRASE` -- **obligatoria** en el entorno si se activa, o `run` falla antes de crear nada. Salt nuevo por job + IV nuevo por fichero, ambos en claro dentro de `manifest.json` (no son secretos, solo la passphrase lo es). `restore`/`restore-to-pool`/`verify` sobre un backup cifrado exigen la misma variable de entorno; una passphrase incorrecta se reporta como fallo de integridad de ese fichero (igual que un bitrot), nunca como un crash. `--encrypt` desactiva el hardlink de `--incremental` para ese run (cada job cifrado usa clave/IV distintos, así que el ciphertext de "el mismo fichero" nunca coincide entre jobs) -- se puede pedir la combinación sin que falle, simplemente ese run concreto no ahorra espacio por deduplicación.
- **Destino remoto** ([ADR-029](architecture/decisions/ADR-029-backup-destino-remoto.md), regla 3-2-1, §19): `--dest` acepta una URL `http://`/`https://` de otro servidor NexusCloud en vez de una carpeta local (también `backup.remoteDestination` para el modo automático) -- todo lo demás (`--encrypt`, `--keep-last`/`--keep-days`, `restore`/`restore-to-pool`/`verify`) funciona igual, sin flags nuevos. Exige `NEXUSCLOUD_BACKUP_REMOTE_TOKEN` en el entorno del ORIGEN, que debe coincidir con `NEXUSCLOUD_BACKUP_RECEIVE_TOKEN` en el entorno del servidor REMOTO -- un token compartido dedicado (nunca una sesión de usuario, nunca en `config.yaml`), verificado contra el header `X-NexusCloud-Backup-Token` en las rutas `/api/v1/backups/inbound/*`, que ni se registran si el remoto no configuró `NEXUSCLOUD_BACKUP_RECEIVE_TOKEN`. `--incremental` con un destino remoto nunca enlaza (no hay inodos que compartir por red): cae a copia completa siempre, igual mecanismo que cuando el enlace local falla. En el servidor remoto, un backup recibido aparece en SU PROPIO `backup list` con total normalidad -- es indistinguible de uno hecho localmente ahí, salvo por cómo llegaron los bytes.

## Qué falta (fases posteriores)

- **RAID hardware (controladoras dedicadas) y JBOD** (§12): descartado permanentemente, decisión del usuario (2026-09-13) -- ver nota en la sección RAID arriba.
- **Backup Manager**: completo -- manual/automático/retención/verify/restore-to-pool/incremental/cifrado/destino remoto (ver sección Backup Manager, ADR-015 a ADR-029). Lo único explícitamente fuera de alcance: reintentos automáticos ante un fallo de red a medio backup remoto (ADR-029), y el sistema general de API Tokens (§78) que un destino remoto más flexible podría querer más adelante.
- **Subida anónima** (§38): activación explícita del admin, foco anti-abuso -- modelo distinto al de un enlace normal de Sharing.
- **Miniaturas/previsualización/búsqueda de contenido** (§33-35): Fase 2, nunca empezado -- la pieza de mayor alcance de todo el backlog.
- **Favoritos/Recientes** (§143): mencionado en el dashboard del explorador web pero sin backend que lo soporte todavía (ver `architecture.md`).
- **Sharing: permiso de subida a un usuario/grupo concreto** (§37): hoy solo los enlaces lo soportan (no existe ningún camino de autorización de subida para user/group, solo vía token de enlace público -- haría falta diseñar uno nuevo); notificaciones por email tampoco están implementadas.
- **MySQL/MariaDB** (Fase 1): implementado ([ADR-031](architecture/decisions/ADR-031-mysql-mariadb.md)) -- tercer dialecto, `go-sql-driver/mysql`. Ver sección "Base de datos" arriba para las divergencias de esquema/SQL reales frente a sqlite/postgres.
- **Mover/renombrar, limpieza de carpetas vacías, umbral de borrado configurable, auto-sync por par y detección de rutas solapadas en el motor de sync** (Fase 3, cliente Flutter): implementado (ADR-030 parte B, §85) -- ya no son gaps.
- **Auto-actualización del cliente de escritorio** (Fase 3): implementada con Velopack, sin certificado de firma de código ([ADR-032](architecture/decisions/ADR-032-client-auto-update-velopack.md)) -- ya no es un gap; la vía MSIX con certificado real se descartó.
