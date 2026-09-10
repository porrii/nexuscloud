# Fase D — Storage Pools reales (CRUD + resolución de Provider por pool)

> Plan detallado de la Fase D del
> [plan de evolución de almacenamiento](storage-evolution-plan.md).
> **HECHA** (commit `8dc5e47`), incluido el endpoint HTTP de discos que
> quedaba pendiente de la Fase C. Redactado el 2026-09-10 tras terminar las
> fases A, B y C (commits `57a80d6`, `c1d2626`); aprobada e implementada el
> mismo día. Se implementó tal cual salvo un detalle: los comandos del CLI
> aceptan también el NOMBRE del pool, no solo su id.

---

## 1. Problema que resuelve

Hoy `storage_pools` existe como tabla (desde la migración `0001`) y
`files.pool_id` guarda a qué pool pertenece cada fichero, pero **todo el
I/O físico pasa por un único `Provider`** construido en `server.go` a partir
de la ruta del pool por defecto. Si existieran dos pools, los ficheros con
`pool_id = B` se escribirían igualmente en el directorio del pool A. No hay
forma de crear, editar, desactivar ni borrar pools (el CLI `storage
list/add` son stubs).

La Fase D hace que multi-pool **funcione de verdad**: cada pool tiene su
propio `Provider`, y se pueden gestionar pools desde el CLI. La política de
"qué pool recibe un fichero nuevo" sigue siendo "el pool activo de mayor
prioridad" (`DefaultPool`) — el balanceo real entre discos queda para una
fase posterior; la Fase D solo deja el esquema, el CRUD y la resolución
listos.

## 2. Alcance

**Dentro:**
- `ProviderResolver`: resuelve y cachea un `Provider` por `pool_id`.
- `FileService` deja de recibir un `Provider` único y recibe el resolver.
- Migración `0006_pool_policies` (sqlite + postgres): 4 columnas nuevas en
  `storage_pools`, todas con `DEFAULT` = comportamiento actual.
- `Pool` gana los campos de política; `SQLPoolRepository` gana
  `UpdatePool`, `SetPoolStatus`, `DeletePool`.
- CLI real: `storage list`, `storage add`, `storage enable`,
  `storage disable`, `storage set-policy`.

**Fuera (fases posteriores, con su propia aprobación):**
- Balanceo automático entre discos de un pool (`utilization_policy` se
  guarda pero solo se honra `fill`).
- Endpoint HTTP admin de pools/discos (cambio de superficie de API).
- Mover ficheros ya existentes de un pool a otro (migración de datos).
- Snapshots / replicación reales (solo se reservan las columnas).
- Tipos de pool no locales (S3, remoto): el resolver los rechaza con un
  error claro; el hueco de diseño queda documentado en
  `storage-evolution-plan.md` §Fase F.

## 3. Diseño

### 3.1 Migración `0006_pool_policies`

`internal/db/migrations/{sqlite,postgres}/0006_pool_policies.{up,down}.sql`

```sql
-- up (idéntico en ambos motores; ALTER ... ADD COLUMN con DEFAULT es
-- no destructivo y compatible con SQLite >= 3.35 / modernc.org/sqlite 1.58)
ALTER TABLE storage_pools ADD COLUMN utilization_policy TEXT NOT NULL DEFAULT 'fill';
ALTER TABLE storage_pools ADD COLUMN backup_policy      TEXT NOT NULL DEFAULT 'inherit';
ALTER TABLE storage_pools ADD COLUMN versioning_policy  TEXT NOT NULL DEFAULT 'inherit';
ALTER TABLE storage_pools ADD COLUMN snapshot_policy    TEXT NOT NULL DEFAULT 'none';
```
```sql
-- down
ALTER TABLE storage_pools DROP COLUMN snapshot_policy;
ALTER TABLE storage_pools DROP COLUMN versioning_policy;
ALTER TABLE storage_pools DROP COLUMN backup_policy;
ALTER TABLE storage_pools DROP COLUMN utilization_policy;
```

Valores aceptados (validados en el repositorio / CLI, no en SQL):
- `utilization_policy`: `fill` (único honrado en D), `round-robin`, `manual`.
- `backup_policy` / `versioning_policy`: `inherit` (usa la config global),
  `on`, `off`.
- `snapshot_policy`: `none` (único soportado en D).

`configVersion` **no** cambia (esto es esquema de BD, no de config).

### 3.2 `Pool` y `PoolRepository` (`pool.go`, `pool_sql_repository.go`)

```go
type Pool struct {
	ID        string
	Name      string
	Type      string // "local" (único soportado)
	Path      string
	Priority  int
	Status    string // "active" | "disabled"
	CreatedAt time.Time

	UtilizationPolicy string // "fill" | "round-robin" | "manual"
	BackupPolicy      string // "inherit" | "on" | "off"
	VersioningPolicy  string // "inherit" | "on" | "off"
	SnapshotPolicy    string // "none"
}

type PoolRepository interface {
	CreatePool(ctx, *Pool) error
	GetPoolByID(ctx, id) (*Pool, error)
	ListPools(ctx) ([]*Pool, error)
	DefaultPool(ctx) (*Pool, error)

	UpdatePool(ctx, *Pool) error                 // nuevo — nombre/prioridad/políticas
	SetPoolStatus(ctx, id, status string) error  // nuevo — active/disabled
	DeletePool(ctx, id string) error             // nuevo — falla si hay files/directories con ese pool_id (FK ON DELETE RESTRICT ya lo garantiza; se traduce a un error legible)
}
```

`poolSelectColumns` y `scanPoolRow` se amplían con las 4 columnas nuevas.
`CreatePool` las incluye en el `INSERT`. `EnsureDefaultPool` (bootstrap.go)
crea el pool "default" con `utilization_policy='fill'`, resto en su default.

### 3.3 `ProviderResolver` (nuevo, `provider_resolver.go`)

```go
type ProviderResolver interface {
	// For devuelve el Provider del pool indicado (lo construye la primera
	// vez y lo cachea). Concurrency-safe.
	For(ctx context.Context, poolID string) (Provider, error)
}

type poolProviderResolver struct {
	pools   PoolRepository
	factory func(root string) (Provider, error) // por defecto NewLocalFilesystemProvider; inyectable en tests
	mu      sync.Mutex
	cache   map[string]Provider
}
```

`For`:
1. `cache[poolID]` → si está, devolverlo.
2. `pools.GetPoolByID` → si `Type != "local"`, error
   `ErrUnsupportedPoolType` (claro, no un panic).
3. `factory(pool.Path)` (que ya hace `ResolveRoot` — símil Fase B).
4. Guardar en cache y devolver.

Nota de límite: si un admin cambia `pool.Path` en caliente vía CLI, el
resolver sigue con el `Provider` cacheado hasta reiniciar. Se documenta en
la ayuda de `storage` que cambiar la ruta de un pool requiere reinicio.

### 3.4 `FileService` (`file_service.go`, `file_service_sharing.go`)

- Campo `provider Provider` → `providers ProviderResolver`.
- Firma de `NewFileService(...)`: el parámetro `provider Provider` pasa a
  ser `providers ProviderResolver`. **Único cambio de contrato del plan.**
- Los 16 usos de `s.provider.X(ctx, ...)` (15 en `file_service.go`, 1 en
  `file_service_sharing.go`) pasan a:
  ```go
  p, err := s.providers.For(ctx, poolID) // poolID: pool.ID en Upload/Create;
  if err != nil { return ..., err }      //         file.PoolID / version... en Download/Delete/Restore
  ... p.X(ctx, ...)
  ```
  Es un cambio mecánico de un solo patrón; cada sitio ya tiene en contexto
  el `pool` (de `DefaultPool`) o el `FileMeta`/`FileVersion` con `.PoolID`.
- `snapshotVersion` / `pruneOldVersions` usan `existing.PoolID` (la versión
  vive en el mismo pool que el fichero).
- No cambia ninguna ruta lógica (`physicalPath`, `stagingPath`,
  `versionStoragePath`) — solo por qué `Provider` pasan.

### 3.5 `server.go`

```go
resolver := storage.NewPoolProviderResolver(poolRepo) // usa NewLocalFilesystemProvider por dentro
// EnsureDefaultPool sigue igual (ya usa layout.UserData desde la Fase B)
fileSvc := storage.NewFileService(fileRepo, directoryRepo, versionRepo, shareRepo,
	poolRepo, resolver, hasher, ...)   // era: ..., provider, hasher, ...
```
El `layout`/`NewLocalFilesystemProvider` del arranque se mantiene solo para
que el pool por defecto quede con la ruta canónica; el resolver reconstruye
su propio provider para ese pool la primera vez (barato, cacheado).

### 3.6 CLI (`storage_cmd.go`)

Todas cargan config → `db.Open` → `db.Migrate` (como `doctor`) →
`NewSQLPoolRepository`.

| Comando | Acción |
|---|---|
| `storage list` | tabla de pools: id, nombre, prioridad, estado, ruta, políticas, nº de ficheros |
| `storage add --name N --path P [--priority K]` | crea un pool `local` (valida que `P` existe y es escribible, vía un write-test como `doctor`) |
| `storage enable <id>` / `storage disable <id>` | `SetPoolStatus`. Rechaza desactivar el último pool activo |
| `storage set-policy <id> --utilization ... --versioning ... [...]` | `UpdatePool` con validación de valores |
| `storage remove <id>` | `DeletePool`; error legible si tiene ficheros |

`storage disks` (Fase C) no cambia.

## 4. Compatibilidad hacia atrás

- Instalaciones de 1 solo pool: el resolver cachea 1 provider, comportamiento
  byte a byte idéntico al de hoy.
- Filas `files`/`directories` existentes: ya llevan `pool_id`; el resolver
  lo usa tal cual. Cero migración de datos.
- Migración `0006`: solo `ADD COLUMN ... DEFAULT`; `migrate up` es no
  destructivo. `migrate down` existe y es simétrico.
- API `/api/v1`: sin cambios (el endpoint HTTP de pools queda fuera).
- Cliente Flutter: sin cambios.

## 5. Riesgos

| # | Riesgo | Mitigación |
|---|--------|------------|
| D.1 | 16 puntos de edición en `file_service*.go` | Cambio mecánico de un patrón único; `file_service_test.go` (870 líneas) + `file_service_sharing_test.go` (421) como red. Se hace en un commit revisable. |
| D.2 | `DROP COLUMN` en el `down` de sqlite | `modernc.org/sqlite` 1.58 = SQLite 3.35+, soporta `DROP COLUMN`. Se verifica con `migrate up` + `migrate down` + `up` en el contenedor antes de dar por buena la migración. |
| D.3 | Cache del resolver vs. cambio de `pool.Path` en caliente | Documentado: cambiar la ruta de un pool requiere reinicio. Alternativa (invalidar cache en `UpdatePool`) queda para después; en D el server no edita pools, solo el CLI con el server parado es el caso normal. |
| D.4 | Desactivar/borrar el pool por defecto deja el sistema sin dónde escribir | `disable` rechaza el último pool activo; `remove` falla si hay ficheros (FK). `DefaultPool` sigue eligiendo el de mayor prioridad activo. |
| D.5 | `pool.Type` distinto de `local` en una fila futura | `resolver.For` devuelve `ErrUnsupportedPoolType` explícito; no se escribe nada a un sitio equivocado. |

## 6. Pruebas

- **Unitarias nuevas** (`internal/storage`):
  - `ProviderResolver`: cachea (2 llamadas → 1 build), rechaza tipo no
    local, propaga error de pool inexistente.
  - `FileService` con un resolver de test (factory → dirs temporales por
    pool): subir a pool A, cambiar la prioridad para que B sea el default,
    subir otro fichero a B, y comprobar que **ambos** se descargan bien
    (cada uno resuelto a su pool). Borrar un fichero de A y comprobar que
    su byte-tree desaparece de A y no de B.
  - `SQLPoolRepository`: `UpdatePool`, `SetPoolStatus`, `DeletePool`
    (incluido el error con FK).
  - Migración: `up` deja las 4 columnas con sus defaults en un pool
    existente; `down` las quita; `up` de nuevo es idempotente.
- **Integración** (`tests/integration`): un caso nuevo con 2 pools
  configurados que sube/descarga en cada uno.
- `go build ./...` (+ `GOOS=windows`), `go vet`, `gofmt`, `go test ./...`
  completos en verde, en el contenedor `golang:1.25`.

## 7. Estimación de tamaño

~8 archivos de código + 4 SQL + 3 de test. `file_service.go` crece ~+50
líneas (los `For` + err checks). Ningún archivo cruza el límite de 800
líneas del proyecto (`file_service.go` pasaría de 625 a ~675).

## 8. Qué necesito para empezar

Un "apruebo la Fase D" (o cambios sobre este plan). Al ser cambio de firma
de `FileService` + migración de esquema, no se toca código sin esa
aprobación explícita, según la regla del proyecto.
