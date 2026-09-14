# ADR-023: Retención de la Papelera por Tamaño Total (§16)

## Estado

Aceptado.

## Contexto

ADR-006 dejó la purga automática de la papelera con una única política: antigüedad (`trash.retentionDays`). El propio storage.md lo documentaba desde entonces como "fuera de esta pasada". Una papelera sin límite de tamaño puede crecer sin control durante toda la ventana de retención -- especialmente con archivos grandes -- exactamente el escenario que un límite de tamaño existe para prevenir.

## Decisión

1. **`TrashConfig` gana `MaxTotalSizeBytes int64`** (`0` = sin límite, mismo idioma que el resto de límites opcionales del proyecto).
2. **Composición con `RetentionDays`: "el más restrictivo gana" -- se poda si CUALQUIER política activa lo pide -- al revés que la retención de backups (ADR-017, "el más generoso gana").** Esto no es una elección de estilo: un límite de tamaño de papelera es protección de espacio en disco, no una promesa de cuánto conservar. Con la composición de ADR-017 (podar solo si TODAS las políticas coinciden), un archivo enorme borrado ayer NUNCA se podaría por tamaño mientras no cumpliera también la antigüedad -- el límite de tamaño quedaría inútil en la práctica para justo el caso que pretende cubrir. La retención de backups, en cambio, compone de forma generosa a propósito: cantidad y antigüedad son dos formas de preguntar "¿cuánto histórico de recuperación es suficiente?", y ante la duda se prefiere conservar de más -- un objetivo genuinamente distinto.
3. **`PurgeExpiredTrash` pasa de listar solo lo ya-caducado (`ListFilesDeletedBefore`) a traer TODA la papelera (`ListAllTrashedFiles`, nuevo método de `FileRepository`, ordenado `deleted_at DESC`)** -- hace falta ver el conjunto completo para poder aplicar el voto por tamaño sobre él. Recorre de más-reciente a más-antiguo acumulando bytes; en cuanto el acumulado supera el límite, ese archivo y todos los que quedan (más antiguos aún) se marcan para purgar por tamaño, además de cualquiera que ya cumpliera la antigüedad. El orden es intencional y fácil de invertir por error: recorrer de más-antiguo a más-reciente podaría lo más NUEVO en vez de lo más viejo.
4. **Las carpetas no participan del límite de tamaño.** `ensureDirectoryEmpty` garantiza que solo se puede mover a papelera una carpeta ya vacía de contenido activo, así que una carpeta en papelera siempre pesa 0 bytes -- su purga sigue siendo puramente por antigüedad (`ListDirectoriesDeletedBefore`), sin cambios.
5. **Validación**: `trash.maxTotalSizeBytes` no puede ser negativo; `0` es válido (sin límite), mismo criterio que `backup.retentionCount`/`backup.retentionDays`.

## Consecuencias

- `PurgeExpiredTrash(ctx, retention, maxTotalSizeBytes)` gana un tercer parámetro -- todos los call sites (servidor, tests) actualizados.
- 3 tests nuevos en `internal/storage` (además del ya existente por antigüedad): purga por tamaño sola pese a retención generosísima, purga por antigüedad sola pese a límite de tamaño generosísimo -- las dos mitades de "el más restrictivo gana" -- probados con SQLite y filesystem reales, sin mocks.
- **Verificado E2E contra un servidor real (Docker, distroless)**: config con `retentionDays: 3650` (nunca se cumpliría) y `maxTotalSizeBytes: 1` byte; se subió y borró un archivo real de 10 bytes; al reiniciar el contenedor (dispara la pasada de purga de arranque) el log confirmó `"papelera purgada por retención" files=1 retention_days=3650` y la papelera quedó vacía -- la purga por tamaño se disparó pese a que la antigüedad, por sí sola, jamás la habría disparado. Confirma que la configuración YAML llega intacta hasta el bucle de purga real, no solo en los tests unitarios de Go.
