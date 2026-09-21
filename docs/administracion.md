# Administración: usuarios, grupos, sesiones, compartición, doble factor

Todo lo de aquí se hace por CLI, siempre como el usuario de servicio
(`sudo -u nexuscloud ...` en Linux) o directamente en Windows. Referencia
rápida de cada comando en [`comandos.md`](comandos.md); esta página es la
guía de "cómo hago X" paso a paso.

## Usuarios

### Crear un usuario

```sh
nexuscloud --config config.yaml users create --username maria --password 'una-contraseña-de-verdad'
```

Sin `--password`, la pide de forma interactiva. Rol `user` por defecto —
para dar de alta a otro administrador, usa
`admin create-user --role administrator` en su lugar (ver
[`comandos.md`](comandos.md#administración-de-la-instancia-admin)).

### Ver quién tiene cuenta

```sh
$ nexuscloud --config config.yaml users list
47ac9197-2045-46a8-8609-f14e656401eb  admin                 active      admin
1cb35768-8452-4773-aa72-ea9e387ed639  maria                 active      maria
```

### Deshabilitar / reactivar un usuario

```sh
nexuscloud --config config.yaml users disable maria
nexuscloud --config config.yaml users enable maria
```

`disable` bloquea el login inmediatamente; sus archivos y configuración
no se tocan, y `enable` lo devuelve exactamente al estado anterior.

### Editar nombre visible o email

```sh
nexuscloud --config config.yaml users edit maria --display-name "María Real" --email maria@ejemplo.com
```

Al menos uno de los dos flags es obligatorio; el que omitas no se toca.

### Borrar un usuario para siempre

```sh
nexuscloud --config config.yaml users delete maria --confirm
```

**Irreversible y en cascada**: sesiones, pertenencia a grupos,
invitaciones que hubiera creado, y sus comparticiones desaparecen con
él; los METADATOS de todos sus archivos y carpetas también — pero el
contenido físico en el Storage Pool **no se borra solo, queda huérfano
en disco** (limitación real, no un efecto secundario de este comando:
la API HTTP tiene exactamente la misma limitación). Sin `--confirm`, el
comando se niega a hacer nada y explica lo que borraría. Ante la duda,
usa `disable` en su lugar — se puede deshacer, esto no.

## Invitaciones

Alternativa a `users create` cuando quieres que sea la otra persona
quien elija su contraseña, en vez de crearle tú la cuenta directamente:

```sh
$ nexuscloud --config config.yaml users invitation create --created-by admin --max-uses 1 --ttl-hours 24
Invitación creada (id=b0672a58-b591-4b58-b728-8f00be49454f)
Token (dáselo a la persona que se va a dar de alta; se muestra una sola vez): <token>
Expira: 2026-09-16 14:30:00 -- usos máximos: 1

$ nexuscloud --config config.yaml users invitation list
ID                                    ROL             USOS  EXPIRA                ESTADO      CREADA POR
b0672a58-b591-4b58-b728-8f00be49454f  user (def.)      0/1  2026-09-16 14:30:00   usable      <id-de-admin>

$ nexuscloud --config config.yaml users invitation revoke b0672a58-b591-4b58-b728-8f00be49454f
Invitación b0672a58-b591-4b58-b728-8f00be49454f revocada.
```

`--created-by` es obligatorio (identifica al administrador real que la
crea; si ese administrador se borra después, la invitación se borra en
cascada con él). El canje en sí (`POST /api/v1/invitations/redeem`) es
público y sin comando CLI a propósito: es la propia persona invitada
quien lo hace, con su usuario y contraseña elegidos, desde la web o por
API. Revocar antes de canjear invalida el token para siempre, aunque
todavía tuviera usos disponibles.

## Sesiones activas de un usuario

```sh
$ nexuscloud --config config.yaml sessions list --username maria
ID                                    DISPOSITIVO           IP               CREADA                ÚLTIMA ACTIVIDAD      ESTADO
54cb7325-9248-42e7-9cdd-15b1e8a17965  Mozilla/5.0 ...       203.0.113.9      2026-09-15 14:20:00   2026-09-15 14:35:00   activa

$ nexuscloud --config config.yaml sessions revoke --username maria 54cb7325-9248-42e7-9cdd-15b1e8a17965
Sesión 54cb7325-9248-42e7-9cdd-15b1e8a17965 revocada.
```

Revocar invalida ese token de sesión al instante — la próxima petición
con él da 401, sin que "maria" tenga que hacer nada. Útil si sospechas
que alguien más tiene acceso a su sesión, o simplemente cambió de
equipo y quieres limpiar accesos viejos.

## Compartición

```sh
$ nexuscloud --config config.yaml files upload --username maria informe.txt /Documentos/informe.txt
$ nexuscloud --config config.yaml shares create --username maria /Documentos/informe.txt --share-type user --target-username juan
Share creado (id=...)

$ nexuscloud --config config.yaml shares list --username juan --with-me
ID    TIPO  RECURSO       DESTINO  EXPIRA  ESTADO
...   user  informe.txt   ...      -       activo
```

Comparticiones con un grupo entero (en vez de usuario por usuario) usan
`--share-type group --target-group <nombre>` — cualquier miembro actual
del grupo ve el recurso en su `shares list --with-me`, sin tener que
compartir con cada persona por separado. Un enlace público
(`--share-type link`) imprime un token en claro una sola vez; quien lo
tenga accede sin sesión (`GET /api/v1/public/shares/<token>/download`) —
exige `sharing.publicLinksEnabled: true` en `config.yaml` (desactivado
por defecto). Ver [`comandos.md`](comandos.md#compartir-archivos-y-carpetas-shares)
para todas las opciones (`--can-upload`, `--password`, `--expires-at`,
límites de descarga/tamaño).

## Grupos

Sirven para compartir carpetas/archivos con varias personas a la vez (una
familia, un equipo) en vez de usuario por usuario. Compartir con un grupo
en sí se hace desde la interfaz web o por API (`POST /api/v1/shares` con
`share_type: "group"`) — el CLI cubre crear el grupo y meter gente dentro:

```sh
$ nexuscloud --config config.yaml users group create Familia
Grupo "Familia" creado (id=b8da3154-b428-4772-b11a-ebe2d38aacc1)

$ nexuscloud --config config.yaml users group add-member maria Familia
Usuario "maria" añadido al grupo "Familia"
```

`add-member` es idempotente: repetir la misma llamada no da error ni
duplica nada. Un nombre de grupo repetido sí da error (409/"ya existe").

## Doble factor (TOTP)

Compatible con cualquier app autenticadora estándar (Google Authenticator,
Aegis, 1Password, Authy...). El proceso es de dos pasos a propósito: el
secreto generado en `enroll` **no queda activo** hasta que confirmas un
código real generado por tu app en `verify` — así nunca puedes activar 2FA
por accidente con un secreto que nunca llegaste a registrar y quedarte
fuera de tu propia cuenta.

```sh
$ nexuscloud --config config.yaml users totp enroll --username maria
Secreto: JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R
URL (pégala en tu app autenticadora, o genera un QR con ella): otpauth://totp/NexusCloud:maria?algorithm=SHA1&digits=6&issuer=NexusCloud&period=30&secret=JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R

Todavía NO está activo. Genera un código con tu app y confirma con:
  nexuscloud users totp verify --username maria --secret JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R <código>
```

Copia esa URL en tu app autenticadora (o convierte la URL en un código QR
con cualquier generador y escanéala), espera al código de 6 dígitos que te
muestre, y confirma:

```sh
$ nexuscloud --config config.yaml users totp verify --username maria --secret JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R 450456
2FA activado para "maria": a partir de ahora el login exigirá también el código TOTP.
```

A partir de aquí, el login de "maria" (por la API, o por cualquier cliente
que la use) exige el código además de la contraseña.

### Si algo sale mal (recuperar el acceso)

```sh
nexuscloud --config config.yaml users totp disable --username maria
```

Desactiva 2FA para ese usuario sin necesitar ningún código — la vía de
recuperación si se perdió el teléfono con la app, o el secreto se copió
mal. En un servidor headless de un solo administrador, esto es
importante: no hay ninguna otra forma de deshacerlo.

## Passkeys (WebAuthn)

Alternativa a TOTP como segundo factor (más fuerte, resistente a
phishing) y, con el mismo passkey, también login sin contraseña. A
diferencia de TOTP, **desactivado por defecto** y requiere configurar el
dominio real del servidor primero — sin eso, el navegador rechaza
cualquier passkey:

```yaml
security:
  webAuthn:
    enabled: true
    rpID: "nexuscloud.tu-dominio.com"      # solo el dominio, sin esquema ni puerto
    rpOrigin: "https://nexuscloud.tu-dominio.com"
```

`rpOrigin` debe usar `https://` (o `http://localhost` solo en desarrollo
local) — WebAuthn lo exige.

Registrar un passkey **solo puede hacerse desde la web** (Cuenta →
Passkeys → "Añadir passkey"): el navegador es quien genera el par de
claves y lo guarda en el propio dispositivo/llave, no hay equivalente
por CLI ni por API sin pasar por `navigator.credentials.create()`. Si un
usuario ya tiene algún passkey registrado, el login (por cualquier vía)
lo exigirá con prioridad sobre TOTP aunque también tenga TOTP activado.

### Si algo sale mal (recuperar el acceso)

```sh
nexuscloud --config config.yaml users webauthn list --username maria
nexuscloud --config config.yaml users webauthn revoke <id-del-passkey> --username maria
```

`list` muestra el `id` interno de cada passkey (no el que da el
navegador) junto con su nombre y fecha de último uso. `revoke` quita ese
passkey concreto sin necesitar el dispositivo físico — la vía de
recuperación si se perdió la llave/el teléfono, mismo criterio que
`users totp disable`. Si el usuario se queda sin ningún passkey y no
tiene TOTP activado, el login vuelve a pedir solo contraseña.

## Acceso WebDAV (unidad de red)

Permite montar el espacio de un usuario como unidad de red o usarlo desde
clientes estándar (Explorador de Windows, Finder, rclone, Cyberduck...).
**Desactivado por defecto**:

```yaml
webdav:
  enabled: true
  path: "/webdav"          # se sirve por el mismo puerto que la API
  readOnly: false          # true = solo consultar, nadie puede escribir
  maxUploadSizeBytes: 0    # 0 = sin límite
```

Cada persona crea sus propios **tokens de acceso** (uno por dispositivo) en
la web —Cuenta → Acceso WebDAV— y los usa como contraseña junto a su nombre
de usuario. **La contraseña de la cuenta no vale por WebDAV**, a propósito:
los clientes WebDAV no pueden hacer el segundo factor. **Usa HTTPS**: el
token viaja en cada petición.

Por CLI (por ejemplo, para preparar un dispositivo desde el servidor, o para
cortar un acceso sin que la persona intervenga):

```sh
nexuscloud --config config.yaml users webdav-token create --username maria --label "portátil de casa"
nexuscloud --config config.yaml users webdav-token list --username maria
nexuscloud --config config.yaml users webdav-token revoke <id-del-token> --username maria
```

`create` imprime el token una sola vez. `list` muestra cuándo se usó cada
uno por última vez, para detectar los que ya no usa nadie. `revoke` lo
invalida al instante y es la vía de recuperación si un token se filtra.

Lo que se sube por WebDAV pasa por la misma papelera, versionado y auditoría
que la web. Un límite importante: **con la papelera activa, un nombre borrado
no se puede reutilizar hasta restaurarlo, eliminarlo para siempre o que
caduque la retención**; eso rompe a las aplicaciones que borran y recrean
ficheros temporales con el mismo nombre. Si quieres WebDAV como unidad de red
para trabajar con editores, `trash.enabled: false` lo evita a costa de la red
de seguridad. Detalle completo, guía por cliente y límites conocidos en
[`nexuscloud/docs/webdav.md`](../nexuscloud/docs/webdav.md).

## Papelera y versiones de otro usuario

El administrador puede gestionar la papelera y el historial de
versiones de CUALQUIER usuario sin que ese usuario intervenga —
ver [`comandos.md`](comandos.md#archivos-de-un-usuario-files)
(`files trash list/restore`, `files versions list/download/restore`).

## Registro de auditoría

```sh
nexuscloud --config config.yaml audit list --limit 50
```

Login, altas/bajas, compartición, etc. — ver
[`comandos.md`](comandos.md#registro-de-auditoría-audit). Solo las
acciones que pasan por la API HTTP se auditan (login incluido); las
operaciones puramente de CLI de esta página no generan eventos de
auditoría hoy.
