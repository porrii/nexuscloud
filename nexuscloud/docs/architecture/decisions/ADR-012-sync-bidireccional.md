# ADR-012: Sincronización bidireccional del cliente — manifiesto de estado local, conflict copies, sin propagación de borrados

## Estado

Aceptado.

## Contexto

`ADR-011` construyó el motor de sync v1: **unidireccional servidor→local**,
manual, un solo par, **sin base de datos** — decide qué descargar comparando
`size`+`mtime` local (que el propio motor fija tras cada descarga) contra lo que
el servidor reporta. Ese mismo ADR dejó explícitamente fuera "subir cambios
locales", "conflictos §40" y "varios pares", y anotó que un slice bidireccional
*"tendrá que introducir persistencia propia entonces"*: sin un estado base "a
fecha de la última sincronización" no se puede distinguir "cambió en remoto" de
"cambió en local" de "cambió en los dos" (§40 exige *no sobrescribir
silenciosamente* y *crear conflict copies cuando corresponda*).

Este slice (13) añade dirección de sincronización (`Descargar` / `Subir` /
`Ambos`) y, para `Ambos`, esa reconciliación de tres vías. **Queda fuera la
propagación de borrados** en cualquier sentido — la parte con más riesgo de
pérdida de datos —, para un slice futuro dedicado. Decidido con el usuario
(2026-09-10): alcance = "bidireccional sin borrados".

Antes de fijar el diseño se comprobó contra el backend real un detalle no
evidente: `POST /files?path=/a/b&name=x` con `/a/b` inexistente **crea el
archivo con ese `parent_path` pero sin filas de carpeta**, y entonces
`GET /files?path=/` no lista `/a`, así que el recorrido remoto nunca vería ese
archivo. Por eso la subida de un archivo anidado tiene que **materializar las
carpetas padre** antes (`POST /directories`, idempotente, nivel a nivel —
`name` no admite `/`). Esto obligó a añadir `FilesRepository.createDirectory`,
que el cliente no había necesitado hasta ahora.

## Decisión

1. **Dirección de sincronización como enum de tres valores**
   (`SyncDirection { download, upload, both }`), persistida en
   `SyncConfigRepository`. **Por defecto `download`** — una configuración
   anterior a este slice se comporta igual sin que el usuario toque nada; un
   valor persistido desconocido (versión futura, dato corrupto) también cae a
   `download`, el modo que nunca escribe hacia el servidor.

2. **Manifiesto de estado como un fichero JSON por par, NO una base de datos.**
   `FileSyncStateStore` escribe
   `getApplicationSupportDirectory()/sync_state/<hash>.json` con una entrada por
   ruta relativa: `{remote_size_bytes, local_size_bytes, sha256,
   remote_updated_at, local_modified_at}`. A la escala de §160 (uso personal) el
   manifiesto entero cabe en memoria y se reescribe entero en cada sync; una BD
   (`sqflite`/`drift`) solo añadiría una dependencia nativa pesada sin resolver
   ningún problema real todavía. `path_provider` pasa de transitiva a directa
   (ya estaba resuelta; sin cambio de versión).
   - **Fuera de la carpeta sincronizada** a propósito: dentro se
     auto-sincronizaría y el usuario podría borrarlo por error. El `<hash>` es
     SHA-256 de `"$remotePath|$localPath"` → cambiar de par ⇒ manifiesto nuevo,
     empezando de cero.
   - Manifiesto ausente o corrupto ⇒ se lee como base vacía (semántica de
     primera pasada), **nunca lanza**. La siguiente escritura lo deja bien.
   - Tamaño remoto y local **separados**: en sync coinciden, pero tras una
     conflict copy la base refleja dos lados con tamaños distintos y hay que
     recordar cada uno para no re-disparar el conflicto.
   - Escritura atómica (tmp + rename), mismo criterio que ADR-002/ADR-011.

3. **Reconciliación de tres vías en UN único recorrido** (no una bajada seguida
   de una subida — eso dejaría que la bajada pisara una edición local antes de
   que la subida la viera, agujero §40). Para cada ruta, con `R` = remoto, `L` =
   local, `B` = base:

   | B | R | L | Acción |
   |---|---|---|--------|
   | – | ✓ | – | remoto nuevo → descargar |
   | – | – | ✓ | local nuevo → subir |
   | – | ✓ | ✓ | sin base: `sha(L)==R.sha` → registrar; si no → **conflicto** |
   | ✓ | ✓ | ✓ | `rCambió`/`lCambió` vs base (fecha±2s ∨ tamaño) → ninguno: omitir · solo R: descargar · solo L: subir · ambos: `sha(L)==R.sha` → registrar; si no → **conflicto** |
   | ✓ | – | ✓ | remoto ausente — **borrado NO se propaga**: subir `L` (resucita) |
   | ✓ | ✓ | – | local ausente — **borrado NO se propaga**: descargar `R` (resucita) |
   | ✓ | – | – | ambos ausentes → soltar la entrada de base |

   `sha(L)` se calcula **solo** en las ramas que lo piden; el atajo `size`+`mtime`
   corta el caso común sin leer el archivo (`sha256.bind(file.openRead()).first`,
   el mismo patrón que verifica las descargas, `files_remote_data_source.dart`).

4. **Conflicto (§40) = conflict copy, nunca sobrescritura.** Se descarga la
   versión remota junto al archivo local como
   `nombre (conflicto <fecha remota local>).ext`; si ya existe con ese nombre no
   se reescribe. El archivo local y el servidor quedan **intactos**. Se reporta
   en `SyncResult.conflicts`. **La base se avanza al estado actual de los dos
   lados** (con sus tamaños distintos): el conflicto se notifica una vez y no se
   re-dispara en cada tick mientras ninguno vuelva a cambiar de verdad. La
   conflict copy queda como artefacto para que el usuario funda los cambios a
   mano — no hay resolución asistida en la UI en este slice.

5. **Modo `upload`**: recorre el árbol local; sube lo que no está en el servidor
   o difiere en contenido (`sha(L) != R.sha`). El servidor **versiona solo**
   cualquier sobrescritura de un archivo activo (`file_service.go`, no da 409 —
   ADR-007), así que `upload` no puede provocar pérdida silenciosa: eso ES
   "crear versiones cuando corresponda" del §40. Nunca descarga ni borra.

6. **Modo `download`**: comportamiento idéntico a ADR-011, pero además escribe
   la entrada de manifiesto tras cada descarga/omisión — puro apunte contable.
   Así, quien pase de `download` a `both` ya tiene base y no ve conflictos
   espurios en la primera pasada de una carpeta ya poblada.

7. **Al recorrer el árbol local se excluyen**: basura del SO (`.DS_Store`,
   `Thumbs.db`, `desktop.ini`) y **las propias conflict copies** (patrón
   `* (conflicto AAAA-MM-DD HH.MM.SS)*`) — si no, una conflict copy se subiría
   como "archivo local nuevo" y se propagaría al servidor.

8. **`FilesRepository.createDirectory(parentPath, name)`** nuevo (delega en
   `POST /directories`, idempotente). Lo usa la subida para materializar las
   carpetas padre de un archivo anidado antes de subirlo. Los 5 fakes de test
   que implementan `FilesRepository` ganan el stub correspondiente.

## Consecuencias

- **Borrar un archivo en un lado NO lo borra en el otro**: en `both` se vuelve a
  traer del lado que aún lo tiene ("resurrección"). Es el comportamiento seguro
  elegido a propósito para este slice; el slice de borrados lo cambiará con su
  propia guarda anti-"borrado masivo" y mover-a-papelera.
- El manifiesto es estado local no versionado: si se pierde (máquina nueva,
  `%LOCALAPPDATA%` limpiado), la siguiente `both` trata todo como primera pasada
  — puede generar conflict copies donde antes no las había si los dos lados
  divergieron mientras no había manifiesto. Coste aceptado; el manifiesto se
  reconstruye solo a partir de esa pasada.
- `SyncEngine` pasa a requerir un `SyncStateStore` en el constructor — cambio de
  firma que arrastra a los tests que lo instancian (se les inyecta un almacén en
  memoria).
- `mover/renombrar` un archivo se ve como borrado + alta (no se detecta como
  movimiento) — fuera de alcance, como en ADR-011.
- Sigue siendo **un solo par** y **sin vigilancia de filesystem**: `both` razona
  por recorrido completo en cada pasada, igual que `download` (ADR-011 dec. 5).
