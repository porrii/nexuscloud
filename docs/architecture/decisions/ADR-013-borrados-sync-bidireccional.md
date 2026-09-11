# ADR-013: Propagación de borrados en la sync bidireccional -- papelera en los dos lados, guarda anti-"borrado masivo"

## Estado

Aceptado.

## Contexto

ADR-012 (slice 13) dejó la sync bidireccional (`Ambos`) sin propagar
borrados a propósito: un archivo previamente sincronizado que desaparece de
un lado se "resucita" desde el otro. Era lo más seguro para el primer slice
bidireccional, pero significa que borrar de verdad un archivo obliga a
hacerlo a mano en los dos sitios -- justo lo contrario de lo que promete
"sincronización bidireccional".

Este slice (14) cierra el hueco, con dos condiciones explícitas del usuario:
una guarda anti-"borrado masivo" y mover a papelera en vez de un borrado
definitivo directo. Alcance: **solo el modo `Ambos`** -- `Descargar`/`Subir`
siguen sin tocar nada que falte en el lado no tocado, igual que ADR-011/012.

## Decisión

1. **La resurrección solo tenía sentido sin base.** La tabla de clasificación
   de ADR-012 no cambia; solo cambia la ACCIÓN en las dos filas donde SÍ
   había base y un lado está ausente. Antes (slice 13) esas dos filas
   resucitaban (volvían a bajar/subir); ahora se interpretan como lo que de
   verdad son -- **el usuario borró el archivo en un lado** -- y se
   convierten en candidatos a propagar ese borrado.

2. **Recolectar antes de ejecutar.** `_reconcilePass` ya no ejecuta el
   borrado al vuelo: junta todos los candidatos de la pasada
   (`_DeleteCandidate`, con ambos sentidos) y decide al final, sobre el
   **lote completo**, no sobre lo que quede tras descontar lo ya confirmado
   -- si se comparara contra el resto, confirmar solo 3 de un lote de 12
   dejaría que los otros 9 se colaran igual por quedar "bajo el umbral"
   ellos solos (bug real encontrado y corregido durante los propios tests
   de este slice). La regla real:
   - Lote de tamaño ≤ `_maxAutoDeleteBatch` (10, cuenta los dos sentidos
     juntos): se ejecuta entero, sin pedir nada. Diez es deliberadamente
     conservador para uso personal (§160) -- un borrado suelto es un gesto
     normal; un lote grande de golpe huele más a carpeta mal apuntada o
     unidad desconectada que a intención real.
   - Lote > 10: **solo** se ejecuta lo que esté explícitamente en
     `confirmedDeletePaths` (claves que el usuario ya confirmó viendo la
     lista real en una llamada anterior); el resto queda en
     `SyncResult.pendingDeletes`, sin importar cuántos sean. Un candidato
     pendiente conserva su entrada de manifiesto tal cual -- ni se borra ni
     se resucita -- así la próxima pasada lo reconoce como el mismo
     candidato hasta que se confirme o el archivo reaparezca.

3. **Guarda extra: carpeta local inexistente.** Con la resurrección de
   ADR-012, una carpeta local desconectada/mal apuntada era inofensiva (todo
   se volvía a bajar). Con borrados activos sería catastrófico: TODO el
   árbol remoto parecería "borrado en local", y en un par pequeño podría
   colar por debajo del umbral y borrarse de verdad. Por eso `_runSync`
   comprueba `Directory(pair.localPath).existsSync()` antes de reconciliar
   en modo `Ambos`; si falla, aborta la pasada entera (ni sube, ni baja, ni
   borra) con un error claro, y **copia el manifiesto existente tal cual**
   a la salida -- sin este cuidado, escribir un manifiesto vacío habría
   borrado el rastro de todo lo sincronizado hasta entonces.

4. **Papelera en los dos lados, nunca un borrado definitivo directo.**
   Propagar hacia el servidor reutiliza `FilesRepository.deleteFile(id,
   permanent: false)`, que ya existe (ADR-006) -- cero código servidor
   nuevo. Propagar hacia local usa una **papelera local** nueva
   (`LocalTrashStore`/`FileLocalTrashStore`): mueve el archivo a
   `getApplicationSupportDirectory()/local_trash/<clave del par>/...`, con
   un sello de tiempo en el nombre para que dos borrados sucesivos del mismo
   `relPath` nunca se pisen. Se descartó depender de la Papelera de
   reciclaje real de Windows -- exigiría un plugin nativo COM
   (`IFileOperation`), la misma clase de complejidad que el plugin WinRT del
   slice 12, no justificada para esto. `SyncPair` gana `stableKey` (el hash
   que antes vivía duplicado dentro de `FileSyncStateStore`) para que la
   papelera local y el manifiesto de estado usen la misma clave de carpeta
   sin repetir la lógica de hashing.

5. **Confirmación solo desde una interacción real del usuario.**
   `SyncEngine.syncNow` gana `confirmedDeletePaths` (claves internas). La UI
   (`SyncSettingsPage`) muestra `showConfirmDialog` (ya existente,
   `core/widgets/confirm_dialog.dart`, mismo patrón que `TrashPage`) con la
   lista real de archivos y su sentido -- nunca solo una cifra -- y, si se
   confirma, vuelve a llamar a `syncNow` con esas claves.
   `AutoSyncScheduler._tick` **nunca** rellena `confirmedDeletePaths`: un
   tick automático sin nadie delante jamás confirma un borrado masivo; el
   recuento de pendientes se refleja en el resumen de texto que ya persiste
   `saveLastAutoSyncOutcome`.

## Consecuencias

- Dos tests de ADR-012 que afirmaban "borrado no propagado: se vuelve a
  bajar/subir" quedan **invertidos a propósito**: ahora afirman que SÍ se
  propaga (a la papelera correspondiente). Es el cambio de comportamiento
  central de este slice, no una regresión.
- No hay UI para navegar/restaurar la papelera local en este slice -- los
  archivos quedan recuperables a mano en esa carpeta.
- No se borran carpetas vacías resultantes (ni local ni remoto) -- mismo
  criterio que ADR-011/012 con mover/renombrar: fuera de alcance.
- El umbral (10) es una constante en código, no configurable desde la UI
  todavía -- candidato a ajustarse en un slice futuro si la práctica
  demuestra que el valor no encaja.
- `SyncEngine` pasa a requerir también un `LocalTrashStore` en el
  constructor -- arrastra a los tests que lo instancian directamente
  (`sync_engine_test.dart`, `auto_sync_scheduler_test.dart`).
