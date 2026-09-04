# ADR-008: Compartición con recurso polimórfico tipado, hasher inyectado y contraseña por cabecera

## Estado

Aceptado.

## Contexto

§37 pide un "sistema avanzado de compartición" con tres capacidades: usuario→usuario, usuario→grupo, y enlaces públicos (con contraseña, expiración, límite de descargas, límite de tamaño, revocación y nombre personalizado). Con Sharing implementado, la Fase 2 completa queda cerrada (Web UI, File manager, Papelera, Versionado, Sharing).

Cuatro decisiones de diseño concretas necesitaban resolverse antes de escribir código:

1. **Cómo referenciar el recurso compartido** (un archivo o una carpeta, nunca ambos) sin acoplar la tabla `shares` a un identificador polimórfico sin FK real.
2. **Cómo hashear la contraseña de un enlace** sin que `internal/storage` importe `internal/auth` (regla de dependencia, `docs/architecture.md`: storage no conoce ni users ni auth).
3. **Cómo transmitir esa contraseña** en cada petición a un enlace sin dejarla en logs, historial del navegador o cabeceras `Referer`.
4. **Qué acceso da compartir una carpeta**: solo su listado vacío, o todo su contenido recursivamente (incluidas subcarpetas sin share propio).

## Decisión

1. **`file_id`/`directory_id` como columnas separadas y nullables**, con `CHECK ((file_id IS NOT NULL) <> (directory_id IS NOT NULL))`, en vez de un par `(resource_type, resource_id)` sin restricción real de integridad. Ambas columnas tienen `REFERENCES ... ON DELETE CASCADE`, así que borrar un archivo o carpeta para siempre limpia sus shares automáticamente -- misma filosofía que `file_versions.file_id` (migración 0004, [ADR-007](ADR-007-versioning.md)). Al ser una tabla completamente nueva (migración 0005), no hay ningún problema de migración compleja como en [ADR-006](ADR-006-trash.md): los índices parciales (`WHERE target_user_id IS NOT NULL`, etc.) se crean bien desde el principio.
2. **`PasswordHasher` como interfaz local** (`Hash`/`Verify`) definida en `internal/storage`, en vez de importar `internal/auth`. `*auth.Hasher` la satisface estructuralmente sin ningún cambio en `internal/auth` -- Go no exige una declaración explícita de "implements". `internal/server`, que ya construye un único `*auth.Hasher` para el login, se lo pasa tal cual a `NewFileService`. Cero duplicación de Argon2id, cero ciclo de imports.
3. **Contraseña de enlace en la cabecera `X-Share-Password`**, nunca en la query string ni en una cookie nueva. Un enlace sin contraseña sigue siendo un `<a href>` plano que funciona sin JavaScript; uno con contraseña necesariamente pasa por la SPA, que la pide y repite la petición con la cabecera -- no hace falta un mecanismo de "unlock" con estado en servidor ni un nuevo tipo de sesión.
4. **Compartir una carpeta da acceso a todo su contenido**, incluidas subcarpetas sin share propio: `FileService.Download`/`ListSharedDirectory` prueban primero un share directo sobre el recurso exacto y, si no hay, recorren las carpetas ancestro (`hasShareAccessToAncestorDirectories`) buscando uno que lo cubra. Es un bucle acotado por la profundidad de carpetas, irrelevante a la escala objetivo de ~100 usuarios (§160) -- no hace falta cache ni tabla materializada (§162 simplicidad). Navegar una carpeta compartida se re-autoriza en cada nivel contra la base de datos usando el ID real de la subcarpeta (obtenido de un listado ya autorizado), nunca una sub-ruta que el cliente pudiera manipular para escapar de la raíz compartida.

Decisión adicional de alcance, tomada al escribir el código: **las opciones de permiso (descarga/subida) solo se exponen para enlaces**, no para shares de usuario/grupo -- §37 las lista explícitamente bajo "### Enlaces", no bajo "### Usuarios"/"### Grupos". `CreateShare` fuerza `can_upload=false` para `share_type` `user`/`group`; compartir con permiso de escritura a una persona o grupo concretos queda fuera de esta pasada (un enlace de subida cubre ese caso).

## Consecuencias

- Ninguna migración recrea tablas ni necesita conocer el nombre autogenerado de una constraint (a diferencia de [ADR-006](ADR-006-trash.md)): `shares` nace ya con la forma final.
- `internal/auth` permanece sin cambios; la regla de dependencia (`storage` no importa `auth` ni `users`) se mantiene intacta pese a que Sharing necesita Argon2id.
- El límite de tamaño de subida (`max_upload_size_bytes`) se aplica con un lector propio (`errLimitReader`) que corta con error en cuanto se lee un byte de más, en vez de truncar en silencio como `io.LimitReader` -- así `Upload` aborta de forma natural y su propio manejo de staging limpia el fichero parcial sin pasos adicionales.
- `download_count` se incrementa con un `UPDATE ... WHERE ... AND (max_downloads IS NULL OR download_count < max_downloads)` atómico (`RowsAffected`), no leer-luego-escribir: dos descargas concurrentes contra el último hueco de `max_downloads` nunca dejan pasar a ambas (verificado con un test de concurrencia real).
- Revocar es un soft-update (`revoked_at`), no un `DELETE`: el registro sobrevive para el rastro de auditoría (`audit_events`, eventos `share_create`/`share_revoke`); las listas ("compartido conmigo"/"compartido por mí") excluyen revocados por defecto.
- Fuera de esta pasada, documentado explícitamente: subida con permiso de escritura dirigida a un usuario/grupo concreto (solo enlaces la soportan); §38 (Subida Anónima) sigue totalmente separado y no implementado -- es un modelo de autorización distinto (activación explícita del admin, foco anti-abuso), no una variante de un share normal.
