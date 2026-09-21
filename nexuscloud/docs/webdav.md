# WebDAV

Monta tu espacio de NexusCloud como unidad de red, o úsalo desde clientes
estándar (Explorador de Windows, Finder, rclone, Cyberduck, `davfs2`...).
Es un módulo independiente (§43): **desactivado por defecto**, se activa,
se limita y se audita desde `config.yaml`. Las decisiones y su porqué están
en [ADR-034](architecture/decisions/ADR-034-webdav.md).

WebDAV no es una vía paralela al disco: pasa por las mismas reglas que la
API REST y la web — papelera, versionado, pools, propiedad de los archivos y
auditoría. Lo que subes por WebDAV lo ves en la web y al revés.

## Activarlo

```yaml
webdav:
  enabled: true
  path: "/webdav"          # prefijo de URL, sin barra final
  readOnly: false          # true = solo consultar (OPTIONS/GET/HEAD/PROPFIND)
  maxUploadSizeBytes: 0    # 0 = sin límite; si no, 413 para subidas mayores

security:
  rateLimit:
    webdavPerMinute: 1200  # cubo propio por IP, aparte del de la API
```

| Opción | Variable de entorno | Por defecto |
|---|---|---|
| `webdav.enabled` | `NEXUSCLOUD_WEBDAV_ENABLED` | `false` |
| `webdav.path` | `NEXUSCLOUD_WEBDAV_PATH` | `/webdav` |
| `webdav.readOnly` | `NEXUSCLOUD_WEBDAV_READ_ONLY` | `false` |
| `webdav.maxUploadSizeBytes` | `NEXUSCLOUD_WEBDAV_MAX_UPLOAD_SIZE_BYTES` | `0` (sin límite) |
| `security.rateLimit.webdavPerMinute` | — | `1200` |

Se sirve por el **mismo puerto** que la API, bajo `path`. `path` no puede
colgar de `/api`, `/health` ni `/ready` (`nexuscloud config validate` lo
rechaza). Con `enabled: false` no se monta ninguna ruta y las de gestión de
tokens tampoco existen.

> **Usa HTTPS.** WebDAV autentica con HTTP Basic: el token viaja en cada
> petición. Por HTTP plano iría sin cifrar. Sirve NexusCloud con TLS
> (`server.tlsCertFile`/`tlsKeyFile`) o detrás de un proxy inverso con TLS, y
> en ese caso declara su IP en `server.trustedProxies` para que el límite de
> tasa y la auditoría vean la IP real del cliente. Si el proxy es nginx,
> revisa además `client_max_body_size` (por defecto rechaza subidas de más de
> 1 MB) y `proxy_request_buffering off` para no almacenar en el proxy los
> ficheros grandes.

## Credenciales: tokens de acceso

Un cliente WebDAV no puede hacer TOTP ni un passkey. Por eso **no se usa la
contraseña de tu cuenta**: aceptarla dejaría a cualquiera que la conozca
leyendo y borrando todo saltándose tu segundo factor. En su lugar creas un
**token de acceso WebDAV por dispositivo**, que se usa como contraseña:

- **Usuario:** tu nombre de usuario de NexusCloud.
- **Contraseña:** el token (`nwd_…`).
- **URL:** `https://tu-servidor/webdav/`.

### Crearlo desde la web

Cuenta → **Acceso WebDAV** → *Crear acceso*. Ponle un nombre que reconozcas
(«portátil de casa»). La web te enseña **una sola vez** la dirección, el
usuario y el token, con un botón de copiar para cada uno; cuando cierras el
aviso no hay forma de volver a verlo (solo se guarda su huella). Si lo
pierdes, revoca ese acceso y crea otro. La sección solo aparece si el
administrador ha activado WebDAV.

### Crearlo desde la línea de comandos

```sh
nexuscloud --config config.yaml users webdav-token create --username maria --label "portátil de casa"
nexuscloud --config config.yaml users webdav-token list --username maria
nexuscloud --config config.yaml users webdav-token revoke <id-del-token> --username maria
```

`create` imprime el token una sola vez, en su propia línea. `list` muestra el
`id`, el nombre, cuándo se creó y cuándo se usó por última vez (así se
detecta un token que ya no usa nadie). `revoke` lo invalida al instante: la
siguiente petición de ese dispositivo recibe 401. Es también la vía de
recuperación si un token se filtra.

Un token solo sirve para WebDAV: no abre la web ni la API. Si el usuario se
desactiva, sus tokens dejan de funcionar; si se borra, se borran con él.

## Conectar clientes

### rclone (verificado)

rclone exige la contraseña ofuscada:

```sh
rclone obscure <token>          # imprime la contraseña ofuscada
rclone config create nexus webdav \
  url https://tu-servidor/webdav vendor other \
  user maria pass <lo-que-imprimió-obscure>

rclone lsd nexus:                                   # listar carpetas
rclone copy ./fotos nexus:fotos --transfers 8       # subir
rclone check ./fotos nexus:fotos --download         # comprobar byte a byte
rclone sync nexus:fotos ./copia-local               # bajar
```

`vendor other` es el valor correcto. Con este vendor rclone no puede fijar la
fecha de modificación en el servidor, así que compara por tamaño: un segundo
`sync` sin cambios no transfiere nada.

### Linux: `davfs2` (parcialmente verificado)

```sh
sudo apt install davfs2
echo 'https://tu-servidor/webdav/ maria <token>' | sudo tee -a /etc/davfs2/secrets   # chmod 600
sudo mount -t davfs https://tu-servidor/webdav/ /mnt/nexuscloud
```

Crear, leer, sobrescribir, renombrar (davfs2 bloquea con `LOCK` y renombra
con `MOVE` + `If`), copiar, renombrar carpetas y ficheros grandes funcionan.
En la máquina de pruebas `ls` y `rm -r` fallaron con `Invalid argument`, pero
**también contra un servidor WebDAV de referencia independiente**, así que
es un problema de ese entorno y no se pudo evaluar. Comprueba tu caso.

### Windows (no verificado)

Explorador de archivos → *Conectar a unidad de red…* → carpeta
`https://tu-servidor/webdav/` → marca *Conectar con otras credenciales* →
tu usuario y el token. O por consola:

```bat
net use Z: https://tu-servidor/webdav/ /user:maria <token>
```

El cliente WebDAV integrado de Windows (servicio *WebClient*) **rechaza por
defecto Basic sobre HTTP**: necesitas HTTPS (o cambiar `BasicAuthLevel` en el
registro, que no se recomienda). También suele limitar por defecto el tamaño
de los ficheros que transfiere; consulta la documentación de Microsoft si
necesitas ficheros grandes. **No hemos podido probarlo con NexusCloud.**

### macOS Finder (no verificado)

Finder → *Ir* → *Conectarse al servidor…* (⌘K) →
`https://tu-servidor/webdav/` → usuario y token. El servidor anuncia
`DAV: 1, 2` (soporte de bloqueos), que es lo que Finder necesita para montar
con escritura en vez de solo lectura. **No hemos podido probarlo.**

### Cyberduck y otros clientes gráficos

Protocolo *WebDAV (HTTPS)*, servidor `tu-servidor`, puerto 443, ruta
`/webdav/`, usuario y token como contraseña.

## Qué hace cada operación

| Operación | Comportamiento |
|---|---|
| `GET` / `HEAD` | Descarga en streaming, con `Range` (reanudar). ETag = SHA-256 del contenido. |
| `PUT` | Crea el archivo, o **sobrescribe** uno existente dejando el contenido anterior como versión (mismo versionado que la API). Una subida cortada a mitad **no deja nada**: nunca se confirma un archivo truncado. |
| `DELETE` | Archivo: a la papelera (o borrado, con `trash.enabled: false`). Carpeta: **recursivo** (RFC 4918), ver límites. |
| `MKCOL` | Crea una carpeta; el padre tiene que existir (409 si no). 405 si ya existe. |
| `MOVE` | Mueve o renombra de verdad, con todo su contenido si es una carpeta. |
| `COPY` | Copia archivos y carpetas en el servidor, sin bajarlos al cliente. |
| `MOVE`/`COPY` con `Overwrite: T` sobre un **archivo** | El destino recibe el contenido nuevo y su contenido anterior queda como versión. El destino nunca pasa por la papelera. |
| `MOVE`/`COPY` con `Overwrite: T` sobre una **carpeta** | 403 sin tocar nada (con la papelera activa). Con `trash.enabled: false` se reemplaza entera, como manda el RFC. |
| `LOCK` / `UNLOCK` | Bloqueos exclusivos, en memoria. |
| `PROPFIND` | Nombre, tipo, tamaño, fecha, ETag y tipo MIME. |
| `PROPPATCH` | 403: no hay propiedades muertas. |

Cada usuario ve solo su propio árbol; no se expone «compartido conmigo».

## Límites conocidos

- **Un nombre en la papelera sigue ocupado.** Con la papelera activa, si
  borras un archivo o carpeta y luego intentas crear otro con el mismo
  nombre, el `PUT` o `MKCOL` da **405** hasta que restaures el original, lo
  borres definitivamente (Papelera → *Eliminar para siempre*) o pasen los
  `trash.retentionDays` (30 por defecto). Es la misma regla que aplica la API
  REST ([ADR-030](architecture/decisions/ADR-030-mover-archivos-carpetas.md),
  §128). Afecta a las aplicaciones que crean y borran ficheros temporales o
  de bloqueo con el mismo nombre (editores, suites ofimáticas). Si quieres usar
  WebDAV como unidad de red para eso, `trash.enabled: false` lo evita a costa
  de perder la red de seguridad de la papelera.
- **Borrar una carpeta con contenido llena la papelera de entradas.** Cada
  archivo y cada subcarpeta van a la papelera como entrada propia, y
  restaurar la carpeta recupera **solo la carpeta vacía**. Nada se pierde,
  pero recuperar un árbol entero exige restaurar sus entradas una a una. El
  borrado no es atómico: si algo falla a medias, lo ya borrado queda borrado.
- **No se puede reemplazar una carpeta entera** con `MOVE`/`COPY` +
  `Overwrite: T` mientras la papelera esté activa (403). Borra la carpeta
  destino antes, o muévela a otro nombre.
- **Sin propiedades muertas:** `PROPPATCH` responde 403.
- **Bloqueos solo exclusivos y en memoria:** los compartidos dan 501, y todos
  se pierden al reiniciar el servidor.
- **Sin cuotas:** `quota_bytes` se guarda en usuarios y grupos, pero ninguna
  subida —ni la API ni WebDAV— lo aplica todavía; `PROPFIND` responde 404 a las
  propiedades `quota-*`.
- **Sin «compartido conmigo».**
- **Los ficheros grandes dependen del proveedor de almacenamiento:** el
  provider de un pool debe permitir `Seek`; el local lo permite.

## Auditoría

Las operaciones de archivos por WebDAV generan los mismos eventos que la API
(`upload`, `download`, `delete`, `move`) con `via: webdav` en los metadatos,
más:

| Evento | Cuándo |
|---|---|
| `webdav_token_created` | Se crea un token (web, API o CLI). Guarda el nombre, nunca el token. |
| `webdav_token_revoked` | Se revoca un token. |
| `webdav_auth_failed` | Credenciales rechazadas: token desconocido, usuario que no coincide o cuenta desactivada. Guarda el usuario y la IP, **nunca** el token. Una petición sin cabecera `Authorization` (el primer intento normal de cualquier cliente) no se registra. |

```sh
nexuscloud --config config.yaml audit list --limit 50
```

## Solución de problemas

| Síntoma | Causa probable |
|---|---|
| 401 | Token inventado, revocado o de otro usuario; el usuario no coincide con el dueño del token; se usó la contraseña de la cuenta (no vale); la cuenta está desactivada. |
| 403 en cualquier escritura | `webdav.readOnly: true`. |
| 403 en `MOVE`/`COPY` | Se intenta reemplazar una carpeta existente (ver límites). |
| 405 en `PUT` o `MKCOL` | El nombre está ocupado por algo en la papelera, o `MKCOL` sobre algo que ya existe. |
| 409 | El padre de la ruta no existe. |
| 412 en un `MOVE` con `If:` | Un token de bloqueo que no existe (por ejemplo, tras reiniciar el servidor: los bloqueos se pierden). |
| 413 | Subida mayor que `webdav.maxUploadSizeBytes` (o límite del proxy inverso). |
| 429 | Demasiadas peticiones desde esa IP: sube `security.rateLimit.webdavPerMinute` o revisa `server.trustedProxies` si hay proxy delante (si no, todos los clientes comparten la IP del proxy y el mismo cubo). |

## Verificación

Cómo se ha probado, con qué resultado y qué **no** se ha podido probar (Windows,
Finder, Office) está en [ADR-034](architecture/decisions/ADR-034-webdav.md#consecuencias).
En resumen: `rclone` completo (100 MB con SHA-256 idéntico, 300 archivos en
paralelo comprobados byte a byte, `COPY`/`MOVE` en servidor, nombres con
`ñ`/emoji/espacios), la suite de conformidad `litmus` (67 de 80; los 13
fallos son los límites de arriba) y `davfs2` (montaje real), además de las
pruebas automáticas contra el servidor real.
