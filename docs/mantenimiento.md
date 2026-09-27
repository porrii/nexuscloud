# Mantenimiento

## Comprobar el estado de la instancia

```sh
nexuscloud --config config.yaml doctor
```

Comprueba configuración, base de datos, almacenamiento, puerto y TLS antes
de que algo falle de verdad. Real, contra una instalación sana:

```
PASS      Configuration        configVersion=1
PASS      Database             sqlite, esquema v7
PASS      Storage              /var/lib/nexuscloud/storage
PASS      Port                 0.0.0.0:8080
WARNING   HTTPS                TLS no configurado; aceptable en LAN, revisar antes de exponer a Internet (§67)
PASS      Web pública          desactivada (secure by default, §3/§47)
PASS      Public registration  desactivado
```

`PASS`/`WARNING` es normal en una LAN; cualquier `FAIL` hay que resolverlo.

```sh
nexuscloud --config config.yaml config validate    # solo la configuración, sin tocar nada más
nexuscloud --config config.yaml migrate status     # versión de esquema actual, "dirty" o no
```

## Verificar la detección de RAID/ZFS/Btrfs en tu máquina real

`nexuscloud storage raid`/`storage snapshots` **detectan** RAID/ZFS/Btrfs
ya existentes; no los crean. Si quieres comprobar que la detección
funciona de verdad en tu servidor real (no solo confiar en la teoría),
hay un script que crea un array/pool/subvolumen de usar y tirar sobre
ficheros de bucle (nunca toca tus discos reales) y compara contra lo que
`nexuscloud` detecta:

```sh
sudo nexuscloud/deploy/scripts/verify-raid-zfs-btrfs.sh
```

Si tu sistema no tiene alguno de los tres instalado (p.ej. no usas ZFS),
esa parte se salta sin contar como fallo — solo avisa si algo que SÍ
tienes instalado no se detectó correctamente.

## Logs

```sh
sudo journalctl -u nexuscloud -f          # en vivo
sudo journalctl -u nexuscloud --since today
```

Formato y nivel se controlan en `config.yaml` (`logging.level`,
`logging.format`: `text` o `json`, `logging.output`: `stdout` o una ruta
de fichero).

## Actualizar a una versión nueva

`install.sh` compila un binario nuevo, pero está pensado para una
instalación desde cero: no para el servicio ni hace copia del binario
anterior antes de sustituirlo. Para actualizar una instalación que ya
está en marcha, usa `update.sh` en su lugar:

```sh
git pull

# 1. Compila el binario nuevo sin tocar la instalación activa:
cd nexuscloud && CGO_ENABLED=0 go build -o /tmp/nexuscloud-nuevo ./cmd/nexuscloud && cd ..

# 2. Instálalo de forma segura: para el servicio, guarda copia del binario
#    anterior, migra, y si la migración falla, revierte el binario automáticamente:
sudo nexuscloud/deploy/scripts/update.sh /tmp/nexuscloud-nuevo
```

En Windows, equivalente con `nexuscloud\deploy\scripts\update.ps1
C:\ruta\al\nexuscloud-nuevo.exe`.

Las migraciones (`migrate up`) son siempre no destructivas — nunca borran
datos, así que actualizar es seguro incluso si el esquema cambió. Aun así,
para una actualización importante, haz copia de `storage.dataDir` (o al
menos de la base de datos) antes.

**Migración 0011 (tamaños de 64 bits, cuotas).** En PostgreSQL y MySQL las
columnas de tamaño en bytes eran de 32 bits —un máximo de 2 GiB por archivo y
por cuota— y esta migración las ensancha (`users`/`groups.quota_bytes`,
`files`/`file_versions.size_bytes`, `shares.max_upload_size_bytes`,
`backup_jobs.total_bytes`) y añade los índices que usa el cálculo de uso. Ambos
motores **reescriben la tabla** al cambiar el tipo (y `files`/`file_versions`
son las grandes) con un bloqueo exclusivo: en una instalación con muchos
millones de archivos puede tardar, así que hazla en una ventana tranquila y con
copia previa. SQLite no necesita cambios (sus enteros ya son de 64 bits). Los
datos existentes se conservan tal cual; la migración inversa solo funciona si
ningún valor supera ya los 2 GiB.

**Migración 0012 (propietario en cada versión).** `file_versions` pasa a llevar
el propietario de cada versión, copiado de su archivo, para calcular el uso de
un usuario sin un JOIN con todos sus archivos. Es una tabla mucho más pequeña
que `files`: en PostgreSQL y MySQL añade la columna, la rellena y crea un
índice; en SQLite reconstruye la tabla (única forma de declararla `NOT NULL`).
Tarda segundos incluso con cientos de miles de versiones, y no toca los
archivos ni su contenido. La migración inversa quita la columna y el índice sin
perder ninguna versión.

## Activar la auto-actualización del cliente de escritorio (Windows)

Por defecto está desactivada (secure by default, igual que la web o los
enlaces públicos). Para que el cliente pueda avisar de versiones nuevas y
actualizarse solo (ver [`cliente-de-escritorio.md`](cliente-de-escritorio.md#actualizar-windows)),
el servidor necesita reenviar el feed desde GitHub Releases del
repositorio del proyecto — el cliente nunca lleva ningún token propio
(ADR-032 en `nexuscloud/docs/architecture/decisions/`).

En `config.yaml`:

```yaml
clientUpdates:
  enabled: true
  githubRepo: "tu-usuario/tu-fork-de-nexuscloud"
  channel: win
```

Y en el entorno del servidor (nunca en `config.yaml`, igual que la
passphrase de backup o su token remoto):

```sh
export NEXUSCLOUD_CLIENT_UPDATES_GITHUB_TOKEN='un-token-de-github-con-acceso-a-releases'
```

Si el repositorio es privado (lo normal en un fork propio), el token es
obligatorio — sin él, GitHub aplica el límite de peticiones sin
autenticar y las releases privadas no son descargables de todos modos.
Reinicia el servicio tras cambiar esto.

## Añadir o quitar la interfaz web

Sin reinstalar desde cero, sobre una instalación que ya tienes funcionando
sin ella:

```sh
sudo nexuscloud/deploy/scripts/enable-web.sh
```

(`enable-web.ps1` en Windows). Instala Node.js si falta, compila la web
real, recompila el binario con ella embebida, activa `web.enabled` en tu
`config.yaml` real, y reinstala el binario de forma segura (mismo
mecanismo que `update.sh`). No hace falta parar el servicio a mano — el
script ya lo hace si estaba corriendo, y lo vuelve a arrancar al terminar.

Para quitarla de nuevo: recompila con `install.sh` normal (sin `--web`) y
pasa ese binario a `update.sh` — no hace falta un script aparte, ya
funciona con lo que existe.

## Desinstalar

```sh
sudo nexuscloud/deploy/scripts/uninstall.sh            # conserva datos y config
sudo nexuscloud/deploy/scripts/uninstall.sh --purge    # borra también datos y config
```

(`uninstall.ps1` / `-Purge` en Windows). Por seguridad, sin `--purge`
nunca se borran tus archivos ni la configuración — solo el servicio y el
binario.

## Problemas frecuentes

**El servicio no arranca.**
```sh
systemctl status nexuscloud
journalctl -u nexuscloud -n 50 --no-pager
```
Casi siempre: el puerto ya está en uso (cambia `server.port`), o
`storage.dataDir` no es escribible por el usuario `nexuscloud` (revisa
permisos con `ls -la` sobre esa ruta).

**`doctor` marca la base de datos o el almacenamito en rojo.**
Confirma que `storage.dataDir` existe y que el usuario `nexuscloud` es
dueño (`chown -R nexuscloud:nexuscloud <ruta>`).

**Olvidé la contraseña del administrador.**
No hay recuperación por email (NexusCloud no manda correos). Como
administrador del sistema, crea un usuario `super_admin` nuevo:
```sh
sudo -u nexuscloud nexuscloud --config /etc/nexuscloud/config.yaml admin create-user --username otro-admin --role super_admin
```

**Un usuario se quedó bloqueado por el doble factor (2FA/TOTP).**
```sh
nexuscloud --config config.yaml users totp disable --username <usuario>
```
Ver [`administracion.md`](administracion.md#doble-factor-totp).

Para dudas puntuales que no sean un fallo real, ver
[`preguntas-frecuentes.md`](preguntas-frecuentes.md).
