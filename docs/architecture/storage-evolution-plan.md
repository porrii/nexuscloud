# Plan de evolución de la arquitectura de almacenamiento y despliegue

> Redactado el 2026-09-10 tras el documento "ACTUALIZACIÓN DEL PROYECTO
> NEXUSCLOUD" (16 requisitos). Este documento es el paso 1 que ese documento
> exige ("analiza el estado actual… aplica los cambios de forma incremental,
> justificando técnicamente cada refactorización importante y asegurando que
> no se rompe la compatibilidad").
>
> **Progreso (2026-09-10):**
> - Fase A — config por área: **HECHA** (commit `57a80d6`).
> - Fase B — `Layout` + endurecimiento de symlinks: **HECHA** (commit `57a80d6`).
> - Fase C — enumeración de discos (solo lectura): **HECHA** (commit `c1d2626`).
> - Fase D — pools reales + refactor de `FileService`: **HECHA**
>   (commit `8dc5e47`), junto con el endpoint HTTP admin de discos.
> - Fase E (despliegue nativo): pendiente.
> - Fase F (ganchos futuros): documentada.

---

## 1. Qué se analizó

Backend Go completo, en concreto:

- `internal/config/` — `config.go` (282 líneas), `env.go`, `validate.go`.
- `internal/storage/` — `provider.go`, `local_provider.go`, `pool.go`,
  `pool_sql_repository.go`, `bootstrap.go`, `path.go`, `file_service.go`,
  y el resto de repositorios SQL.
- `internal/db/db.go`, `internal/logging/logging.go`, `internal/server/server.go`,
  `internal/cli/` (root, start, doctor, config_cmd, stubs).
- `internal/db/migrations/{sqlite,postgres}/` — esquema (`storage_pools`,
  `files.pool_id`).
- `Dockerfile`, `docker-compose.yml`, `deploy/systemd/nexuscloud.service`,
  `scripts/dev.sh`, `config.example.yaml`.
- `NEXUSCLOUD.md` §9-12 (almacenamiento, storage pools, discos, RAID) y
  §154-157 (directorios de datos, proveedor).

**Conclusión de partida:** la Fase 1 ya construyó como concepto de primera
clase casi todo lo que pide la actualización — `Provider` (abstracción de
backend), `Pool` (`storage_pools` con id/name/type/path/priority/status,
`files.pool_id` FK), config en capas con `Validate()` y `configVersion` +
`migrateSchema()`, `SafeJoin` (anti path-traversal), escritura atómica
(tmp+rename), systemd hardening, distroless nonroot. **Lo que falta es
terminar lo que está en stub, ensanchar la configuración para que cada área
de almacenamiento sea reubicable por separado, formalizar la regla de "todo
pasa por la abstracción", y añadir tooling de despliegue nativo.** Es
evolución incremental y compatible hacia atrás, no un rediseño.

---

## 2. Estado actual por requisito

Leyenda: ✅ cubierto · 🟡 parcial · ❌ ausente

| # | Requisito | Estado | Detalle |
|---|-----------|--------|---------|
| 1 | Compatibilidad con lo actual | — | Este plan es aditivo; ver §4 (fases) y §5 (riesgos). |
| 2 | Almacenamiento nunca codificado | 🟡 | `storage.dataDir` y `storage.storageDir` configurables (yaml/env). `database.dsn` configurable. **Pero** `cache/logs/backups/config/versionado/miniaturas/temporales` se **derivan** de `dataDir` sin override propio (ver `config.go`: `CacheDir()`, `LogsDir()`, `BackupsDir()`, `ConfigDir()` son `filepath.Join(dataDir, "…")` fijos). `defaultDataDir()` es sensible al SO (`ProgramData` / `/var/lib/nexuscloud`); solo `C:\NexusCloud` como último recurso si falta `%ProgramData%`. |
| 3 | Gestión avanzada de discos | 🟡→❌ | Modelo `Pool` + `PoolRepository` + `EnsureDefaultPool` + tabla `storage_pools`. **Falta**: enumerar unidades del SO, capacidad/libre/usado/sistema-de-archivos/punto-de-montaje/estado/SMART/temperatura; CLI `storage list/add` (hoy stubs "Fase 5" en `internal/cli/stubs.go`); API para el gestor. |
| 4 | Separación lógica del almacenamiento | 🟡 | Accesores separados por área en `config.go`, pero todos `dataDir`-relativos y no configurables por separado. **Faltan como áreas propias**: miniaturas, archivos temporales, ubicación del versionado. |
| 5 | Arquitectura preparada para crecer | ✅ (base) | `Provider` interface, `Pool`, `configVersion`+migración de esquema, migraciones de BD. Necesita las extensiones de §4 (fases). |
| 6 | Abstracción del almacenamiento | 🟡 | `Provider` cubre **solo el contenido de ficheros de usuario** (vía `FileService`). BD (`sql.Open` sobre ruta + `os.MkdirAll`), logging (`os.OpenFile`) y config (`os.ReadFile`) tocan el FS directamente. Defendible para infraestructura (SQLite necesita una ruta real; un logger a través de una abstracción async sería frágil), pero **nada formaliza** que los datos de usuario y derivados (miniaturas, versionado, caché de contenido) deban ir por `Provider`. |
| 7 | Configuración completamente dinámica | ✅ | Capas defaults→`config.yaml`→`NEXUSCLOUD_*`→flags. Mapa de entorno **explícito** (auditable de un vistazo, no reflection). `Validate()` rechaza config inválida. `configVersion` + `migrateSchema()`. |
| 8 | Preparado para RAID/snapshots/replicación/… | 🟡 | `Pool.Type` existe (solo `"local"`). `NEXUSCLOUD.md` §10 lista políticas por pool (utilización/backup/versionado/snapshots) que **no están** en el struct `Pool` todavía. §12 ya fija la decisión correcta: NexusCloud **no** implementa RAID propio, detecta el del SO (mdadm / Storage Spaces). |
| 9 | Despliegue flexible, Docker opcional | 🟡 | Binario nativo funciona (`nexuscloud start`), systemd unit real, `nexuscloud config init`. Docker es claramente opcional (compose con casi todo comentado; `dev.sh` usa Docker solo para *compilar* en máquinas sin Go). **Faltan**: scripts de instalación/actualización/desinstalación, servicio de Windows (`kardianos/service` "evaluado, no implementado"), empaquetado (`.deb`/`.rpm`/instalador Windows). |
| 10 | Seguridad | ✅ (fuerte) | `SafeJoin` (anti traversal, defensa en profundidad), escritura atómica, distroless nonroot, systemd `NoNewPrivileges`/`ProtectSystem=strict`/`PrivateTmp`, Argon2id, rate limiting (login/api/enlaces públicos), `Validate()` rechaza CORS `*`, tabla `audit_events`. **Gap concreto**: `SafeJoin` valida con `filepath.Abs` pero **no** resuelve symlinks (`filepath.EvalSymlinks`); un symlink creado dentro de la raíz de un pool y apuntando fuera podría escapar en lectura/escritura. Ver §5.3. |
| 11 | Rendimiento | 🟡 | No hay gap estructural. SQLite con **1 sola conexión** (decisión deliberada para evitar "database is locked" con el driver Go puro, a la escala objetivo ~100 usuarios). Escritura en streaming. Miniaturas/operaciones masivas/escaneo optimizado: no construidos aún. |
| 12 | Escalabilidad | ✅ | IDs `TEXT` (no enteros), sin límites de recuento en el esquema, sin caps artificiales. La conexión única de SQLite es un techo consciente; la ruta PostgreSQL existe para instalaciones mayores. |
| 13 | Mantenibilidad | 🟡 | Límites de paquete limpios, interfaces entre módulos. `file_service.go` (625 líneas) y `file_service_test.go` (870) son grandes pero no críticos. La refactorización de §4 fase 2 es buena ocasión para partir `FileService`. |
| 14 | Compatibilidad futura | ✅ | `CGO_ENABLED=0`, SQLite Go puro, sin código específico de plataforma todavía, driver de BD intercambiable, binario estático. |
| 15 | Flexibilidad tecnológica | ✅ | Ninguna tecnología es obligatoria sin justificación; alternativas configurables (sqlite/postgres, http/https, web on/off). |
| 16 | Objetivo final | — | Alcanzable de forma incremental; ver §4. |

---

## 3. Lo que NO hay que tocar (y por qué)

Para evitar refactors innecesarios (requisito 1: "Refactoriza únicamente
cuando aporte una mejora clara"):

- **El mapa de entorno explícito de `env.go`.** Tentador convertirlo a
  reflection genérica al crecer el número de claves, pero su valor es
  justo la auditabilidad. Se amplía a mano, entrada por entrada.
- **La conexión única de SQLite.** Es una decisión de correctness
  documentada, no un descuido. Cambiarla es un tema aparte (§11), no de
  este plan.
- **`SafeJoin` como mecanismo único.** Se refuerza (§5.3), no se sustituye.
- **La separación "BD = metadatos, FS = contenido"** (`NEXUSCLOUD.md` §9).
  Ya es correcta; ningún requisito nuevo la contradice.
- **El binario único con subcomandos cobra.** La forma del CLI (§98) ya
  reserva `storage`/`backup`/`security`. Se rellenan los stubs, no se
  reestructura el árbol.
- **`configVersion` + `migrateSchema()`.** El mecanismo de evolución de
  config ya existe; se usa, no se reinventa.

---

## 4. Plan por fases (incremental, compatible hacia atrás)

Cada fase es independientemente entregable y no rompe nada de lo anterior.
El orden va de menor a mayor riesgo/alcance.

### Fase A — Config: cada área de almacenamiento reubicable por separado
**Riesgo: mínimo. Alcance: ~1-2 archivos, <120 líneas. Sin cambio de esquema
ni de contrato de API. Compatible 100%.**

- Añadir a `StorageConfig` campos **opcionales** (`omitempty`): `databaseDir`,
  `cacheDir`, `thumbnailsDir`, `versionsDir`, `tempDir`, `logsDir`,
  `backupsDir`, `configDir`. Vacío = comportamiento de hoy exacto
  (`<dataDir>/<área>`).
- Los accesores de `config.go` (`CacheDir()`, etc.) devuelven el override
  si está, si no el valor derivado actual. Añadir `ThumbnailsDir()`,
  `VersionsDir()`, `TempDir()`.
- Añadir las `NEXUSCLOUD_*_DIR` correspondientes al mapa explícito de
  `env.go`.
- `Validate()`: si dos áreas distintas resuelven a la misma ruta y eso es
  problemático (p.ej. `tempDir` == `storageDir`), avisar; no es error salvo
  solapes peligrosos.
- `EnsureDataDirs()` crea también las áreas nuevas.
- `config.example.yaml` documenta los campos nuevos como "vacío = por
  defecto; rellena solo si quieres esa área en otro disco".
- **No** requiere subir `configVersion` (solo campos nuevos opcionales).
- Actualiza `deploy/systemd/nexuscloud.service`: comentario recordando
  añadir cada área reubicada a `ReadWritePaths=`.

### Fase B — `internal/storage/paths` o `internal/storage/layout`: un único lugar que resuelve rutas
**Riesgo: bajo. Alcance: nuevo paquete + adaptar llamadores. Sin esquema.**

- Un tipo `Layout` construido una vez desde `*config.Config` que expone
  `UserDataRoot()`, `ThumbnailsRoot()`, `VersionsRoot()`, `CacheRoot()`,
  `TempRoot()`, etc., **ya resueltos y validados** (absolutos, symlinks de
  la raíz resueltos una vez — ver §5.3).
- `server.go`, `db.go`, `logging.go` reciben rutas **de `Layout`**, no
  calculan `filepath.Join` por su cuenta. Formaliza el requisito 6 sin
  meter la BD/logging por un `Provider` async (que sería frágil): la regla
  pasa a ser *"nadie calcula rutas de almacenamiento a mano; se piden a
  `Layout`; el contenido de usuario y derivados van además por `Provider`"*.
- Test: `Layout` con overrides mixtos + rutas relativas + symlinks.

### Fase C — Gestor de discos (solo lectura): enumerar unidades
**Riesgo: medio (primer código específico de plataforma). Alcance: nuevo
subpaquete + CLI + endpoint admin.**

- Nuevo `internal/storage/diskinfo` con interfaz `Enumerator` y
  adaptadores por build tag: `enumerator_linux.go` (parsear `/proc/mounts`
  + `syscall.Statfs`), `enumerator_windows.go` (`golang.org/x/sys/windows`:
  `GetLogicalDrives` + `GetDiskFreeSpaceEx` + `GetVolumeInformation`),
  `enumerator_stub.go` (otros SO: devuelve "no soportado", no rompe el
  build — requisito 14).
- Campos: nombre, punto de montaje, sistema de archivos, capacidad total,
  libre, usado, estado. SMART/modelo/serie/temperatura: **best effort**,
  vía `smartctl` si está en PATH (opcional, degradación limpia si no).
  Nunca depender de una única API (`NEXUSCLOUD.md` §11).
- Rellenar `internal/cli/stubs.go` → `nexuscloud storage disks` (tabla) y
  `nexuscloud storage list` (pools). Quitar el `notImplementedYet` de esas
  dos.
- Endpoint admin `GET /api/v1/admin/storage/disks` (solo admin, solo
  lectura). Sin exponer serie/SMART salvo config explícita (§11 "cuando sea
  seguro y apropiado").

### Fase D — Pools de verdad: crear/editar/desactivar + políticas
**Riesgo: alto (toca `FileService` y el esquema). Alcance: migración +
refactor del constructor de `FileService` + CLI + API.**

- `PoolRepository` gana `UpdatePool`, `SetPoolStatus`, `DeletePool` (este
  último solo si no hay `files.pool_id` apuntando — el FK ya es
  `ON DELETE RESTRICT`, así que es seguro).
- `Pool` gana políticas (`NEXUSCLOUD.md` §10): `utilizationPolicy`
  (fill / round-robin / manual), `backupPolicy`, `versioningPolicy`,
  `snapshotPolicy` — como columnas nuevas nullable (migración `0006`,
  `DEFAULT` = comportamiento de hoy). **Compatible**: pools existentes
  quedan con la política por defecto.
- **Refactor real de `FileService`**: hoy `server.go` construye **un**
  `LocalFilesystemProvider` desde `pool.Path` y lo inyecta. Para multi-pool
  hace falta un `ProviderResolver` (`provider(ctx, poolID) (Provider, error)`,
  con caché) inyectado en su lugar. Cambia la firma de
  `storage.NewFileService(...)`. Todos los sitios que resuelven contenido
  (`Read`/`Write`/`Move`/`Delete`/versionado) piden el provider del
  `pool_id` de la fila, no de un provider global.
- CLI `storage add/remove/enable/disable/set-policy`. API admin equivalente.
- Migración de datos: **ninguna forzosa**. El pool "default" sigue siendo
  el de mayor prioridad activo; los `files` existentes ya llevan su
  `pool_id`.

### Fase E — Despliegue nativo de primera clase
**Riesgo: bajo-medio. Alcance: scripts + servicio Windows + docs. No toca
la lógica de la app.**

- `deploy/scripts/`: `install.sh` / `update.sh` / `uninstall.sh` (Linux:
  copiar binario a `/usr/local/bin`, crear usuario de servicio, instalar la
  unit, `nexuscloud config init` en `/etc/nexuscloud/`, `migrate up`).
  Equivalentes `.ps1` para Windows.
- Servicio de Windows: integrar `kardianos/service` en un subcomando
  `nexuscloud service install|uninstall|start|stop` (aditivo; el modo
  foreground actual sigue siendo válido — requisito 9).
- Empaquetado opcional (no bloqueante): `nfpm` para `.deb`/`.rpm`.
- `docs/deployment.md`: matriz de métodos (nativo / systemd / Docker /
  Compose / OCI) dejando claro que **nativo es plenamente soportado** y
  Docker es una comodidad, no un requisito.
- La config (almacenamiento/puertos/certs/BD/caché/logs/usuarios/permisos)
  ya es independiente del método de despliegue — solo hay que documentarlo.

### Fase F — Ganchos para el futuro (documentación)
**Riesgo: mínimo (solo interfaces/campos reservados).**

#### Contrato para añadir un backend de almacenamiento

`internal/storage/provider.go` define `Provider`; cualquier backend nuevo
(S3-compatible, "NexusCloud remoto", etc.) solo tiene que implementar esa
interfaz y registrarse en el `ProviderResolver` que introduzca la Fase D.
No debe tocar `FileService`. Un provider nuevo tiene que cumplir:

- **`Write`**: atómico visto desde fuera — un fallo a mitad no puede dejar
  un objeto parcial visible en `relPath`. Devuelve `size` y `sha256Hex`
  calculados sobre lo realmente escrito (no sobre el `io.Reader` de
  entrada). `LocalFilesystemProvider` lo consigue con tmp + rename; un
  backend de objetos usaría subida multiparte + commit.
- **`Move`**: atómico cuando la plataforma lo permita; es la operación que
  usa el historial de versiones para apartar contenido sin copiarlo. Si el
  backend no tiene move atómico real (p.ej. S3), copy+delete es aceptable
  siempre que el destino no sea visible hasta completarse.
- **Rutas lógicas**: `relPath` siempre con `/` como separador, relativo a
  la raíz del pool. El provider es responsable de su propia protección
  frente a escapes (traversal y symlinks): `SafeJoinResolved` /
  `ResolveRoot` de `path.go` son la referencia para uno local; uno remoto
  normaliza y rechaza `..` antes de construir la clave del objeto.
- **`ctx`**: respetar la cancelación en operaciones largas (`Write`/`Read`
  de ficheros grandes).

#### Puntos de extensión sin reestructuración

- **Cifrado por volumen / compresión / deduplicación**: el sitio natural es
  un `Provider` decorador (`EncryptingProvider{inner Provider}`,
  `CompressingProvider{...}`) que envuelve a otro. `FileService` no se
  entera. La clave/algoritmo saldría de la política del `Pool`.
- **Snapshots / replicación**: columnas nullable en `storage_pools`
  (Fase D las deja reservadas) + un servicio aparte que las lee; ninguna
  lógica en la ruta de lectura/escritura de ficheros.
- **Balanceo entre discos de un pool**: `Pool.utilizationPolicy` (Fase D) +
  un `ProviderResolver` que, para un pool multi-disco, elige el disco al
  crear cada fichero. El `pool_id` de `files` ya existe; haría falta un
  `disk_id` (o guardar la ruta absoluta resuelta) — migración aditiva.

---

## 5. Incompatibilidades y riesgos concretos

### 5.1 Firma de `storage.NewFileService(...)` (Fase D)
Hoy recibe un `provider` único. Multi-pool obliga a un `ProviderResolver`.
Es el único cambio de contrato interno real del plan. Mitigación: hacerlo
en su propia fase, con los tests de `file_service_test.go` (870 líneas) como
red; el resolver por defecto para 1 pool se comporta idéntico a hoy.

### 5.2 Migraciones de esquema
Fase D añade columnas a `storage_pools` (migración `0006`, ambas variantes
sqlite/postgres). Todas con `DEFAULT` = comportamiento actual. `migrate up`
es no destructivo por diseño. `configVersion` **no** sube en ninguna fase
(solo se añaden campos opcionales de config).

### 5.3 `SafeJoin` y symlinks (requisito 10) — RESUELTO (Fase B, commit `57a80d6`)
`SafeJoin` neutralizaba `../` y rutas absolutas pero no symlinks: un symlink
creado **dentro** de la raíz de un pool y apuntando fuera escaparía en
lectura/escritura. Implementado:
1. `ResolveRoot` canoniza la raíz una vez (`Abs` + `EvalSymlinks`) al
   construir el provider — la raíz sí puede ser un symlink legítimo
   (`storageDir` → disco montado).
2. `SafeJoinResolved` recorre el ancestro existente más profundo del
   resultado y comprueba que su forma canónica sigue bajo la raíz;
   **rechaza cualquier componente symlink**, apunte dentro o fuera
   (`FileService` nunca crea symlinks). Cubre también el caso del symlink
   con destino inexistente (fichero nuevo tras un symlink que escapa).
Efecto secundario esperado y deseado: symlinks internos "legítimos" dejan
de funcionar dentro del almacenamiento; es exactamente lo que el requisito
pide bloquear.

### 5.4 Código específico de plataforma (Fase C) — RESUELTO (commit `c1d2626`)
Primer `_linux.go` / `_windows.go` del proyecto (`internal/storage/diskinfo`).
`enumerate_other.go` con `//go:build !linux && !windows` compila siempre y
devuelve `ErrUnsupported`. Verificado con `GOOS=windows go build` y
`GOOS=darwin go build`. El core no depende de la plataforma; el adaptador
es opcional (requisito 14).

### 5.5 `x/sys/windows` como dependencia — RESUELTO sin coste (commit `c1d2626`)
`golang.org/x/sys` ya estaba en `go.sum` como dependencia indirecta (la
trae `modernc.org/sqlite`). La Fase C solo la promociona a directa en
`go.mod`; no se añade ningún módulo nuevo. El adaptador Windows
cross-compila; su runtime no se ha podido probar (no hay entorno Go de
Windows en esta máquina).

### 5.6 Interacción con el cliente Flutter
Ninguna. El cliente habla con la API HTTP; nada de esto cambia contratos
existentes de `/api/v1`. Los endpoints nuevos son `admin`-only y aditivos.

---

## 6. Qué NO hace este plan

- No implementa RAID (correcto: `NEXUSCLOUD.md` §12 — se detecta el del SO).
- No cambia el motor de BD ni la estrategia de conexión de SQLite.
- No toca la superficie `/api/v1` existente.
- No añade dependencia de Docker a ninguna funcionalidad.
- No reescribe `FileService` entero — solo su forma de obtener el provider
  (Fase D).
- No toca el cliente Flutter.

---

## 7. Orden recomendado y estado

| Fase | Riesgo | ¿Bloquea a? | Estado |
|------|--------|-------------|--------|
| A — config por área | mínimo | — | **hecha** (`57a80d6`) |
| B — `Layout` + symlink fix | bajo | C, D | **hecha** (`57a80d6`) |
| C — gestor de discos (RO) | medio | — | **hecha** (`c1d2626`) + endpoint HTTP (`8dc5e47`) |
| D — pools reales + `FileService` | alto | E parcial | **hecha** ([plan](storage-phase-d-plan.md), commit `8dc5e47`) |
| E — despliegue nativo | bajo-medio | — | pendiente |
| F — ganchos futuros | mínimo | — | documentado arriba |

**Endpoint HTTP de discos:** `GET /api/v1/storage/disks` (admin-only, bajo
`RequireAdmin`), añadido en `8dc5e47`. Ruta sin segmento `/admin/` para
seguir la convención existente (`/users`, `/audit`, ... ya son admin-only
sin prefijo). 501 con cuerpo claro en un SO sin adaptador de `diskinfo`.

**Fase E — despliegue nativo (pendiente):** `deploy/scripts/` con
`install.sh`/`update.sh`/`uninstall.sh` (+ `.ps1` para Windows), subcomando
`nexuscloud service install|...` con `kardianos/service` (dependencia
nueva), `docs/deployment.md` con la matriz de métodos. No toca la lógica de
la app. Se puede hacer en paralelo con cualquier otra cosa.

---

## Apéndice — Estado del slice 11 (instalador MSIX del cliente), sin relación con este plan

Aparcado. El empaquetado MSIX del cliente Flutter funciona y produce un
`.msix` con manifiesto correcto (`runFullTrust`, `internetClient`, identidad
`Porrii.NexusCloud`), instalable sin elevación con Modo de desarrollador
(`Add-AppxPackage -Register`). La verificación visual en runtime quedó
bloqueada por un fallo de compositado gráfico de la sesión de fondo que
afecta también a la config conocida-buena (`flutter run --no-enable-impeller`)
— no a los cambios. El cambio nativo de `main.cpp` (`set_impeller_switch`) se
revirtió por no poder verificarse. Cambios sin commitear: `client/pubspec.yaml`
(dev-dep `msix` + `msix_config`), `.gitignore` (patrones de certificado),
`client/windows/runner/resources/app_icon_512.png` (nuevo).
