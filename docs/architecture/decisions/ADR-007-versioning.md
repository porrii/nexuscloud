# ADR-007: Versionado con staging-then-move y snapshot-then-swap

## Estado

Aceptado.

## Contexto

§15 exige historial de versiones configurable (activado/desactivado, límite de versiones por archivo) como segunda línea de defensa contra ransomware y sobrescrituras accidentales — la papelera (§16, [ADR-006](ADR-006-trash.md)) protege contra `DELETE`, pero no contra un `Upload` que reemplaza el contenido de un archivo activo con el mismo nombre: eso nunca pasaba por `Delete`, así que la papelera no lo veía.

Dos preguntas de diseño concretas:

1. **¿Cuándo decidir si hace falta versionar?** Un `Upload` no sabe si el contenido nuevo difiere del existente hasta haberlo leído completo (el hash SHA-256 se calcula en streaming, §136-137, `docs/storage.md#integridad`). Escribir directamente sobre el archivo activo mientras se calcula el hash arriesgaría perder el contenido anterior si el proceso se interrumpe a mitad de escritura, o crear una versión "vacía" para una subida que en realidad no cambia nada (el mismo archivo resubido sin cambios no debería generar ruido en el historial).
2. **¿Qué hacer al restaurar una versión antigua?** Restaurar la versión N no debe implicar perder la versión que estaba activa justo antes de restaurar — de lo contrario "restaurar" sería en sí mismo una operación con pérdida de datos, contradiciendo el propósito de tener historial.

## Decisión

1. **`Upload` escribe primero a una ubicación provisional** (`.nexuscloud-staging/<id-aleatorio>`, fuera del árbol visible del usuario) y calcula su SHA-256 ahí, sin tocar todavía el archivo activo existente (si lo hay).
2. Si existe un archivo activo en ese path **y** su hash difiere del nuevo contenido, su contenido actual se **mueve** (no se copia — `Provider.Move`, implementado como `os.Rename` en `LocalFilesystemProvider`, evita duplicar bytes en disco) a `.nexuscloud-versions/<file_id>/<version_num>` y se registra una fila en `file_versions` (migración 0004) con el `UpdatedAt` original del archivo como su `CreatedAt` — la versión "recuerda" cuándo fue realmente la actual, no cuándo dejó de serlo.
3. Si el hash coincide (resubida idéntica) o `versioning.enabled=false`, no se crea versión — el contenido en staging simplemente reemplaza al existente sin generar historial.
4. Solo entonces el contenido en staging se mueve a su destino final y se actualiza la fila de `files`.
5. **`RestoreVersion` nunca pierde datos**: antes de traer de vuelta el contenido de la versión N, el contenido *actualmente* activo se snapshotea igual que en el paso 2 de un `Upload` normal (o se descarta si el versionado está desactivado, coherente con el punto 3) — luego el contenido de la versión N se mueve a la posición activa y su fila se elimina del historial (ya no es una versión pasada, es el presente).
6. **Límite automático**: al superar `versioning.maxVersionsPerFile`, se purga la versión más antigua (contenido físico + fila) tras cada snapshot nuevo — política de limpieza puramente por conteo, sin considerar antigüedad ni tamaño total ocupado.
7. Borrar un archivo para siempre (`?permanent=true`, o purga de la papelera por retención) purga también las versiones — un historial sin el archivo al que pertenece no tiene sentido y solo ocuparía espacio.

## Consecuencias

- Ninguna ventana de pérdida de datos: en ningún punto del flujo el contenido anterior se borra antes de que el nuevo esté completo y verificado en disco (staging), ni el contenido activo se borra antes de que su snapshot esté a salvo en `.nexuscloud-versions/`.
- Coste de una escritura extra en el peor caso (staging → destino final) en vez de escribir directo — aceptado conscientemente: el coste de I/O es bajo comparado con el riesgo de corromper el único archivo activo a mitad de una subida interrumpida.
- `Move` en vez de copia mantiene el coste de versionar un archivo grande en operaciones de metadatos del filesystem (rename), no en bytes copiados — relevante para archivos grandes en discos lentos.
- Igual que en [ADR-006](ADR-006-trash.md), la política de purga es solo por conteo (`maxVersionsPerFile`); purga por antigüedad o por espacio total ocupado por el historial de un archivo queda fuera de esta pasada y documentada como tal en `docs/storage.md#versionado-15`.
- El historial de versiones vive bajo el mismo pool/propietario que el archivo activo (`.nexuscloud-versions/<owner_id>/<file_id>/...`), así que hereda el mismo aislamiento por filesystem que ya provee `SafeJoin` — no hace falta ninguna comprobación de propiedad adicional más allá de la que ya hace `FileService` sobre el `file_id`.
