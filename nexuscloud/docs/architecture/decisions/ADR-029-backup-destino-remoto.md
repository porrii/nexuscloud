# ADR-029: Backup a Destino Remoto -- Otro Servidor NexusCloud (§18/§173/§19)

## Estado

Aceptado.

## Contexto

El Backup Manager (ADR-015/025/026/028) solo respaldaba a una carpeta LOCAL: `Run`/`Restore`/`RestoreToPool`/`Verify`/la retención (ADR-017) escribían y leían con `os.MkdirAll`/`os.OpenFile`/`os.Open`/`os.RemoveAll` directamente sobre `DestinationPath`. Un destino SMB/NFS ya funcionaba si se montaba como carpeta local, pero el spec (§18/§173/§19, regla 3-2-1) pide explícitamente poder señalar como destino **otro servidor NexusCloud real, por red** -- no un host cualquiera por SSH, una segunda instancia de este mismo software. Era el último punto pendiente del Backup Manager (Tarea #30), cerrando el pilar completo de Fase 5 salvo verificación con hardware RAID/ZFS/Btrfs real (Tarea #32, aparte).

`storage.Provider` (`internal/storage/provider.go`) ya estaba deliberadamente diseñado agnóstico de backend (`Write(relPath, io.Reader)`/`Read`/`Delete`/`Exists`/`MkdirAll`/`Move`, con un comentario explícito dejando sitio a "S3 o remote NexusCloud" para pools, §157/ADR-002), pero el Backup Manager nunca lo usaba para su destino -- solo para leer del pool origen.

## Decisión

### 1. Nueva interfaz `backup.Destination`, deliberadamente NO reutiliza `storage.Provider`/`Pool.Type`

`storage.Provider` está atado a semántica de POOL (BackupPolicy, prioridad, versionado, sharing) que un destino de backup no necesita; convertir un destino de backup en un "Storage Pool remoto" habría expandido el alcance hacia §157/158 (Storage Pools remotos/S3), explícitamente un slice mayor y distinto según el propio NEXUSCLOUD.md -- confirmado al investigar que `Pool.Type` solo acepta `"local"` hoy (rechazado en código, `ProviderResolver.For`, no en el esquema SQL) y que nadie ha empezado esa pieza todavía. En su lugar, `internal/backup/destination.go` define una interfaz mínima con exactamente lo que el paquete necesita:

```go
type Destination interface {
    WriteFile(ctx context.Context, jobID, relPath string, r io.Reader) (size int64, err error)
    OpenFile(ctx context.Context, jobID, relPath string) (io.ReadCloser, error)
    RemoveFile(ctx context.Context, jobID, relPath string) error
    RemoveJob(ctx context.Context, jobID string) error
    Link(ctx context.Context, prevJobID, jobID, relPath string) error
}
```

`relPath` es siempre una ruta lógica con `/` (nunca separadores nativos del SO): `"manifest.json"` o `"data/<pool-id>/<owner-id>/<ruta>/<nombre>"` -- la misma convención que ya usaba `jobDir` antes de este ADR, ahora expresada como sufijo relativo en vez de con `filepath.Join` absoluto. `resolveDestination(destPath, remoteToken string)` decide `localDestination` vs `remoteDestination` mirando si `destPath` empieza por `http://`/`https://`.

### 2. `localDestination` gana `storage.SafeJoin` -- refactor de comportamiento, no solo de forma

Antes de este ADR, `jobID`/las rutas siempre venían de código interno de confianza (`idgen.New()`, metadatos ya validados en la BD). Desde este ADR, el mismo tipo puede quedar detrás de un endpoint HTTP autenticado solo por un token compartido (ver punto 4) -- así que `localDestination.path()` pasa `jobID+"/"+relPath` por `storage.SafeJoin(root, ...)` (el mismo aislamiento que ya usa todo `internal/storage` para archivos de usuario) en vez de un `filepath.Join` a pelo. Esto hace que la protección exista UNA VEZ, para cualquier llamador presente o futuro, en vez de depender de que cada nuevo llamador recuerde añadirla por su cuenta. `NewLocalDestination(root)` expone el tipo fuera del paquete para que `internal/api/v1` lo reutilice tal cual en el servidor receptor (ver punto 5) -- un backup recibido por red queda en disco con el mismo layout, y la misma protección, que uno hecho localmente.

### 3. `copyVerified` se divide en `copyToDestination` (Run) y `copyToLocalFile` (restoreOneFile), con el cifrado recompuesto del lado LECTURA

`Run` escribe al `Destination` del backup (local o remoto); `restoreOneFile` (usado por `Restore`) siempre escribe a una carpeta LOCAL simple (la carpeta de restauración elegida por el usuario) leyendo del `Destination` -- dos responsabilidades de escritura distintas que antes compartían una única función con un parámetro `key` a veces vestigial. `copyToDestination` compone la cadena de lectura así: `io.TeeReader(r, hasher)` (alimenta el hash con el PLAINTEXT tal como sale de la fuente) envuelto, si `key != nil`, por un `cipher.StreamReader` (cifra sobre la marcha) -- el resultado se pasa a `dest.WriteFile`, que para un destino remoto es literalmente el cuerpo de una petición HTTP `PUT` (streaming real, sin cargar el fichero completo en memoria, con `Transfer-Encoding: chunked` cuando el tamaño no se conoce de antemano). El hash comparado sigue siendo siempre el del PLAINTEXT, exactamente igual que ADR-028: cambia solo lo que se escribe/lee en la red, nunca el criterio de integridad. Si el hash no coincide, se limpia lo ya escrito con `dest.RemoveFile` -- para un destino remoto, esto acepta el coste de haber subido de más en el caso raro de un mismatch (normalmente bitrot del propio origen) a cambio de no complicar el protocolo con una fase de "verificar antes de comprometerse".

### 4. Autenticación: token compartido dedicado -- nunca una sesión de usuario, nunca §78 (API Tokens) todavía

Investigación previa confirmó que NO existe ningún mecanismo de autenticación máquina-a-máquina hoy (solo sesión de usuario por login; §78 "API Tokens" está documentado en el spec pero sin ninguna implementación, confirmado sin resultados en todo el repo). Decisión tomada con el usuario entre tres opciones (usuario+contraseña dedicados; implementar §78 ahora; token compartido dedicado): **token compartido dedicado**, mismo espíritu que los tokens de `PublicLink` (un secreto de un único propósito, no una identidad de usuario) -- evita mezclar tráfico de máquina con una identidad de usuario real (2FA, auditoría de sesiones) y evita construir ya una feature de seguridad transversal (§78) mucho más grande que este slice.

- En el ORIGEN: `NEXUSCLOUD_BACKUP_REMOTE_TOKEN` (env var, nunca un flag de CLI -- mismo criterio que `NEXUSCLOUD_BACKUP_PASSPHRASE`, ADR-028: un secreto no debe pasar por argv ni por el historial de shell). `RunOptions`/`RestoreOptions`/`RestoreToPoolOptions`/`VerifyOptions` ganan `RemoteToken string`.
- En el RECEPTOR: `NEXUSCLOUD_BACKUP_RECEIVE_TOKEN` (env var, leída UNA VEZ al arrancar el servidor -- **tampoco vive en `config.yaml`**, a diferencia de lo esbozado inicialmente en el plan: se prefirió mantener la misma disciplina de "secretos solo por variable de entorno" ya establecida para la passphrase, en vez de introducir una inconsistencia nueva en el slice siguiente). Con la variable vacía (por defecto), `NewRouter` ni siquiera registra las rutas `/api/v1/backups/inbound/*` -- 404 llano, no un 401 que confirmaría que la capacidad existe (secure by default, §3/§47, mismo criterio que `publicLinksEnabled`).
- `requireBackupToken` (middleware nuevo y estrecho en `internal/api/v1`) compara el header `X-NexusCloud-Backup-Token` con `subtle.ConstantTimeCompare` -- nunca toca `internal/auth`/sesiones/usuarios.

### 5. Protocolo del receptor: `PUT`/`GET`/`DELETE` por fichero + un paso `complete` explícito

Rutas nuevas, fuera del árbol de sesión de usuario (`internal/api/v1/backup_remote_handlers.go` + registro condicional en `router.go`):
- `PUT /api/v1/backups/inbound/{jobID}/files/*` -- escribe un fichero (delega en `backup.NewLocalDestination(cfg.BackupsDir())`).
- `GET /api/v1/backups/inbound/{jobID}/files/*` -- lo devuelve.
- `DELETE /api/v1/backups/inbound/{jobID}/files/*` -- borra un único fichero (usado cuando su hash no verificó, ver punto 3).
- `POST /api/v1/backups/inbound/{jobID}/complete` -- lee el manifiesto ya recibido y SOLO ENTONCES crea la fila en `backup_jobs` de esta instancia (status completed, contadores derivados del propio manifiesto).
- `DELETE /api/v1/backups/inbound/{jobID}` -- borra todo lo asociado al job (ficheros + fila si existe); lo usa la retención del ORIGEN (`pruneOldBackups`) para podar backups antiguos en el remoto igual que ya podaba uno local.

El paso `complete` explícito evita inventar transacciones distribuidas: si el origen nunca llega a llamarlo (corte de red a medias), el job recibido queda como una carpeta huérfana sin fila -- invisible en `backup list` e inofensiva, EL MISMO invariante que ya sostiene ADR-015 para un backup local incompleto (`backup list` lee de la BD, nunca del disco), reutilizado aquí en vez de inventar uno nuevo.

### 6. CLI: `--dest` ya lo cubre, sin flags nuevos de superficie

`backup run --dest https://backup2.example.com:8080 [--encrypt] [--incremental]...` -- ninguna combinación existente necesitó cambiar su forma:
- `--encrypt` + remoto: recomendable (cifrar antes de salir por red es más seguro que depender solo de TLS), cero código adicional -- `copyToDestination` ya cifra antes de que el reader llegue a `dest.WriteFile`.
- `--incremental` + remoto: el hardlink nunca aplica -- `remoteDestination.Link` siempre devuelve `ErrLinkUnsupported`, y `Run` ya sabía caer a copia completa cuando el enlace falla (ADR-026) sin ninguna rama nueva. Documentado como un tercer caso (junto al cifrado) donde `--incremental` no ahorra espacio.
- `backup list`/`restore`/`restore-to-pool`/`verify` no cambiaron de forma: el ORIGEN ya sabe qué jobs lanzó y contra qué destino por su propia tabla `backup_jobs`, nunca hace falta "listar lo que hay en el remoto" por separado.
- El modo automático programado (`backup.enabled`, ADR-016) también puede apuntar a un destino remoto: `BackupConfig.RemoteDestination` (vacío = local, de siempre) sustituye `destDir` en `startBackupScheduleLoop` si no está vacío -- cierra la asimetría que habría quedado si solo el CLI manual pudiera usar un destino remoto mientras cifrado/incremental ya llegaron al modo automático en ADR-028/026.

## Trade-off, con la misma franqueza que el resto de esta serie

Un fallo de red aborta el job entero igual que hoy aborta un fallo de escritura local (ADR-015) -- no hay reintentos automáticos ni reanudación de una subida cortada a medias. Es una decisión consciente: la incrementalidad y el cifrado ya son optimizaciones/protecciones que se comportan de forma predecible ante un fallo; añadir reintentos con backoff introduciría estados intermedios (¿reintentar desde el fichero que falló, o desde cero?) que no aportan valor real hasta que exista una necesidad concreta de backups muy grandes sobre redes inestables -- se deja como mejora futura si hace falta, no se implementa especulativamente.

## Fuera de alcance (explícito)

- **Sistema general de API Tokens (§78)**: descartado para este slice por decisión explícita del usuario. Su propio slice futuro si hace falta revocación/permisos granulares desde UI -- el token compartido de este ADR cubre exactamente un propósito (autenticar el flujo de backup remoto), no es un sustituto de §78 para el resto de la API.
- **Storage Pools de tipo remoto/S3 (§157/§158)**: comparte la filosofía de `Provider` ya preparada para ello, pero es un slice mayor y distinto -- no confundir con este ADR.
- **Reintentos automáticos / reanudar una subida cortada a medias**: ver Trade-off arriba.
- **TLS obligatorio**: se permite `http://` para LAN de confianza, mismo criterio que el propio servidor (§66, que ya acepta HTTP plano por configuración) -- se documenta la recomendación de `https://` al cruzar una red no confiable, sin bloquear `http://` por código.

## Consecuencias

- Nuevo `internal/backup/destination.go` (`Destination`, `localDestination`, `remoteDestination`, `NewLocalDestination`); `manifest.go` (`ReadManifest` exportado, `readManifest`/`writeManifest` toman `Destination`+`jobID`); `manager.go` refactorizado -- ninguna función toca ya `os.*`/`filepath.*` sobre el destino del backup directamente.
- `Verify` pasa de argumentos posicionales a `VerifyOptions` (mismo criterio que `RestoreOptions`/`RestoreToPoolOptions`, momento natural para la consistencia al tener que tocar su firma de todas formas).
- Nuevo `internal/api/v1/backup_remote_handlers.go` + registro condicional en `router.go`; `Handlers` gana `BackupsDir`/`BackupRepo`/`BackupReceiveToken`.
- `BackupConfig.RemoteDestination` (config.yaml, no es un secreto); el token en sí nunca vive en config.yaml en ninguno de los dos extremos.
- 12 tests nuevos: `remoteDestination` contra un servidor HTTP simulado (`httptest.Server`) cubriendo write/open/remove/removeJob, token incorrecto rechazado, `Link` siempre `ErrLinkUnsupported`, un hash que no verifica limpia lo ya subido; `localDestination` confinado dentro de su root pese a un jobID/relPath "atacante"; tests de integración contra un `server.Build` real (rutas ausentes sin token configurado, token ausente/incorrecto rechazado, y el ciclo completo PUT→complete→GET→DELETE registrando/borrando de verdad en `backup_jobs`).
- **Verificado E2E contra DOS servidores NexusCloud reales en contenedores Docker distintos, en la misma red Docker** (no un solo proceso simulando ambos lados): usuario y fichero reales vía HTTP en el origen; `backup run --dest http://<remoto>:8080 --encrypt` viajó de verdad por la red del contenedor; `backup verify` con la passphrase correcta dio OK leyendo del remoto, con una incorrecta reportó el mismo fallo de integridad que un bitrot; `backup restore` y `backup restore-to-pool` reconstruyeron el contenido original exacto leyendo de vuelta por red; un segundo `backup run --keep-last 1` disparó la retención remota (`DELETE` HTTP real), confirmado con el propio `backup list` del servidor REMOTO mostrando solo el job superviviente.
