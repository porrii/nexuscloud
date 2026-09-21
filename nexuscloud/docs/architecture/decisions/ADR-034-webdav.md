# ADR-034: WebDAV

## Estado

Aceptado.

## Contexto

§43 pide WebDAV como módulo independiente que pueda activarse y
desactivarse, configurarse, limitarse y auditarse; §3 y §169 lo quieren
desactivado por defecto, protegido por autenticación y registrado en
auditoría. Fase 6 (§163) lo agrupa con Passkeys bajo "seguridad avanzada":
este ADR cubre el segundo slice (el primero es [ADR-033](ADR-033-webauthn-passkeys.md)).

El objetivo es que un usuario pueda montar SU árbol de archivos como unidad
de red, o usarlo desde clientes estándar (Explorador de Windows, Finder,
rclone, Cyberduck...), con exactamente las mismas reglas de negocio que la
API REST -- papelera, versionado, pools, propiedad, auditoría --, no como
una vía paralela al disco.

## Decisión

1. **Librería: `golang.org/x/net/webdav`**, la implementación de referencia
   del protocolo en Go, en lugar de escribir PROPFIND/LOCK/COPY/MOVE a mano.
   **Fijada en `v0.58.0`, no `@latest`**: la siguiente exige `go 1.26.0` y
   arrastraría esa exigencia al `go.mod` (CI sigue en Go 1.25; el mismo
   problema ya evitado con `go-webauthn` y `govulncheck`). Ya era una
   dependencia indirecta (`v0.54.0`); pasa a directa y sube con ella
   `crypto` 0.55, `term` 0.45 y `text` 0.41, todas `go 1.25.0`.

2. **Autenticación: HTTP Basic con *tokens de acceso WebDAV*, nunca la
   contraseña de la cuenta.** Los clientes WebDAV no pueden hacer TOTP ni un
   passkey interactivo. Aceptar la contraseña por Basic dejaría a cualquiera
   que la conozca leyendo y borrando todo **saltándose el segundo factor**
   que ya protege el login, y además la enviaría en cada PROPFIND. Un token
   por dispositivo es el estándar de la industria ("contraseñas de
   aplicación"):
   - `nwd_` + 32 bytes de `crypto/rand` en base64url (256 bits, mismo
     generador que sesiones e invitaciones). Solo se guarda su SHA-256; el
     token en claro se muestra una única vez (§78).
   - Revocable individualmente; solo sirve para WebDAV (no abre la API ni
     la web).
   - El usuario de Basic es el `username` real y debe coincidir con el
     dueño del token (comparación en tiempo constante): así el cliente tiene
     un identificador y un token robado no sirve con otro nombre.
   - Token desconocido, usuario que no coincide o cuenta desactivada dan el
     **mismo** 401 (sin enumeración de usuarios) y el mismo evento de
     auditoría `webdav_auth_failed`, que registra el usuario y la IP y
     **nunca** el token.
   - `last_used_at` se actualiza como mucho una vez por minuto: un cliente
     lanza decenas de peticiones por segundo y no debe escribir en la base
     de datos en cada una.

3. **Mismo listener y ruta configurable** (`webdav.path`, por defecto
   `/webdav`), montada en el router raíz junto a la API: TLS, cabeceras de
   seguridad y auditoría compartidos. El spec habla de "puerto/ruta"; un
   segundo puerto no aportaba nada. `config.Validate` rechaza rutas que
   cuelguen de `/api`, `/health` o `/ready` y las que contengan `.`/`..`.
   **chi solo conoce los métodos HTTP estándar**: PROPFIND, PROPPATCH,
   MKCOL, COPY, MOVE, LOCK y UNLOCK devolverían 405 antes de llegar al
   handler. Se registran con `chi.RegisterMethod` en un `init()` (global e
   idempotente); una prueba de integración recorre el router real para que
   nadie lo rompa sin darse cuenta.

4. **Adaptador `webdav.FileSystem` sobre `storage.FileService`, nunca sobre
   el directorio físico.** Ir al disco se saltaría la base de datos, la
   propiedad, la papelera, el versionado y la auditoría. Hay **un
   `Handler`, un `FileSystem` y un `LockSystem` por usuario** (creados al
   vuelo): la identidad del dueño es estructural en vez de ambiental (ninguna
   llamada puede tocar el árbol de otro) y los locks de x/net se indexan por
   ruta relativa, así que una única instancia compartida haría que dos
   usuarios con un `/informe.doc` se bloqueasen entre sí.
   `FileService` trabaja por ID y solo por ruta en `Upload`/`List`/`Mkdir`;
   resolver una ruta = listar su carpeta padre y buscar por nombre (API
   pública, sin tocar el núcleo de almacenamiento).

5. **Escritura en streaming (`io.Pipe` → `FileService.Upload`)**, sin
   spooling a disco: reutiliza el temporal + `rename` atómico de `Upload` y
   admite ficheros grandes sin doblar la E/S. **El handler de x/net llama a
   `Close()` también cuando la copia del cuerpo falló** (cliente cortado a
   mitad de PUT): con una implementación ingenua eso confirmaría un fichero
   truncado como versión nueva y válida. El cuerpo de la petición se envuelve
   con un rastreador de errores; si falló, `Close()` aborta la subida y no
   queda nada. Una prueba con un cuerpo `chunked` corrupto (más un control
   positivo) lo comprueba. El ETag es el SHA-256 real del contenido, el mismo
   que `FileMeta.SHA256`.

6. **Lecturas con `Seek`** (`http.ServeContent`, necesario para `Range` y
   reanudar): el provider local devuelve un `*os.File`, que lo permite; un
   provider futuro que no sea seekable falla con un error claro, no con
   código especulativo.

7. **"Limitarse" (§43):** `webdav.readOnly` (solo OPTIONS/GET/HEAD/PROPFIND,
   403 en el resto), `webdav.maxUploadSizeBytes` (0 = sin límite; 413 por
   `Content-Length` y `http.MaxBytesReader` para cuerpos `chunked`) y un cubo
   de rate limit **propio** por IP, `security.rateLimit.webdavPerMinute`
   (1200 por defecto): mucho más holgado que el de la API, porque un cliente
   WebDAV legítimo lanza ráfagas de PROPFIND.

8. **Auditoría (§43):** las operaciones reutilizan los tipos de la API
   (`upload`, `download`, `delete`, `move`) marcadas con `via: webdav`, más
   `webdav_token_created`, `webdav_token_revoked` y `webdav_auth_failed`.

9. **Sobrescritura con MOVE/COPY (`Overwrite: T`): sin efectos
   destructivos.** Con `Overwrite: T` (que es el valor por defecto si el
   cliente no manda la cabecera) x/net hace `RemoveAll(destino)` y luego crea
   o renombra encima. Con la papelera activa eso es una trampa: el destino
   iría a la papelera, su nombre quedaría reservado (§128, ADR-030) y el paso
   siguiente fallaría, con el cliente viendo un error **y el destino ya fuera
   del árbol**. Lo encontraron `litmus` y una reproducción con `curl` (un
   MOVE devolvía 403 y `destino.txt` había pasado a la papelera). Ahora, con
   la papelera activa y dentro de un MOVE/COPY, ese `RemoveAll` significa
   "empezar una sobrescritura" y no borra nada:
   - **Destino archivo:** el COPY sube encima y el MOVE copia el contenido
     encima y borra el origen. En ambos, el contenido anterior del destino
     queda como versión (igual que un PUT sobre un archivo existente) y el
     destino nunca pasa por la papelera.
   - **Destino carpeta** (o tipos incompatibles): 403 **sin tocar nada**.
     Reemplazar un árbol entero exigiría borrar y volver a crear los mismos
     nombres, imposible mientras la papelera los reserve.
   - Con `trash.enabled: false` el borrado es físico y los nombres quedan
     libres, así que se mantiene el comportamiento original y la semántica
     completa del RFC 4918 (carpetas incluidas). Para que el adaptador
     pueda saberlo se añade `FileService.TrashEnabled()`.

10. **`LockSystem` propio para el MOVE con `If:`.** x/net exige que las
    condiciones de la cabecera `If` cubran a la vez el origen **y** el
    destino, aunque el destino no esté bloqueado; RFC 4918 §7.5 solo pide
    token para los recursos que sí lo están. Un cliente que bloquea un archivo
    y luego lo renombra (`davfs2`, y con toda probabilidad otros) manda solo el
    token del origen y recibía **412 Precondition Failed**. `lockSystem`
    envuelve `MemLS` y, cuando los tokens no cubren los dos recursos, exige
    que cubran al menos uno y retiene el otro con un bloqueo temporal (que
    falla si otro lo tiene): token inventado o destino bloqueado por otro
    siguen dando 412. Una prueba "canario" documenta el comportamiento de
    x/net y fallará si algún día lo corrigen (entonces el envoltorio sobra).

11. **Esquema (migración `0010_webdav_tokens`, sqlite + postgres + mysql):**
    `webdav_tokens(id, user_id → users ON DELETE CASCADE, token_hash UNIQUE,
    label, created_at, last_used_at)`. Mismo criterio que `sessions`: MySQL
    sin índice redundante para la FK, `token_hash VARCHAR(64)`.

12. **Gestión de tokens:** `POST/GET/DELETE /api/v1/auth/webdav/tokens` (solo
    registrados con `webdav.enabled=true`, mismo criterio que
    `ClientUpdatesProxy` y WebAuthn; el revocado comprueba la propiedad en el
    propio `WHERE`, §198), sección "Acceso WebDAV" en `AccountPage` (crea el
    token, lo muestra una sola vez con la URL, el usuario y botones de
    copiar; se oculta sola si el backend responde 404) y CLI
    `nexuscloud users webdav-token create|list|revoke --username`. A
    diferencia de los passkeys, la CLI **sí** puede crear tokens: no
    necesita un navegador.

## Consecuencias

- Nuevo `WebDAVConfig` (`enabled`, `path`, `readOnly`, `maxUploadSizeBytes`),
  desactivado por defecto, variables `NEXUSCLOUD_WEBDAV_*` y
  `security.rateLimit.webdavPerMinute`. Con `enabled=false` no se construye
  nada ni se monta ninguna ruta.
- **Defecto previo encontrado y corregido en el núcleo.** Con la papelera
  activa, `FileService.Delete` es un borrado lógico (el archivo sigue en
  disco), así que `DeleteDirectory` fallaba con "directorio no vacío" al
  intentar quitar físicamente una carpeta cuyos archivos estaban en la
  papelera; el borrado recursivo de WebDAV lo destapó. Ahora `DeleteDirectory`
  tolera ese caso y `permanentlyDeleteDirectory` intenta la limpieza física.
  Dos pruebas de regresión en `internal/storage`.
- **Límites conocidos** (documentados para el usuario en
  [`webdav.md`](../../webdav.md#límites-conocidos)):
  - **Regla de la papelera (§128).** Un nombre ocupado por algo en la
    papelera no se puede reutilizar hasta restaurarlo, borrarlo
    definitivamente o que caduque la retención (30 días). Por WebDAV, "borrar
    y volver a crear el mismo nombre" da **405** (`PUT`/`MKCOL`), con la
    papelera activa. Afecta a editores y aplicaciones que crean y borran
    ficheros temporales o de bloqueo con el mismo nombre. Es la misma regla
    que aplica la API REST; con `trash.enabled: false` no ocurre. **Decisión
    de producto tomada** (ver abajo).
  - **Borrado recursivo = una entrada de papelera por elemento.** Borrar una
    carpeta con contenido manda cada archivo y cada subcarpeta a la papelera
    por separado; restaurar la entrada de la carpeta recupera **solo la
    carpeta vacía**. Medido: `rclone purge` de 300 archivos en 20 carpetas
    dejó 321 entradas. Nada se pierde, pero recuperar un árbol entero
    exigiría restaurar cada entrada. No es atómico: si falla a medias, lo ya
    borrado queda borrado.
  - **Sin propiedades muertas:** `PROPPATCH` responde 403.
  - **Solo locks exclusivos y en memoria** (`MemLS`): los compartidos dan 501
    y todos se pierden al reiniciar el servidor.
  - x/net valida poco el XML de PROPFIND (no rechaza una declaración de
    espacio de nombres inválida) y no evalúa cabeceras `If` complejas.
  - No se expone "compartido conmigo", solo el árbol propio. Las cuotas
    (`quota_bytes` en usuarios y grupos) se guardan pero ningún camino de
    subida las aplica, ni la API ni WebDAV; PROPFIND responde 404 a las
    propiedades `quota-*`.
  - El provider debe permitir `Seek` (el local lo permite).
- **Verificación.** 39 tests en `internal/webdav` (tokens, adaptador con un
  `FileService` real, handler sobre `httptest`, locks) y 10 de integración
  HTTP contra el servidor real (`server.Build`: rutas ausentes con WebDAV
  desactivado, los métodos llegan de verdad por el router, WebDAV y la API
  REST comparten papelera/versionado/auditoría, solo lectura, ruta
  personalizada, rate limit propio, auditoría, gestión de tokens, overwrite);
  las pruebas de reversión (quitar `RegisterMethod`, el rastreador de errores
  del cuerpo o el manejo del overwrite) hacen fallar los tests. La suite
  completa pasa contra MySQL 8 y PostgreSQL 16 reales (Docker, en un
  contenedor Linux). `govulncheck@v1.1.4` (la versión de CI): **0
  vulnerabilidades alcanzables**. Avisa además de 3 en módulos requeridos que
  el código no llama, todas en `golang.org/x/crypto v0.55.0` (la versión que
  exige `x/net v0.58.0`): dos se corrigen en `v0.56.0`, que exige Go 1.26 y
  por eso no se adopta (mismo motivo que el punto 1), y una no tiene
  corrección. Antes de este slice eran 4, en `v0.52.0`.
- **Verificación con clientes reales**, contra el binario Linux en un
  contenedor:
  - **rclone**: 100 MB con SHA-256 idéntico de ida y vuelta; 300 archivos con
    8 transferencias en paralelo y `rclone check --download` con 0
    diferencias (sin un solo 429); `cat`, `moveto`/`copyto` (MOVE/COPY en el
    servidor, de archivos y de colecciones), sobrescritura con exactamente una
    versión archivada recuperable, segundo `sync` sin transferir nada, nombres
    con `ñ`, emoji, espacios, `#`, `%` y `+`, archivo vacío, `purge`
    recursivo; cada paso contrastado con el CLI del servidor y la papelera.
  - **litmus 0.13** (suite de conformidad de WebDAV): **67 de 80** (`basic`
    16/16, `http` 4/4, `locks` 30/34, `props` 9/13, `copymove` 8/13). Los 13
    fallos son todos de las categorías anteriores: reemplazar una colección
    entera con `Overwrite: T` y sus cascadas de nombre en la papelera
    (`copymove` 4, 7, 8, 9, 10; `props` 25), propiedades muertas
    (`props` 6 y 26, `locks` 11 y 33), y límites de x/net (`props` 3, `locks`
    20 y 23, más un aviso en `locks` 18).
  - **davfs2** (montaje FUSE real): crear, leer, sobrescribir, renombrar
    (`LOCK` + `MOVE` con `If`), `mv` sobre un archivo existente, `cp`,
    renombrar una carpeta con contenido y 20 MB con SHA-256 idéntico tras
    remontar. Encontró el defecto del punto 10. `ls` y `rm -r` fallan con
    `EINVAL` en este entorno, **igual que contra un servidor de referencia
    independiente** (`rclone serve webdav`): es una incompatibilidad del FUSE
    de la máquina de pruebas, no de NexusCloud, así que `readdir` y `rmdir`
    con davfs2 no pudieron evaluarse.
  - **Sin verificar aquí:** el cliente integrado de Windows (Mini-Redirector,
    que por defecto exige HTTPS para Basic), Finder de macOS y aplicaciones de
    escritorio como Microsoft Office. Por el defecto de la papelera anterior,
    las que guardan con ficheros temporales del mismo nombre son las más
    expuestas.
- **Decisión de producto (§128), tomada el 2026-09-21: opción (a).** Mantener
  la reserva de nombres de la papelera para WebDAV es coherente con la API y
  ADR-030, pero limita el uso como "unidad de red" con editores. Opciones:
  (a) dejarlo como está y documentar `trash.enabled: false` para quien quiera
  esa experiencia; (b) relajar la regla en el núcleo (permitir crear un
  nombre ocupado por la papelera y resolver el conflicto al restaurar), lo
  que exige tocar el esquema en los tres motores porque hoy la unicidad
  incluye las filas eliminadas y cambiar el comportamiento de "restaurar".
  **Se adopta (a)**, que ya está explicada al usuario en
  [`webdav.md`](../../webdav.md#límites-conocidos). (b) queda descartada por
  ahora y solo se retomaría si el uso de WebDAV como unidad de red con
  editores pasa a ser un objetivo real.
