# ADR-015: Backup Manager v1 — backup manual, manifiesto en disco, restauración a carpeta elegida

## Estado

Aceptado.

## Contexto

§18 exige un Backup Manager independiente (automático/manual/incremental/completo/programación/retención/destino configurable/verificación/logs/restauración); §173 exige poder cifrar y nunca purgar backups sanos solo porque exista uno nuevo; §12/§17 exigen detectar RAID/snapshots del sistema operativo en vez de implementarlos. Es una fase enorme (Fase 5): esta ADR cubre solo su primer slice, el cimiento más valioso y verificable — backup manual y completo, a una carpeta local, con verificación de integridad y restauración.

El terreno ya estaba parcialmente preparado desde fases anteriores:
- `internal/cli/stubs.go` reservaba la forma exacta del CLI desde la Fase 1: `nexuscloud backup run|list|restore` (§98).
- `storage.Pool` ya tenía un campo `BackupPolicy` (`inherit`/`on`/`off`, migración `0006_pool_policies`) sin ningún código que lo leyera.
- `StorageConfig.BackupsDir`/`cfg.BackupsDir()` ya reservaban `<DataDir>/backups` como destino por defecto.
- `docs/architecture/storage-evolution-plan.md` ya fijaba la decisión de no implementar RAID propio.

## Decisión

1. **Una fila de BD por invocación de `backup run`, nunca por fichero.** `backup_jobs` (migración `0007_backup_jobs`, sqlite+postgres) guarda `id/status/destination_path/pool_ids_json/file_count/total_bytes/started_at/finished_at/error_message`. El detalle por fichero (propietario/ruta/nombre/tamaño/sha256) vive en un **`manifest.json` autocontenido** dentro de `<destino>/<job-id>/`, no en la base de datos — un backup debe poder inspeccionarse y restaurarse aunque la base de datos de origen deje de estar disponible, que es justo el escenario que un Backup Manager existe para cubrir. El manifiesto se escribe una única vez, al terminar con éxito: nunca queda a medias.

2. **Semántica de `BackupPolicy`**: `inherit` y `on` se incluyen en el backup; solo `off` excluye — incluso si el pool se pidió explícitamente por `--pool` (una exclusión explícita es una señal más fuerte que pedirlo por nombre). Sin esto, un pool nuevo (que nace en `inherit`) quedaría sin ninguna copia de seguridad hasta que alguien lo configurara a mano, contradiciendo el espíritu de §58 (advertir cuando no hay estrategia de backup).

3. **Layout del backup por IDs físicos**: `<destino>/<job-id>/data/<pool-id>/<owner-id>/<ruta>/<nombre>`, calcando el `physicalPath()` que `FileService` ya usa internamente para aislar a cada propietario en el filesystem real. Restaurar es entonces una copia directa sin ninguna traducción de rutas. Se prefirió a organizarlo por nombres legibles (pool/usuario) para no depender de resolver usernames en el momento del backup ni decidir qué pasa si un usuario se renombra entre dos backups; el `manifest.json` documenta a qué pool/usuario corresponde cada ID para quien necesite inspeccionar el backup a mano.

4. **`Run` es todo-o-nada; `Restore` no lo es.** Si cualquier fichero falla al respaldarse (lectura, escritura, hash que no coincide con `FileMeta.SHA256`), el job entero se marca `failed` con el fichero exacto en el error y `manifest.json` nunca se escribe — así `Restore` jamás puede operar sobre un backup incompleto, porque ni siquiera puede encontrar su manifiesto. La carpeta parcial en disco no se borra (queda como evidencia para depurar). `Restore`, en cambio, intenta recuperar todos los ficheros que pueda: un solo fichero con bitrot en el propio disco de backup no debe privar al usuario de recuperar el resto — los fallos se acumulan (`errors.Join`) y se devuelven juntos, nunca en silencio. Ambas rutas verifican SHA-256 contra lo ya conocido (el de `FileMeta` al respaldar, el del manifiesto al restaurar) antes de dar cualquier copia por buena, y borran el fichero de destino si no coincide.

5. **`Restore` extrae a una carpeta elegida por el usuario; no reinserta en un pool activo ni toca la base de datos.** Recuperar un pool completo con sus metadatos (conflictos de nombre, IDs nuevos, propietarios que ya no existan) es un slice futuro dedicado, deliberadamente más grande y más arriesgado que este primer paso.

6. **Solo CLI en este slice**, sin endpoint HTTP ni UI web — mismo orden que Storage Pools en la Fase D (CLI primero, API/UI cuando haga falta). `internal/backup` no toca la ruta de lectura/escritura normal de `FileService`: lee a través del mismo `ProviderResolver`/`FileRepository` que ya usa el resto de `internal/storage`.

## Consecuencias

- Un `backup_jobs` grande no multiplica filas de BD (cientos de miles de ficheros en un manifiesto JSON son baratos de leer/escribir; lo serían mucho menos como filas).
- Ningún cambio de contrato en `FileService`/`Provider`: `ListFilesByPool` (nuevo método de `FileRepository`) es el único añadido a una interfaz ya existente, un `SELECT` más junto a `ListFiles`.
- Quedan explícitamente fuera de este slice, para ADRs/slices futuros: RAID (§12) y snapshots (§17) — detección del SO; backup incremental, cifrado; otros destinos como "otro servidor NexusCloud" (SMB/NFS ya funciona hoy montado como carpeta local, sin código especial); alertas (§58), dashboard (§56) y documentar la regla 3-2-1 (§19). Programación/automático (slice 2, ADR-016), retención (slice 3, ADR-017) y un comando `backup verify` dedicado (slice 4, re-verifica sin restaurar) ya se implementaron en slices posteriores.
- Un futuro "restaurar directamente a un pool" tendrá que decidir conflictos de nombre y propietarios inexistentes; el `manifest.json` de este slice ya contiene toda la información (owner_id/parent_path/name/sha256) que esa función necesitará, así que no hace falta ningún cambio retroactivo al formato para soportarlo después.
