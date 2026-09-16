# Despliegue de NexusCloud

NexusCloud **no depende de Docker** ni de ninguna tecnología de despliegue
concreta (requisitos 9 / 14 / 15 de la actualización de arquitectura). Toda
la lógica de la aplicación es independiente del método con el que se
arranque, se pare, se actualice o se configure. Elige el que mejor se
adapte:

| Método | Cuándo | Punto de entrada |
|--------|--------|------------------|
| **Nativo + systemd** (Linux) | servidor Linux dedicado, control total | `install.sh` (raíz del repo) |
| **Nativo + servicio** (Windows) | equipo Windows dedicado | `install.bat` (raíz del repo) |
| **Nativo en primer plano** | pruebas, u otro supervisor (runit, s6, NSSM…) | `nexuscloud start --config <ruta>` |
| **Docker** | ya usas Docker, quieres aislamiento | `Dockerfile` |
| **Docker Compose** | Docker + PostgreSQL / reverse-proxy juntos | `docker-compose.yml` |
| **OCI genérico** (Podman…) | runtime compatible con OCI | la misma imagen del `Dockerfile` |

Docker y Compose son métodos **de conveniencia**, no un requisito. Una
instalación nativa es un método plenamente soportado. Ninguna funcionalidad
de NexusCloud requiere Docker.

CI compila y verifica el binario para Linux amd64/arm64 (Raspberry Pi) y
Windows amd64 en cada push (`.github/workflows/ci.yml`).

---

## Configuración: la misma en todos los métodos

`almacenamiento`, `puertos`, `certificados`, `base de datos`, `caché`,
`logs`, `usuarios`, `permisos` se configuran igual sea cual sea el
despliegue:

1. `config.yaml` (ver `config.example.yaml`, o `nexuscloud config init`).
2. Variables de entorno `NEXUSCLOUD_*` (prioridad sobre el fichero).
3. Flags de CLI (máxima prioridad).

Cada área de almacenamiento (base de datos, caché, miniaturas, versionado,
temporales, logs, backups, config) puede vivir en un disco distinto sin
tocar código: `storage.databaseDir`, `storage.cacheDir`, … o
`NEXUSCLOUD_DATABASE_DIR`, `NEXUSCLOUD_CACHE_DIR`, … (ver
`docs/architecture/storage-evolution-plan.md`).

---

## Primer arranque (§140)

Igual con cualquier método:

1. `nexuscloud config init --out <ruta>` — genera `config.yaml` con valores
   seguros por defecto (sin web pública, sin registro público, rate
   limiting activo).
2. `nexuscloud --config <ruta> migrate up` — aplica el esquema.
3. `nexuscloud --config <ruta> admin create-user --username ...` — crea el
   primer usuario, que recibe el rol `super_admin`. Nunca se permite
   `admin`/`admin` ni una contraseña de menos de 8 caracteres.
4. `nexuscloud --config <ruta> doctor` — verifica configuración, base de
   datos, almacenamiento, puerto y TLS antes de exponer la instancia.
5. Arranca (`start`, `service start`, `systemctl start nexuscloud`, o
   `docker compose up -d` según el método).

---

## Nativo + systemd (Linux)

```sh
# Desde la raíz del repo clonado -- compila desde código fuente (instala Go
# si hace falta) y lo instala todo en un solo paso:
sudo ./install.sh
# -> crea el usuario de servicio, instala el binario en /usr/local/bin,
#    genera /etc/nexuscloud/config.yaml, aplica migraciones, hace 'enable'
#    del servicio. Con una terminal real por delante, ADEMAS pregunta
#    interfaz web/usuario/contraseña del admin, lo crea y arranca el
#    servicio ya mismo (imprime la URL al terminar) -- los comandos de
#    abajo (revisar config, crear admin, arrancar) pasan a ser opcionales,
#    solo hacen falta para repetir cualquiera de esos pasos a mano.
#
# Con la interfaz web (Node.js LTS se instala solo si hace falta, igual
# que Go): sudo ./install.sh --web  -- sin la bandera, en el camino
# interactivo se pregunta igualmente (con "sí" por defecto). Sin ella
# (ni por pregunta ni por bandera), el binario NO incluye la web y
# web.enabled queda en false (secure/lean by default). Añadirla más
# adelante a una instalación ya hecha, sin reinstalar desde cero:
# sudo nexuscloud/deploy/scripts/enable-web.sh
#
# Camino scriptado, sin preguntar nada (Docker/CI/aprovisionamiento):
#   sudo ./install.sh --unattended                      # como antes: no crea admin, no arranca
#   sudo NX_ADMIN_USERNAME=admin NX_ADMIN_PASSWORD=... ./install.sh   # crea admin y arranca sin preguntar
#
# Con un binario ya compilado (go build -o nexuscloud ./cmd/nexuscloud,
# cruzado a otra arquitectura, o descargado) en vez de compilar aquí:
#   sudo ./install.sh /ruta/al/binario
#   (--web no tiene efecto en este caso -- la web solo se puede embeber
#   compilando desde código; usa enable-web.sh sobre el resultado)

sudo -e /etc/nexuscloud/config.yaml
sudo -u nexuscloud /usr/local/bin/nexuscloud \
     --config /etc/nexuscloud/config.yaml admin create-user
sudo systemctl start nexuscloud
systemctl status nexuscloud
journalctl -u nexuscloud -f
```

- Actualizar: `sudo nexuscloud/deploy/scripts/update.sh ./nexuscloud-nuevo`
  (para el servicio, guarda copia del binario anterior, migra, y revierte
  el binario si la migración falla).
- Añadir la web a una instalación ya hecha sin ella:
  `sudo nexuscloud/deploy/scripts/enable-web.sh` (Node.js si falta, compila
  la web, recompila el binario, activa `web.enabled` y llama a `update.sh`
  por debajo). Quitarla de nuevo: recompila con `install.sh` normal (sin
  `--web`) y pasa ese binario a `update.sh` -- no hace falta un script
  aparte para eso, ya funciona con lo que existe.
- Desinstalar: `sudo nexuscloud/deploy/scripts/uninstall.sh`
  (conserva datos y config; `--purge` los borra también).
- Reubicar rutas: `NX_PREFIX`, `NX_CONFIG_DIR`, `NX_DATA_DIR`, `NX_USER`
  (mismas variables para `install.sh`, `update.sh` y `uninstall.sh`).

`deploy/systemd/nexuscloud.service` es la unit de referencia (hardening:
`ProtectSystem=strict`, `NoNewPrivileges`, `PrivateTmp`, mínimo privilegio
§103). Si reubicas `storage.dataDir` o alguna área a otro disco, añade esa
ruta a `ReadWritePaths=`.

---

## Nativo + servicio de Windows

```bat
:: Desde una consola de Administrador, en la raíz del repo clonado --
:: compila desde código fuente (instala Go si hace falta) y lo instala:
install.bat
:: -> copia a "Archivos de programa\NexusCloud", genera
::    ProgramData\NexusCloud\config.yaml, migra y registra el servicio.
::    Con una consola real por delante, ADEMAS pregunta usuario/
::    contraseña del admin, lo crea y arranca el servicio ya mismo
::    (imprime la URL al terminar) -- los comandos de abajo pasan a ser
::    opcionales, solo hacen falta para repetirlos a mano.
::
:: Con la interfaz web (Node.js LTS se instala solo si hace falta, igual
:: que Go): install.bat /web  -- sin la bandera, el propio
:: install-windows.ps1 pregunta igualmente (con "sí" por defecto). Sin
:: ella (ni por pregunta ni por bandera), el binario NO incluye la web y
:: web.enabled queda en false (secure/lean by default).
::
:: Camino scriptado, sin preguntar nada:
::   install.bat /unattended                              :: como antes
::   set NX_ADMIN_USERNAME=admin & set NX_ADMIN_PASSWORD=... & install.bat
```

```powershell
& "$env:ProgramFiles\NexusCloud\nexuscloud.exe" `
    --config "$env:ProgramData\NexusCloud\config.yaml" admin create-user
& "$env:ProgramFiles\NexusCloud\nexuscloud.exe" service start
```

El servicio se gestiona con `nexuscloud service {start|stop|restart|status}`
o desde `services.msc`.
Actualizar: `nexuscloud\deploy\scripts\update.ps1 C:\ruta\a\nexuscloud-nuevo.exe`
(equivalente Windows de `update.sh`: para el servicio si estaba corriendo,
guarda copia del binario anterior, migra, reinicia).
Añadir la web a una instalación ya hecha sin ella:
`nexuscloud\deploy\scripts\enable-web.ps1`. Desinstalar:
`nexuscloud\deploy\scripts\uninstall.ps1` (`-Purge` borra datos y config).

Por debajo, `nexuscloud service …` usa `github.com/kardianos/service`, que
habla con el SCM de Windows, systemd (Linux) o launchd (macOS) según el
sistema. `nexuscloud start` sigue siendo válido para correr en primer plano
bajo cualquier otro supervisor (NSSM, Tarea Programada, …).

En Linux, `install.sh` instala la unit `deploy/systemd/nexuscloud.service`
(con hardening: `ProtectSystem=strict`, `NoNewPrivileges`, `PrivateTmp`…),
que es preferible a la que genera `nexuscloud service install` (genérica de
`kardianos`, sin ese hardening). En Windows, `nexuscloud service install`
es el camino.

---

## Docker / Compose

```sh
docker compose up -d
docker compose exec nexuscloud nexuscloud admin create-user --username tu-usuario
```

La imagen (`Dockerfile`, multi-stage) hace el build en `golang:1.25-bookworm`
y el runtime en `gcr.io/distroless/static-debian12:nonroot` — sin shell, sin
gestor de paquetes, uid 65532, sin `privileged`, sin montar `/` del host
(§103-104). El binario es estático (`CGO_ENABLED=0`, driver SQLite en Go
puro), no depende de glibc/musl en runtime. Fija `NEXUSCLOUD_DATA_DIR=/data`
y expone `/data` como volumen; el resto de la config va por `NEXUSCLOUD_*` o
montando un `config.yaml`.

Para PostgreSQL en vez de SQLite: en `docker-compose.yml` cambia
`NEXUSCLOUD_DB_DRIVER` a `postgres`, añade `NEXUSCLOUD_DB_DSN` y descomenta
el servicio `postgres` (§8).

---

## Actualizaciones seguras (NEXUSCLOUD.md §59)

Sea cual sea el método: `nexuscloud migrate up` es no destructivo (los
`*.up.sql` no borran datos) y `migrate status` deja comprobar la versión de
esquema antes y después. `update.sh` automatiza el ciclo
parar→sustituir→migrar→arrancar con reversión del binario si la migración
falla. Haz un backup del `storage.dataDir` (o al menos de la base de datos)
antes de una actualización importante.
