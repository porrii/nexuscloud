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

## Usuarios, grupos y doble factor (`users`)

```
nexuscloud users list
nexuscloud users create --username <usuario> [--password '...'] [--role user]
nexuscloud users disable <usuario>
```

Grupos (para poder compartir carpetas/archivos con varias personas a la
vez en vez de una por una):

```
nexuscloud users group create <nombre>
nexuscloud users group add-member <usuario> <grupo>
```

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

## Archivos de un usuario (`files`)

El día a día: subir, bajar, listar, mover y borrar archivos, en nombre de
un usuario concreto (por eso `--username` es obligatorio en los seis
subcomandos — es una herramienta de administrador, no un login).

```
nexuscloud files list --username <usuario> [ruta]              # por defecto, la raíz "/"
nexuscloud files mkdir --username <usuario> <ruta>
nexuscloud files upload --username <usuario> <local> <ruta-remota>
nexuscloud files download --username <usuario> <ruta-remota> <local>
nexuscloud files mv --username <usuario> <origen> <destino>
nexuscloud files rm --username <usuario> <ruta> [--permanent]  # por defecto va a la papelera
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

## Lo que todavía no tiene CLI (a propósito, backlog documentado)

Cubre operaciones raras en una instalación de un solo administrador:
gestionar sesiones/papelera/versiones de OTRO usuario, invitaciones,
reactivar/editar/borrar un usuario ya creado, y consultar el registro de
auditoría. Para esas, de momento, la API HTTP
([`nexuscloud/docs/api.md`](../nexuscloud/docs/api.md)) es la única vía.
