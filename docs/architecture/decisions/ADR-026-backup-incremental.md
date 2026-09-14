# ADR-026: Backup Incremental por Deduplicación con Hardlinks (§18/§173)

## Estado

Aceptado.

## Contexto

El Backup Manager solo hacía backup completo: cada `backup run` releía y recopiaba TODOS los ficheros activos, aunque no hubieran cambiado desde el backup anterior. Para instalaciones con backup automático diario (ADR-016) y árboles grandes, esto desperdicia tiempo y espacio de forma creciente.

## Decisión

1. **Manifiesto siempre completo + deduplicación por hardlink, NO una cadena de incrementales.** Se consideraron dos enfoques: (A) cada backup mantiene un manifiesto completo (todos los ficheros activos), pero un fichero sin cambios se enlaza (`os.Link`) al backup anterior en vez de recopiarse; (B) cada backup posterior al primero solo registra lo que cambió, y restaurar exige recorrer una cadena completa en orden. Se eligió (A): con (B), `Restore`/`RestoreToPool`/`Verify` dejarían de poder operar sobre un job aislado, y borrar un eslabón intermedio (retención, ADR-017) rompería todo lo posterior. Con (A), cada job sigue siendo completo e independientemente restaurable exactamente como antes -- los otros tres métodos no necesitan saber que el modo incremental existe, cero cambios en ellos.
2. **Comparación por SHA-256 ya conocido, sin E/S extra.** `FileMeta.SHA256` del archivo activo (ya en la base de datos) se compara contra el SHA-256 que el backup completado más reciente en el MISMO destino registró para ese mismo fichero lógico (`poolID+ownerID+parentPath+name`) en su propio `manifest.json`. Sin backup anterior, o si su manifiesto no se puede leer, la comparación degrada a "nada coincide" -- se comporta como un backup completo normal, sin rama especial para "primera vez".
3. **`os.Link` con fallback a copia completa, nunca un fallo duro.** Si el enlace falla (otro filesystem/dispositivo, el fichero del backup anterior ya no está, el filesystem no soporta hardlinks), se cae a la ruta de copia normal -- la incrementalidad es una optimización de espacio/tiempo, nunca una condición de la que dependa el éxito del backup.
4. **`RunOptions.Incremental`/`BackupConfig.Incremental`, ambos `false` por defecto.** Opt-in explícito, mismo criterio que el resto de capacidades de backup (§19, secure/simple by default). CLI: `backup run --incremental`.
5. **Ningún cambio de esquema.** El manifiesto ya tenía todo lo necesario (SHA-256 por fichero); ningún campo nuevo en `backup_jobs` ni migración.

## Trade-off, con la misma franqueza que el resto de esta serie

Dos backups que comparten un fichero sin cambios comparten literalmente el mismo inodo. Un bitrot en ese inodo afectaría a AMBOS backups a la vez, no a uno solo -- el precio habitual de cualquier deduplicación por referencia compartida (el mismo principio detrás de `rsync --link-dest`, Time Machine, o la deduplicación por chunks de Borg/restic). Mitigado por `backup verify` (sin cambios, ya existente): relee y rehashea el contenido real en la ruta de cada job sin que le importe si esa ruta es una copia propia o un enlace -- detecta corrupción igual en ambos casos, así que un `verify` periódico sigue siendo la defensa real contra esto, igual que ya lo era antes de este ADR.

## Hallazgo colateral: bug latente en `Run` con cero ficheros elegibles

Al verificar esta feature (backup de un pool sin ningún archivo activo -- un caso de borde nunca antes ejercitado en la suite existente), `Run` fallaba al escribir `manifest.json` porque `jobDir` nunca se creaba explícitamente: dependía por completo de que `copyVerified` lo hiciera como efecto secundario al respaldar el primer fichero. Corregido con un `os.MkdirAll(jobDir, ...)` explícito al principio de `Run`, con test de regresión (`TestRunSucceedsWithZeroEligibleFiles`). No relacionado con el resto de este ADR salvo en que el mismo trabajo de verificación lo destapó.

## Consecuencias

- Nuevo flag `--incremental` en `backup run`; `backup.incremental` en la config para el modo automático (ADR-016).
- `pruneOldBackups` (ADR-017) se refactorizó para reutilizar un nuevo helper `completedJobsAtDestination`, sin cambio de comportamiento -- el mismo helper localiza el backup de referencia para el modo incremental.
- 3 tests nuevos en `internal/backup`: sin backup previo se comporta como completo; un escenario con un fichero sin cambios (enlazado, confirmado con `os.SameFile` que comparte inodo -- no solo que el contenido coincide), uno modificado (recopiado, inodo distinto) y uno nuevo (copiado) entre dos backups incrementales consecutivos; y el enlace fallando (fichero del backup anterior borrado a mano) recopia en vez de abortar el job.
- **Verificado E2E contra un contenedor Docker real**: dos `backup run --incremental` consecutivos sobre un archivo real sin cambios comparten inodo de verdad en el filesystem del contenedor (confirmado con un binario auxiliar `os.SameFile` cross-compilado y ejecutado dentro del contenedor, ya que la imagen distroless no tiene `stat`/`find`/shell). Un tercer backup tras modificar el archivo confirmó inodo distinto y contenido correctamente actualizado.
