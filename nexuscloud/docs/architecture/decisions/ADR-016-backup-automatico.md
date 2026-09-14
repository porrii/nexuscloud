# ADR-016: Backup automático — apagado por defecto, sin ejecución inmediata al arrancar

## Estado

Aceptado.

## Contexto

ADR-015 (Backup Manager v1) dejó "programación" (§18) explícitamente fuera de su alcance: el único modo de proteger los datos era que un administrador recordara ejecutar `nexuscloud backup run` a mano. `internal/server/server.go` ya tenía un precedente directo para "algo que se repite solo en segundo plano": `startTrashPurgeLoop`, gateado por `cfg.Trash.Enabled`, con un canal `stop` cerrado en `Server.Close()`.

## Decisión

1. **`config.BackupConfig{Enabled, IntervalMinutes}` nuevo, `Enabled` en `false` por defecto.** A diferencia de `Trash`/`Versioning`/`Sharing` (activadas por defecto porque son "gratis": solo retienen datos que ya existen, sin coste añadido), un backup automático **copia datos reales** a `cfg.BackupsDir()` — por defecto en el mismo disco que el almacenamiento principal, lo que no cumple la regla 3-2-1 (§19: "1 copia fuera del equipo principal") y consume espacio sin límite todavía (sin retención en este slice). Activarlo a ciegas por defecto daría una falsa sensación de protección. El administrador debe activarlo de forma explícita, mismo criterio que `sharing.publicLinksEnabled`/`web.enabled`.
2. **Intervalo en minutos (`IntervalMinutes`, no horas ni cron).** Mismo criterio que `AutoSyncSettings.intervalMinutes` en el cliente Flutter (Fase 3, slice 8): unidad simple, permite tanto "cada 24h" (1440, el valor por defecto cuando se activa) como intervalos cortos para verificación, sin la complejidad de sintaxis cron que §18 no exige explícitamente.
3. **Sin variable de entorno `NEXUSCLOUD_BACKUP_*` nueva.** Ni `trash.enabled` ni `versioning.enabled` ni `sharing.enabled` tienen override de entorno hoy — solo host/puerto/BD/rutas de almacenamiento lo tienen. Se mantiene ese mismo alcance de cobertura en vez de añadir uno nuevo sin precedente.
4. **`startBackupScheduleLoop` calca `startTrashPurgeLoop` con una diferencia deliberada: no se ejecuta una vez al arrancar.** Purgar la papelera al arrancar es barato e idempotente (si no hay nada expirado, no hace nada). Lanzar un backup completo automáticamente en cada arranque del servidor podría ser costoso y sorprendente si el proceso se reinicia con frecuencia (p.ej. durante actualizaciones, §59). El primer backup automático llega tras el primer intervalo completo; un administrador que quiera uno inmediato ya tiene `nexuscloud backup run`.
5. **Sin `PoolIDs`: siempre respalda todos los pools elegibles**, igual que `backup run` sin `--pool`. Restringir pools concretos ya existe como mecanismo (`storage set-policy --backup off`, ADR-015) — no hace falta un segundo.
6. **`backupManager` no se expone en `apiv1.Handlers`.** Sigue sin endpoint HTTP/UI, mismo alcance que el Slice 1 — es un consumidor interno de `internal/server`, no una superficie nueva de la API.

## Consecuencias

- Ningún dato se copia automáticamente a menos que el administrador edite `config.yaml` a propósito — coherente con que NexusCloud sea secure/inactive-by-default en toda superficie que consuma recursos o exponga algo nuevo (§3, §47, ya aplicado a `web.enabled`/`sharing.publicLinksEnabled`).
- Sin retención, un backup automático diario acumula espacio de forma indefinida — riesgo real y conocido, documentado explícitamente como el siguiente slice necesario de esta misma área (rotación/retención, §18/§173: nunca borrar el único backup sano).
- `startBackupScheduleLoop`, igual que `startTrashPurgeLoop`, no tiene test unitario dedicado: la lógica real (`backup.Manager.Run`) ya está exhaustivamente probada en `internal/backup` con SQLite+filesystem reales; el bucle en sí es un `time.Ticker` fino de bajo riesgo al calcar un patrón ya en producción. Se verificó en cambio end-to-end contra un servidor real con un intervalo corto.
- Un futuro cambio de intervalo/activación en caliente (sin reiniciar el servidor) tendría que sustituir el `time.Ticker` fijo por algo reconfigurable — no necesario hasta que alguien lo pida, mismo criterio que Trash/Versioning/Sharing hoy (todos exigen reinicio para cambiar).
