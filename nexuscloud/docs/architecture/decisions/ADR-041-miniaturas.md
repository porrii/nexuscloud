# ADR-041: Miniaturas de imagen, vídeo y PDF

## Estado

Aceptado.

## Contexto

§34 (con §132/§138) pide miniaturas de JPG/PNG/WEBP/GIF/vídeo/PDF, con una
advertencia explícita: *"nunca ejecutar procesadores de archivos no
confiables sin aislamiento y validación"*. De las tres piezas del ítem de
mayor alcance del backlog restante (búsqueda, previsualización, miniaturas —
[ADR-040](./ADR-040-busqueda.md)), esta es la de mayor riesgo: la primera
vez que NexusCloud (a) decodifica contenido de usuario en el **servidor**
(la previsualización de ADR-040 Fase 2 lo hace en el navegador de quien
mira) y (b) ejecuta un binario externo (`exec.Command`) sobre ese contenido.

Estado de partida, confirmado antes de diseñar (3 agentes de exploración de
solo lectura + un pase dedicado de `security-reviewer` sobre el DISEÑO,
antes de escribir ningún código):

- Cero `exec.Command` sobre contenido de usuario en todo el proyecto.
- `config.Storage.ThumbnailsDir`/`Layout.Thumbnails` ya existían y se creaban
  en disco al arrancar, sin consumidor.
- `FileMeta.SHA256` ya existía (reutilizado para deduplicar en backups
  incrementales).
- La cuota (ADR-036) nunca toca el filesystem: `OwnerUsage` solo suma
  `SUM(size_bytes)` sobre `files`/`file_versions` — las miniaturas quedan
  fuera sin doble conteo ni forma de evadirla.
- Cero patrón de worker-pool/semáforo en el proyecto.
- La imagen de producción era `gcr.io/distroless/static-debian12:nonroot`
  (binario 100% estático, sin shell, sin gestor de paquetes) — ffmpeg/
  poppler no pueden vivir ahí sin cambiar de imagen base.
- **Ningún panic de Go se recuperaba en todo el proyecto**: cero
  `middleware.Recoverer`/`recover()` en `internal/`. Tolerable hasta esta
  fase porque nunca había código de parseo real expuesto a bytes
  adversariales.

## Decisión

1. **Alcance completo en una sola fase**: JPG/PNG/GIF/WEBP se decodifican en
   proceso (stdlib de Go + `golang.org/x/image/webp`, cero subproceso);
   vídeo (`ffmpeg`) y PDF (`pdftoppm` de poppler-utils) vía `exec.Command`
   — el primer subproceso del proyecto sobre contenido no confiable.
   Decisión explícita de no dividir en "Fase 3a: solo imagen" / "Fase 3b:
   vídeo y PDF" pese a que esa hubiera sido la secuencia de menor riesgo por
   iteración.

2. **Cola persistente (`thumbnail_jobs`, migración `0016`)**, no una cola en
   memoria: sobrevive a un reinicio del proceso y da visibilidad
   administrativa real (`GET /admin/thumbnail-jobs?status=pending|failed`)
   — sin esto, un administrador no tendría forma de saber que `ffmpeg` no
   está instalado salvo mirando logs. `UNIQUE(file_id)`: resubir el mismo
   archivo reinicia el job existente (`UpsertPending`) en vez de duplicar
   fila. Un job resuelto con éxito se **borra** (`MarkDone`), no se
   conserva — solo importa lo pendiente o roto. Tras `thumbnailMaxAttempts`
   (3) intentos fallidos consecutivos pasa a `status=failed` y deja de
   reintentarse solo.

   Tres puntos de entrada comparten el mismo núcleo
   (`generateAndCacheOnce`/`generateThumbnailBytes`,
   `internal/storage/file_service_thumbnail.go`):
   - `FileService.Upload` encola un job best-effort tras cada subida
     (`enqueueThumbnailJob`), gateado explícitamente en
     `thumbnailsEnabled` — sin ese chequeo la tabla se llenaría en
     silencio aunque la función entera esté desactivada.
   - `startThumbnailLoop` (`internal/server/server.go`, esqueleto de
     `startTrashPurgeLoop`): goroutine en segundo plano, procesa **un** job
     `pending` cada 10s, secuencial, sin worker pool (mismo criterio que el
     resto del proyecto).
   - `GET /api/v1/files/{id}/thumbnail` (bajo demanda, mismo chequeo de
     propiedad/compartición que `Download`): sirve desde caché o genera
     síncronamente si hace falta — cobertura retroactiva de archivos
     subidos antes de esta fase, o con `thumbnails.enabled=false` en su
     momento. `GetByFileID` (nunca resetea `attempts`) se consulta antes de
     `UpsertPending`, para que un job que el bucle ya esté trabajando no
     pierda su cuenta de intentos solo por consultarlo bajo demanda — bug
     real encontrado por el propio test suite durante el desarrollo (ver
     Consecuencias).

3. **Caché en disco particionada por propietario**, no por contenido puro:
   `<ThumbnailsDir>/<owner_id>/<sha256[0:2]>/<sha256>.jpg`. El diseño
   original deduplicaba por SHA256 sin más (dos archivos con bytes
   idénticos de dueños distintos comparten miniatura); el pase de
   `security-reviewer` señaló que eso abre un oráculo de temporización
   entre inquilinos — si ya conoces los bytes exactos de un archivo, medir
   la velocidad de respuesta revela si alguien más en el servidor ya subió
   ese mismo contenido, un canal cruzado que hoy no existe en ningún otro
   sitio del proyecto (`OwnerID` se comprueba en cada acceso, sin
   excepciones). Particionar por propietario cierra el canal sin renunciar
   a deduplicar dentro de un mismo usuario. Escritura **atómica**
   (`<destino>.tmp.<id> -> os.Rename`, mismo patrón que
   `FileService.Upload`): dos peticiones simultáneas sobre el mismo archivo
   nunca visto no entrelazan bytes en el mismo fichero — confirmado con
   `-race` y 20 goroutines concurrentes sobre la misma clave
   (`TestThumbnailCachePutConcurrenteEsAtomico`). Salida siempre JPEG
   (`thumbnailJPEGQuality = 80`), máx. 320px en su lado mayor preservando
   proporción (`golang.org/x/image/draw`), sea cual sea el formato de
   entrada — se pierde transparencia (fondo sólido), decisión consciente.
   `thumbnails.maxCacheBytes` (2 GiB por defecto) topa el total; sin
   eviction LRU (Pendiente explícito, ver Consecuencias). Las miniaturas
   nunca cuentan para la cuota de nadie (confirmado contra `usage.go`).

4. **Aislamiento por pipeline** (`internal/storage/thumbnail_image.go`,
   `thumbnail_exec.go`):
   - **Imagen**: límite de tamaño de entrada ANTES de leer nada
     (`ImageThumbnailLimits.MaxInputBytes`, 25 MiB por defecto);
     `image.DecodeConfig`/`webp.DecodeConfig` ANTES de `Decode` completo,
     rechazando si ancho×alto declarados superan `MaxPixels` (40 MP por
     defecto) — mitigación estándar de decompression bomb, cubierta además
     por un fuzz test (`FuzzDecodeAndResizeImage`, stdlib desde Go 1.18,
     89.208 ejecuciones sin panic en un run de 30s) que exige "nunca panic"
     sobre el pipeline completo, no solo sobre casos elegidos a mano. GIF
     decodifica un único frame (nunca `gif.DecodeAll`). `DefaultImageThumbnailTimeout`
     (10s) envuelve decode+resize+encode en su propia goroutine — con un
     matiz importante: al expirar, el handler deja de ESPERAR el resultado,
     pero la goroutine sigue viva consumiendo CPU/memoria hasta que termina
     por su cuenta (Go no tiene forma de preemptar una decodificación en
     curso). Es una fuga real, no solo lentitud — el semáforo del punto 6
     cubre este pipeline también, no solo vídeo/PDF.
   - **Vídeo/PDF**: comprobación de `exec.LookPath` al primer uso — si el
     binario no está, el job falla con gracia (`last_error` claro) en vez
     de colgarse. `exec.CommandContext` con `args` como slice de Go, jamás
     interpolado en shell; ruta de entrada siempre la física interna del
     storage. **`-protocol_whitelist file` obligatorio en ffmpeg**: "args
     como slice" evita que el ATACANTE inyecte comandos vía nombre de
     archivo, pero no evita que el propio CONTENIDO le pida a ffmpeg que
     abra otra cosa — varios demuxers (HLS/m3u8, `concat`, `subfile`)
     siguen referencias a rutas/protocolos embebidas dentro del archivo; un
     `.mp4` cuyo contenido real es una playlist puede hacer que ffmpeg abra
     `file:///etc/passwd` (LFI) o una URL interna (SSRF) como efecto
     colateral de "generar una miniatura", sin que nadie llame a ningún
     endpoint. Confirmado bloqueado con un fixture real
     (`TestGenerateVideoThumbnail_ProtocolWhitelistBloqueaFileScheme`).
     `pdftoppm -f 1 -l 1`: sin esto, un PDF diminuto en bytes con miles de
     páginas en blanco podría hacer que escriba miles de archivos antes de
     que el timeout lo mate. `DefaultVideoThumbnailTimeout`/
     `DefaultPDFThumbnailTimeout` (30s/20s) vía `exec.CommandContext`, que
     aquí SÍ manda `SIGKILL` de verdad; `SysProcAttr{Setpgid: true}` +
     `cmd.Cancel` en Linux (`thumbnail_exec_linux.go`) matan también
     cualquier hijo que ffmpeg/pdftoppm lancen. Verificado con un timeout
     real de 1µs contra un ffmpeg real
     (`TestGenerateVideoThumbnail_TimeoutMataElProceso`).
   - **Riesgo residual aceptado, no ignorado por omisión**: ffmpeg/poppler
     son C/C++ con historial de bugs de memoria encontrados por fuzzing —
     sin sandboxing por namespaces/seccomp (fuera de alcance esta ronda), un
     0-day exitoso en cualquiera de los dos da ejecución como el usuario del
     servicio. Aceptado para v1 porque ya se cierra el vector barato y real
     que sí correspondía a esta ronda (`-protocol_whitelist`, distinto de
     sandboxing) y porque sandboxing de subproceso es un salto de
     complejidad que merece su propia evaluación futura dedicada.

5. **Cambio de imagen Docker**: `debian:12-slim` con `ffmpeg`/
   `poppler-utils`/`ca-certificates` vía `apt-get install
   --no-install-recommends`, reemplazando `distroless/static-debian12:nonroot`
   — coste ACEPTADO explícitamente (imagen mayor, más paquetes que auditar,
   revierte parte de la reducción de superficie de ataque que esa elección
   buscaba), no presentado como gratis. Compensado con: usuario sin
   privilegios explícito, mismo UID:GID 65532 que la imagen anterior (para
   no romper el propietario de volúmenes `/data` ya existentes al
   actualizar); escaneo de la imagen en CI (`aquasecurity/trivy-action`,
   `severity: CRITICAL,HIGH`, `exit-code: 1`, **pinnado por commit SHA, no
   por tag**, tras el compromiso real de cadena de suministro de esa acción
   el 2026-03-19) en cada push/PR más un rebuild programado semanal, para
   que actualizaciones de seguridad de `apt` lleguen aunque no haya cambios
   de código — ni `govulncheck` (solo módulos Go) ni Dependabot (ecosistema
   `docker` solo sigue la línea `FROM`) ven nunca lo que `apt-get install`
   resuelve en cada build; contención de recursos por cgroup para todo el
   árbol de procesos (`--memory`/`--cpus` en Docker, `MemoryMax`/`CPUQuota`
   en `deploy/systemd/nexuscloud.service` para bare-metal) — mecanismo real
   del kernel, no reinventado desde Go. Ver `docs/deployment.md` para los
   comandos exactos.

6. **Los dos hallazgos más graves del pase de `security-reviewer` sobre el
   diseño**, incorporados sin excepción:
   - **CRÍTICO — cero panic recovery en todo el proyecto.** Un panic de
     decodificación (índice fuera de rango, división por cero — el tipo de
     bug que decodificadores de formatos complejos producen ante entradas
     malformadas) tumbaría el proceso ENTERO para todos los inquilinos,
     disparado automáticamente por el job best-effort sin que nadie llame a
     ningún endpoint. Tres arreglos, cada uno cubre un caso que los otros
     dos no alcanzan: `r.Use(middleware.Recoverer)` como PRIMER middleware
     en `internal/api/v1/router.go` (mejora general del servidor, no solo
     de miniaturas — confirmado con
     `TestRecovererMiddlewareEvitaQueUnPanicTumbeElServidor`); `recover()`
     explícito dentro de la goroutine de decodificación con timeout
     (`GenerateImageThumbnail`, confirmado con
     `TestGenerateImageThumbnail_RecuperaPanic` vía indirección de función
     inyectable); `recover()` explícito dentro de `runOnce()` del bucle en
     segundo plano (una goroutine de fondo desde el arranque, sin ningún
     wrapper HTTP que la proteja jamás — confirmado con
     `TestStartThumbnailLoopSobrevivePanic`, un `ThumbnailJobRepository`
     fake que paniquea en `NextPending`).
   - **ALTO — sin límite de concurrencia, el rate limiter general no
     protege nada aquí.** `apiLimiter` admite ráfagas de hasta
     `apiPerMinute` (300 por defecto) peticiones instantáneas — pedir la
     miniatura de 50 vídeos grandes nunca vistos a la vez (cualquier
     usuario normal, sin privilegios) cae muy por debajo de ese burst.
     Combinado con el cgroup compartido por todo el árbol de procesos, esto
     podía hacer que el OOM-killer matara al proceso PRINCIPAL, no solo a
     los hijos ffmpeg. Arreglo: un semáforo de concurrencia global
     (`thumbnails.maxConcurrentGenerations`, 4 por defecto) compartido por
     los TRES pipelines (imagen incluida — también sufre la fuga de
     memoria del punto 4), combinado con `singleflight.Group`
     (`golang.org/x/sync`, ya indirecta antes de esta fase — promoverla a
     directa no añade proveedor nuevo a la cadena de suministro) para no
     generar dos veces la misma miniatura si el bucle y una petición HTTP
     coinciden sobre el mismo archivo. Semáforo lleno → `ErrThumbnailBusy`
     → `503` inmediato desde el endpoint bajo demanda (el archivo ya tiene
     su icono genérico como fallback en la web) o skip sin penalizar el job
     desde el bucle — confirmado con
     `TestGenerateOrGetThumbnail_SemaforoLleno`.

## Consecuencias

- Nueva tabla `thumbnail_jobs` (migración `0016`, 3 motores); instalaciones
  existentes no ven ningún cambio de comportamiento hasta que activen
  `thumbnails.enabled` explícitamente (`false` por defecto — primera vez
  que el servidor decodifica Y ejecuta binarios externos sobre contenido de
  usuario, reforzado por el cambio de imagen Docker).
- Imagen Docker más grande y con más paquetes que auditar
  (`debian:12-slim` + ffmpeg/poppler-utils/ca-certificates en vez de
  distroless) — coste real, compensado según el punto 5 de la Decisión,
  nunca presentado como gratis.
- `middleware.Recoverer` es una mejora que protege TODO el servidor, no
  solo el código de esta fase — nunca debió depender de que ningún handler
  paniqueara, pero hasta esta fase no había ninguna razón concreta y
  urgente para añadirlo.
- **Bug real encontrado y corregido por el propio proceso de TDD**: la
  primera versión de `GenerateOrGetThumbnail` llamaba a `UpsertPending`
  incondicionalmente, cuyo contrato es "crear O REINICIAR a
  pending/attempts=0" — cada petición bajo demanda sobre un job ya en curso
  volvía a poner `attempts` a 0, así que nunca llegaba a agotar los
  reintentos. Corregido añadiendo `GetByFileID` (lectura pura, nunca
  resetea) como paso previo, con su propio test de regresión
  (`TestThumbnailJobGetByFileIDNuncaResetea`) — no fue un parche puntual
  sobre el síntoma, fue un método nuevo con un contrato distinto y
  explícito.
- **Segundo bug real, encontrado por el test de integración HTTP de
  `GET /admin/thumbnail-jobs`**: `SQLThumbnailJobRepository.ListByStatus`
  no normalizaba `limit<=0` — `strconv.Atoi("")` (el `?limit=` ausente de
  una petición HTTP real) produce `0`, y `LIMIT 0` en SQL significa
  "cero filas", no "sin límite". El endpoint devolvía siempre una lista
  vacía aunque hubiera jobs pendientes. Mismo defecto que
  `audit.SQLRepository.ListEvents` ya evitaba (`if limit <= 0 { limit =
  100 }`) — corregido con el mismo criterio exacto, más un test de
  regresión dedicado a nivel de repositorio
  (`TestThumbnailJobListByStatusLimitCeroUsaDefecto`), no solo a nivel de
  integración HTTP donde se detectó.
- Explícitamente fuera de alcance de esta fase: sandboxing por
  namespaces/seccomp de ffmpeg/pdftoppm; eviction LRU o GC de miniaturas
  huérfanas de la caché; rlimits por-subproceso implementados a mano desde
  Go (se apoya en cgroups del SO); confirmación en vivo de CVEs recientes
  de ffmpeg/poppler/WEBP más allá de lo que cubren `govulncheck`/Trivy en
  CI. En Windows: sin `Setpgid` ni cgroups — timeout+`Process.Kill()` siguen
  aplicando, sin contención de recursos a nivel de SO más allá del gestor
  de tareas (misma asimetría ya documentada en `docs/deployment.md` para el
  hardening de systemd).
- **Verificación**: 44 tests unitarios en `internal/storage` (repositorio
  de jobs, pipeline de imagen, pipeline de vídeo/PDF, caché) + 1 fuzz test
  (89.208 ejecuciones sin panic) + 3 tests de configuración +
  1 test de `middleware.Recoverer` (`internal/api/v1`) + 1 test del
  `recover()` del bucle en segundo plano (`internal/server`) + 7 tests de
  integración HTTP end-to-end (generación bajo demanda, caché, 403/404,
  404 con `thumbnails.enabled=false`, admin-only de
  `GET /admin/thumbnail-jobs`) — 57 tests dedicados en total, sin contar
  los ya existentes que siguen en verde. Los pipelines de vídeo/PDF se
  verificaron además con `ffmpeg`/`pdftoppm` REALES: en un contenedor
  `golang:1.25-bookworm` con ambos instalados ad-hoc para los tests
  unitarios (que se saltan con gracia si el binario no está — confirmado
  en ese mismo entorno), y de extremo a extremo contra la imagen de
  PRODUCCIÓN ya construida (`debian:12-slim`, usuario `nonroot` real):
  subida de un vídeo y un PDF reales vía HTTP, `GET .../thumbnail` devolvió
  JPEG válido en ambos casos (320×320 y 320×160 respectivamente,
  preservando proporción). La miniatura de imagen se verificó además
  visualmente en un navegador real (Chrome): miniatura real para una
  imagen subida, icono genérico sin petición de red para un `.txt`. La
  imagen Docker final se escaneó con Trivy real contra el propio build:
  0 vulnerabilidades CRITICAL/HIGH en el SO Debian (295 paquetes) y en el
  binario Go. `go build`/`go vet`/`gofmt`/`go test ./...` (incluida
  `tests/integration`) en verde en todo el módulo tras cada tarea.
