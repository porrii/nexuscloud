# ADR-024: Retención del Historial de Versiones por Antigüedad y Espacio (§15)

## Estado

Aceptado.

## Contexto

ADR-007 dejó el límite de versiones por fichero con una única política: cantidad (`versioning.maxVersionsPerFile`). Faltaban, documentadas explícitamente como "fuera de esta pasada", dos políticas más: antigüedad (¿cuánto tiempo merece la pena conservar una versión?) y espacio total ocupado por el historial de un fichero (protección de disco independiente de cuántas versiones haya).

## Decisión

1. **`VersioningConfig` gana `MaxVersionAgeDays int` y `MaxVersionsTotalSizeBytes int64`** (`0` = sin límite cada una, mismo idioma que `MaxVersionsPerFile`).
2. **Las tres políticas componen como "el más restrictivo gana"** -- se purga una versión si CUALQUIER política activa lo pide, no solo si todas coinciden. Mismo motivo que ADR-023 (papelera) y misma inversión deliberada frente a la retención de backups (ADR-017, "el más generoso gana"): estas tres políticas son límites de protección de espacio en disco, no una promesa de cuánto historial conservar. La prueba concreta de por qué la composición generosa de ADR-017 sería un error aquí: con `maxVersionsPerFile=10` y `maxVersionAgeDays=90`, un fichero editado 50 veces en una tarde vería las 50 versiones conservadas bajo la composición generosa (todas caben dentro de 90 días) -- el límite de cantidad, pensado exactamente para este caso, quedaría anulado de hecho.
3. **`enforceMaxVersions` recorre `ListVersions` (ya viene `version_num DESC`, más nueva primero) acumulando bytes según avanza.** Por cada versión, en el índice `i`: se purga si `i >= maxVersionsPerFile` (cuando está activo), o si es más antigua que el corte de `maxVersionAgeDays` (cuando está activo), o si el acumulado de bytes hasta esa versión supera `maxVersionsTotalSizeBytes` (cuando está activo). Recorrer de más-nueva a más-antigua es lo que hace que el acumulado tenga sentido: conserva lo reciente hasta agotar el presupuesto, descarta el sobrante más antiguo -- recorrer al revés podaría lo reciente por error (mismo cuidado documentado en ADR-023 para la papelera).
4. **Validación**: ambos campos nuevos rechazan valores negativos; `0` es válido (sin límite), mismo criterio que el resto de límites opcionales del proyecto.
5. **`NewFileService` gana los dos parámetros nuevos** (`maxVersionAgeDays int, maxVersionsTotalSizeBytes int64`), mismo estilo posicional ya usado en su firma -- no se introdujo un struct de opciones nuevo, cambio fuera del alcance de este slice.

## Consecuencias

- Todos los call sites de `NewFileService` (servidor, tests de `internal/storage` e `internal/backup`) actualizados con los dos parámetros nuevos (`0, 0` donde no se ejercitan, preservando el comportamiento anterior exacto).
- 4 tests nuevos en `internal/storage`, con un helper `createSyntheticVersion` (inserta una fila de historial con `CreatedAt` controlado, saltándose `Upload` -- que siempre usa `time.Now()` -- mismo criterio que `createSyntheticCompletedJob` en `internal/backup` para backdatar backups): antigüedad sola, espacio solo, y las dos mitades de "el más restrictivo gana" (antigüedad poda pese a espacio de sobra; espacio poda pese a antigüedad de sobra). Contenido físico real en cada versión sintética (mismo `LocalFilesystemProvider` y mismo layout que `snapshotVersion`), así que la purga opera sobre ficheros reales, no claves huérfanas.
- **Verificado E2E contra un servidor real**: con `maxVersionsTotalSizeBytes` ajustado a un valor deliberadamente pequeño, dos subidas reales al mismo path (creando una versión de 21 bytes seguida de contenido nuevo) purgaron la versión superada inmediatamente al no caber en el presupuesto -- confirma que la configuración YAML llega intacta hasta `enforceMaxVersions` en un binario real, no solo en los tests unitarios de Go.

## Referencias

- [ADR-007: Versionado](ADR-007-versioning.md)
- [ADR-023: Retención de la Papelera por Tamaño Total](ADR-023-papelera-retencion-tamano.md) -- mismo criterio de composición y mismo motivo.
- [ADR-017: Retención de Backups](ADR-017-retencion-backups.md) -- la composición inversa ("el más generoso gana"), y por qué es correcta ahí pero no aquí.
