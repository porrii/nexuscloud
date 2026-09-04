# ADR-006: Papelera con soft-delete, sin índice único parcial

## Estado

Aceptado.

## Contexto

§16 exige papelera configurable (activada/desactivada, días de retención, limpieza automática) como primera línea de defensa contra borrado accidental y ransomware (§128). `files`/`directories` (migración 0001) ya tenían `UNIQUE(pool_id, owner_id, parent_path, name)` para impedir duplicados dentro del árbol de un usuario.

El diseño "correcto" de un sistema con papelera (Google Drive, Windows) permite **reutilizar un nombre** mientras el elemento original sigue en la papelera: trashear `notas.txt` y crear un `notas.txt` nuevo son operaciones independientes. Conseguir eso exige que la restricción de unicidad solo aplique a filas activas — un índice único **parcial** (`WHERE deleted_at IS NULL`), soportado tanto por SQLite como por PostgreSQL.

El problema es la migración: ni SQLite ni PostgreSQL permiten "convertir" una constraint de tabla ya existente en un índice parcial con un simple `ALTER TABLE`. SQLite no soporta `DROP CONSTRAINT` en absoluto (exige recrear la tabla completa: crear la nueva, copiar filas, borrar la vieja, renombrar). PostgreSQL sí soporta `DROP CONSTRAINT`, pero solo si se conoce el nombre exacto de la constraint autogenerada (`files_pool_id_owner_id_parent_path_name_key`, siguiendo la convención de nombres de PostgreSQL) — un nombre que no se pudo verificar contra una instancia PostgreSQL real en esta sesión de desarrollo (solo se disponía de SQLite para pruebas).

## Decisión

Implementar la papelera con la restricción `UNIQUE` **existente sin modificar**:

1. Añadir `deleted_at TEXT` (nullable) a `files` y `directories` (migración 0003, una migración nueva — nunca se reescribe una ya aplicada, §8).
2. `Delete`/`DeleteDirectory` hacen soft-delete (`deleted_at = ahora`) cuando `trash.enabled=true`; con `false`, borran de inmediato.
3. **Mientras un elemento sigue en la papelera, su nombre permanece "ocupado"**: subir o crear algo nuevo con el mismo `(pool, owner, parent_path, name)` se rechaza explícitamente (`ErrNameOccupiedByTrash`, HTTP 409) en vez de intentar convivir con la restricción existente de una forma que pudiera resucitar/sobrescribir el contenido en papelera en silencio (§128: nunca resolver un conflicto de nombre destruyendo datos sin que el usuario lo pida explícitamente).
4. El usuario debe restaurar, purgar (`?permanent=true`) o esperar la purga automática antes de poder reutilizar ese nombre exacto.

## Consecuencias

- Ninguna migración recrea tablas: 0003 es un `ALTER TABLE ... ADD COLUMN` simple y seguro en ambos dialectos, cero riesgo de pérdida de datos durante el despliegue.
- UX ligeramente peor que Google Drive/Windows en un caso concreto (no se puede tener simultáneamente un archivo activo y uno en papelera con el mismo nombre exacto en la misma carpeta) — aceptado conscientemente (§162 Principio de simplicidad: preferir la solución sencilla salvo razón técnica de peso, y aquí la razón de peso —no poder verificar la migración compleja contra PostgreSQL real— apunta hacia la opción simple).
- Mejora futura documentada, no bloqueante: migrar a un índice único parcial en una migración 0004 cuando se pueda verificar contra una instancia PostgreSQL real (p.ej. en CI, que sí compila y testea, aunque esta sesión de desarrollo no tuviera acceso a una).
- `FileService` centraliza la comprobación (`rejectIfTrashOccupiesName`) antes de tocar el filesystem en `Upload` y `Mkdir`, así que añadir el índice parcial más adelante sería un cambio interno sin afectar al contrato de la API (el 409 seguiría siendo posible en casos de carrera, solo cambiaría cuándo se dispara).
