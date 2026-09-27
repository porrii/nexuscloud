# Referencia de comandos

Todo lo que se puede hacer con NexusCloud se puede hacer por línea de
comandos — pensado para un servidor Linux **sin entorno gráfico**, donde el
CLI no es una alternativa a la web, es la única interfaz que existe. Esta
página reúne los comandos reales con ejemplos probados; para el contrato
HTTP (si quieres hablar con la API directamente) ver
[`nexuscloud/docs/api.md`](../nexuscloud/docs/api.md).

Todos los comandos aceptan `--config <ruta>` (si no se indica, usa
`./config.yaml` si existe). En una instalación con `install.sh`, la ruta
real es `/etc/nexuscloud/config.yaml`, así que en los ejemplos siguientes
sustituye `--config config.yaml` por esa ruta, y ejecuta como el usuario de
servicio: `sudo -u nexuscloud nexuscloud --config /etc/nexuscloud/config.yaml ...`.

## Arranque y configuración

```
nexuscloud config init --out config.yaml      # genera un config.yaml con valores seguros por defecto
nexuscloud config validate                    # valida el fichero actual sin arrancar nada
nexuscloud migrate up                         # aplica el esquema de base de datos (idempotente)
nexuscloud migrate status                     # versión de esquema actual
nexuscloud doctor                             # comprueba configuración/BD/almacenamiento/puerto/TLS
nexuscloud start                              # arranca el servidor en primer plano (Ctrl+C para parar)
nexuscloud version                            # versión del binario
```

`doctor` de verdad, contra una instalación real:

```
$ nexuscloud --config config.yaml doctor
PASS      Configuration        configVersion=1
PASS      Database             sqlite, esquema v7
PASS      Storage              /var/lib/nexuscloud/storage
PASS      Port                 0.0.0.0:8080
WARNING   HTTPS                TLS no configurado; aceptable en LAN, revisar antes de exponer a Internet (§67)
PASS      Web pública          desactivada (secure by default, §3/§47)
PASS      Public registration  desactivado
```

## Administración de la instancia (`admin`)

```
nexuscloud admin create-user --username admin --password '...' [--role super_admin|administrator|user|read_only]
```

Solo para el **primer** usuario (o para dar de alta a otro administrador
directamente). El primero creado recibe `super_admin` automáticamente si no
se indica `--role`. Nunca se permite `admin`/`admin` ni una contraseña de
menos de 8 caracteres.

## Usuarios, grupos, invitaciones y doble factor (`users`)

```
nexuscloud users list
nexuscloud users create --username <usuario> [--password '...'] [--role user] [--quota <valor>]
nexuscloud users disable <usuario>
nexuscloud users enable <usuario>
nexuscloud users edit <usuario> [--display-name <n>] [--email <e>] [--quota <valor>]
nexuscloud users delete <usuario> --confirm
nexuscloud users quota [<usuario>] [--detail]
```

`--quota` fija la cuota de almacenamiento propia del usuario: un tamaño
(`100GB`, `1.5TB`, `500MB` o bytes a secas; binarios, 1 GB = 1 GiB),
`unlimited` (sin límite aunque el grupo o la global tengan uno) o `inherit`
(quita la cuota propia y hereda). `users quota` muestra, por usuario, lo que
ocupa (archivos + papelera + versiones), su cuota efectiva, el porcentaje y de
dónde sale (`usuario`, `grupo:<nombre>` o `global`); `--detail` desglosa el uso.
Guía completa en [`administracion.md`](administracion.md#cuotas-de-almacenamiento).

`delete` es **irreversible** y en cascada (sesiones, pertenencia a
grupos, invitaciones creadas por ese usuario, comparticiones, y los
METADATOS de todos sus archivos/carpetas) — por eso exige `--confirm`
explícito, y por eso mismo el contenido físico de esos archivos en el
Storage Pool **no se borra solo, queda huérfano en disco** (limitación ya
existente en la API HTTP, no de este comando). Ante la duda, usa
`disable` en su lugar.

Invitaciones (alternativa a `users create` para que otra persona elija su
propia contraseña; sigue sin haber registro público):

```
nexuscloud users invitation create --created-by <admin> [--role <rol>] [--max-uses <n>] [--ttl-hours <n>]
nexuscloud users invitation list
nexuscloud users invitation revoke <id>
```

`create` imprime el token en claro **una sola vez**; quien lo recibe lo
canjea por la web o por API (`POST /api/v1/invitations/redeem`, pública,
sin comando CLI a propósito — es autoalta, no una operación de
administrador). `revoke` antes de canjear invalida el token para
siempre.

Grupos (para poder compartir carpetas/archivos con varias personas a la
vez en vez de una por una):

```
nexuscloud users group create <nombre> [--quota <valor>]
nexuscloud users group edit <grupo> --quota <valor>
nexuscloud users group add-member <usuario> <grupo>
```

La cuota de un grupo es un límite **por miembro** (cada miembro puede usar hasta
esa cantidad salvo que tenga cuota propia), con los mismos valores que la del
usuario. Si alguien está en varios grupos con cuota, vale la más generosa.

Doble factor (TOTP, compatible con cualquier app autenticadora estándar —
Google Authenticator, Aegis, 1Password, etc.). Se hace en dos pasos a
propósito: el secreto no queda activo hasta que confirmas un código real,
así nunca puedes quedarte bloqueado por copiar mal el secreto en la app:

```
nexuscloud users totp enroll --username <usuario>
#   Secreto: JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R
#   URL (pégala en tu app autenticadora, o genera un QR con ella): otpauth://totp/...
#
#   Todavía NO está activo. Genera un código con tu app y confirma con:
#     nexuscloud users totp verify --username <usuario> --secret JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R <código>

nexuscloud users totp verify --username <usuario> --secret <el-mismo-secreto> <código-de-6-dígitos>
#   2FA activado para "<usuario>": a partir de ahora el login exigirá también el código TOTP.

nexuscloud users totp disable --username <usuario>   # recupera el acceso si algo salió mal (app perdida, secreto mal copiado)
```

Passkeys (WebAuthn, requiere `security.webAuthn.enabled=true` en
`config.yaml` — ver [`administracion.md`](administracion.md#passkeys-webauthn)).
El alta solo puede hacerse desde la web (exige `navigator.credentials.
create()` del navegador); por CLI solo se listan/revocan los ya
registrados, para recuperar el acceso:

```
nexuscloud users webauthn list --username <usuario>
nexuscloud users webauthn revoke <id-del-passkey> --username <usuario>   # recupera el acceso si se perdió la llave/el teléfono
```

Tokens de acceso WebDAV (requiere `webdav.enabled=true` en `config.yaml` —
ver [`administracion.md`](administracion.md#acceso-webdav-unidad-de-red)).
Son la contraseña que usa un cliente WebDAV (nunca la de la cuenta). A
diferencia de los passkeys, sí se pueden crear por CLI:

```
nexuscloud users webdav-token create --username <usuario> [--label "portátil de casa"]   # imprime el token UNA vez
nexuscloud users webdav-token list --username <usuario>                                   # id, nombre, creado, último uso
nexuscloud users webdav-token revoke <id-del-token> --username <usuario>                 # lo invalida al instante
```

Tokens de API (§78, ADR-037) — siempre disponibles, sin opción de config que
los desactive. El token actúa exactamente como el usuario en cualquier
endpoint (alcance todo-o-nada); ver
[`administracion.md`](../docs/administracion.md#tokens-de-api):

```
nexuscloud users api-token create --username <usuario> [--label "script"] [--expires-in 30d|90d|1y|never]   # imprime el token UNA vez
nexuscloud users api-token list --username <usuario>                                                          # id, nombre, creado, expira, último uso
nexuscloud users api-token revoke <id-del-token> --username <usuario>                                       # lo invalida al instante
```

## Archivos de un usuario (`files`)

El día a día: subir, bajar, listar, mover y borrar archivos, en nombre de
un usuario concreto (por eso `--username` es obligatorio en todos los
subcomandos — es una herramienta de administrador, no un login).

```
nexuscloud files list --username <usuario> [ruta]              # por defecto, la raíz "/"
nexuscloud files mkdir --username <usuario> <ruta>
nexuscloud files upload --username <usuario> <local> <ruta-remota>
nexuscloud files download --username <usuario> <ruta-remota> <local>
nexuscloud files mv --username <usuario> <origen> <destino>
nexuscloud files rm --username <usuario> <ruta> [--permanent]  # por defecto va a la papelera
```

Papelera y versiones **de cualquier usuario**, gestionadas por el
administrador sin que ese usuario tenga que hacer nada:

```
nexuscloud files trash list --username <usuario>
nexuscloud files trash restore --username <usuario> <id>          # el id lo da "files trash list"
nexuscloud files versions list --username <usuario> <ruta>
nexuscloud files versions download --username <usuario> <ruta> <num-versión> <local>
nexuscloud files versions restore --username <usuario> <ruta> <num-versión>
```

Ejemplo real, de principio a fin:

```
$ nexuscloud files mkdir --username maria /Documentos
Carpeta creada: /Documentos

$ nexuscloud files upload --username maria informe.txt /Documentos/informe.txt
Subido: /Documentos/informe.txt (33 B)

$ nexuscloud files list --username maria /Documentos
FILE         33 B  informe.txt

$ nexuscloud files download --username maria /Documentos/informe.txt copia.txt
Descargado: /Documentos/informe.txt -> copia.txt (33 B)

$ nexuscloud files mv --username maria /Documentos/informe.txt /Documentos/informe-final.txt
Movido: /Documentos/informe.txt -> /Documentos/informe-final.txt

$ nexuscloud files rm --username maria /Documentos/informe-final.txt
Borrado: /Documentos/informe-final.txt
```

`rm` sin `--permanent` manda a la papelera (recuperable durante el periodo
de retención configurado, `trash.retentionDays`); con `--permanent` borra
de verdad, sin paso intermedio.

## Almacenamiento: discos, Storage Pools, RAID, instantáneas (`storage`)

```
nexuscloud storage disks                    # unidades de almacenamiento detectadas en este equipo
nexuscloud storage list                     # Storage Pools configurados (id, estado, utilización, prioridad, ruta)
nexuscloud storage add --name <n> --path <ruta> [--priority <n>]
nexuscloud storage enable <id>
nexuscloud storage disable <id>
nexuscloud storage remove <id>              # falla si el pool todavía tiene archivos o carpetas
nexuscloud storage set-policy <id> [--backup inherit|on|off] [--versioning inherit|on|off] [--utilization fill|round-robin|manual]
nexuscloud storage raid                     # arrays RAID ya gestionados por el sistema operativo (mdadm/Storage Spaces)
nexuscloud storage snapshots                # instantáneas ya existentes (VSS en Windows, ZFS/Btrfs en Linux)
```

Real, con dos pools:

```
$ nexuscloud storage add --name respaldo --path /mnt/disco2/nexuscloud --priority 50
Storage Pool creado: respaldo (9c38e766-b789-45b6-a6e0-45560a1b5d49) en /mnt/disco2/nexuscloud

$ nexuscloud storage list
ID                                    NOMBRE         ESTADO   UTILIZACIÓN  PRIO FICHEROS  RUTA
00dc7479-7c4c-43e5-be1f-8a9ec72f0ab5  default        active   fill            0        0  /var/lib/nexuscloud/storage
9c38e766-b789-45b6-a6e0-45560a1b5d49  respaldo       active   fill           50        0  /mnt/disco2/nexuscloud
```

`storage raid`/`storage snapshots` **detectan** lo que el sistema operativo
ya gestiona — NexusCloud no implementa RAID ni snapshots propios, delega en
mdadm/Storage Spaces/ZFS/Btrfs/VSS, según lo que ya tengas montado.

## Sesiones activas de un usuario (`sessions`)

```
nexuscloud sessions list --username <usuario>
nexuscloud sessions revoke --username <usuario> <session-id>       # el id lo da "sessions list"
```

Revocar por CLI invalida de verdad el token de esa sesión en el acto —
la próxima petición HTTP con ese token da 401, aunque el usuario no haya
cerrado sesión él mismo (útil si sospechas que a alguien le robaron el
acceso, o simplemente cambió de equipo).

## Compartir archivos y carpetas (`shares`)

```
nexuscloud shares create --username <owner> <ruta> --share-type user|group|link
    [--target-username <u>] [--target-group <g>] [--label <l>]
    [--can-upload] [--no-download] [--password <p>]
    [--expires-at <RFC3339>] [--max-downloads <n>] [--max-upload-size-bytes <n>]
nexuscloud shares list --username <usuario> [--with-me]
nexuscloud shares revoke --username <usuario> <share-id>
```

- `--share-type user`/`group` exige `--target-username`/`--target-group`
  respectivamente. Por defecto dan solo lectura; con `--can-upload` (solo
  sobre una **carpeta**) dan «lectura y subida»: la persona o el grupo
  puede subir archivos nuevos a esa carpeta y a sus subcarpetas desde
  «Compartido» en la web. No sobrescribe un archivo existente, y quien
  sube no puede renombrar, borrar ni crear subcarpetas;
  `--max-upload-size-bytes` limita el tamaño por archivo. Un archivo con
  `--can-upload`, o `--can-upload` junto con `--no-download`, se rechaza.
- `--share-type link` imprime el **token en claro una sola vez** — es lo
  que hay que compartir para poder acceder sin sesión
  (`GET /api/v1/public/shares/<token>/download`). Exige
  `sharing.publicLinksEnabled: true` en `config.yaml` (desactivado por
  defecto).
- `shares list` sin `--with-me` muestra lo que TÚ has compartido; con
  `--with-me`, lo que otros han compartido contigo (directo, o vía un
  grupo del que seas miembro). La columna `PERMISOS` dice qué permite
  cada share: `lectura`, `lectura+subida` o (un enlace de carpeta con
  `--no-download`) `subida`. Un share revocado deja de aparecer en
  ambos listados (el historial vive en el registro de auditoría, no
  aquí).

## Registro de auditoría (`audit`)

```
nexuscloud audit list [--limit <n>] [--offset <n>]
```

Login, altas/bajas de usuario, compartición, etc. — por defecto los 100
más recientes (`--limit` se ajusta solo si es `<= 0` o `> 500`, nunca da
error). Solo lo que ya pasa por la API HTTP se audita (login incluido);
las operaciones puramente de CLI (`files`, `users create`, etc.) no
generan eventos de auditoría hoy, al no pasar por esa capa.

## Backup Manager (`backup`)

Ver [`backup-y-recuperacion.md`](backup-y-recuperacion.md) para la guía
completa (programación, cifrado, destino remoto, incremental). Referencia
rápida:

```
nexuscloud backup run [--dest <ruta>] [--pool <id-o-nombre>]... [--keep-last N] [--keep-days N] [--incremental] [--encrypt]
nexuscloud backup list
nexuscloud backup verify <job-id>
nexuscloud backup restore <job-id> --dest <ruta>
nexuscloud backup restore-to-pool <job-id> --pool <id-o-nombre>
```

## Servicio del sistema operativo (`service`)

```
nexuscloud service install --config <ruta-absoluta>   # requiere administrador
nexuscloud service start|stop|restart|status
nexuscloud service uninstall
```

En Linux, `install.sh` ya usa la unit de systemd de referencia
(`deploy/systemd/nexuscloud.service`, con hardening) en vez de
`service install` directamente — usa `service install` tú mismo solo si
prefieres gestionarlo a mano o estás en un sistema sin `install.sh`.

## Lo que todavía no tiene CLI

Prácticamente todo lo relevante para el uso diario de una instalación de
un solo administrador ya tiene comando — lo único que queda solo por API
es lo que no encaja bien en un CLI de un solo paso: canjear una
invitación (autoalta, pública), y resolver/navegar/descargar un enlace
público (eso lo hace quien lo recibe, con un navegador o `curl`, no el
administrador). Para eso, la API HTTP
([`nexuscloud/docs/api.md`](../nexuscloud/docs/api.md)) sigue siendo la
vía.
