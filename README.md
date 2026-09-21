# NexusCloud

[![Licencia MIT](https://img.shields.io/github/license/porrii/nexuscloud)](LICENSE)
[![Última release](https://img.shields.io/github/v/release/porrii/nexuscloud)](https://github.com/porrii/nexuscloud/releases/latest)
[![CI](https://github.com/porrii/nexuscloud/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/porrii/nexuscloud/actions/workflows/ci.yml)

Plataforma de almacenamiento en la nube privada, autoalojada (self-hosted),
modular y multiplataforma. Escrita en Go, licencia MIT. Todo el código vive
en [`nexuscloud/`](nexuscloud/).

NexusCloud es el primer componente **con servidor** del ecosistema **Nexus**
(self-hosted, capaz de funcionar sin Internet): pensado para correr en tu
propia red local o detrás de tu propio dominio, sin depender de ningún
proveedor externo para guardar tus archivos.

## Qué hace hoy

- **Usuarios y acceso**: roles (RBAC), grupos, cuotas, invitaciones (sin
  registro público), contraseña + Argon2id, sesiones revocables, TOTP (2FA) y
  Passkeys/WebAuthn (segundo factor o login sin contraseña).
- **Archivos**: subida/descarga en streaming con descargas reanudables
  (`Range`), papelera con purga automática configurable (por antigüedad y
  por tamaño total), historial de versiones, protección activa contra path
  traversal e IDOR.
- **Compartición**: usuario→usuario, usuario→grupo, y enlaces públicos
  (contraseña, expiración, límite de descargas/tamaño, revocación).
- **Interfaz web** (React+TS+Vite, embebida en el propio binario): explorador
  con arrastrar-y-soltar, papelera, versiones, compartición, sesiones.
- **WebDAV** (opcional, desactivado por defecto): monta tu espacio como
  unidad de red o úsalo con rclone, Cyberduck, Finder o el Explorador de
  Windows, con tokens de acceso por dispositivo (nunca tu contraseña) y las
  mismas reglas de papelera, versiones y auditoría que la web. Ver
  [`nexuscloud/docs/webdav.md`](nexuscloud/docs/webdav.md).
- **Cliente de escritorio** (Windows/Linux, Flutter): paridad funcional con
  la web, sincronización bidireccional de varias carpetas a la vez,
  detección de mover/renombrar sin re-subir contenido, auto-sync
  configurable, bandeja del sistema, arranque con el sistema operativo.
- **Backup Manager**: manual o programado, completo o incremental
  (deduplicación por hardlinks), cifrado (AES-256-CTR), a una carpeta local
  o a otro servidor NexusCloud remoto (regla 3-2-1), con retención y
  restauración directa a un pool activo.
- **Detección de RAID y Snapshots del sistema operativo**: mdadm/Storage
  Spaces (RAID Linux/Windows), VSS/ZFS/Btrfs (Snapshots Windows/Linux) —
  solo lectura, NexusCloud no gestiona el RAID ni las snapshots por sí
  mismo.
- **Multi-base de datos**: SQLite (por defecto, cero configuración),
  PostgreSQL o MySQL/MariaDB, seleccionable sin tocar código.
- API REST versionada (`/api/v1`), CLI (`nexuscloud`) con `start`, `doctor`,
  `migrate`, `admin`, `users`, `config`, `backup`, `storage`, `service`.
- Seguridad transversal: rate limiting, CORS estricto, cabeceras
  defensivas, auditoría. Docker, systemd, servicio de Windows, CI
  multiplataforma (Linux amd64/arm64, Windows).

**Explícitamente fuera de alcance todavía**: cliente Android, subida
anónima, sistema de plugins, miniaturas/búsqueda de contenido. El detalle completo de qué fase cubre qué
está en [`nexuscloud/docs/architecture.md`](nexuscloud/docs/architecture.md#fases).

## Instalación

### Automática (recomendado)

Clona el repo y ejecuta el instalador de tu sistema. Ninguno de los dos
necesita Docker ni entorno gráfico: compilan el binario desde código
fuente (instalando Go si hace falta) y registran NexusCloud como servicio
del sistema (systemd en Linux, servicio de Windows).

```bash
git clone https://github.com/porrii/nexuscloud.git
cd nexuscloud
sudo ./install.sh          # Linux (añade --web si también quieres la interfaz web)
```

```bat
git clone https://github.com/porrii/nexuscloud.git
cd nexuscloud
install.bat                :: Windows -- desde una consola de Administrador (añade /web para la interfaz web)
```

Con una terminal/consola real por delante, el propio instalador te
pregunta lo poco que hace falta (¿interfaz web?, usuario y contraseña del
administrador) y **deja NexusCloud funcionando** al terminar: admin
creado, servicio arrancado, con la URL a abrir en el navegador impresa al
final -- no hace falta saber de systemd, `admin create-user` ni nada por
el estilo. Para el uso avanzado/scriptado (Docker, CI, aprovisionamiento
automático, sin preguntas) sigue disponible `--unattended`/`/unattended`
(deja el servicio instalado pero parado, como antes) o fijar
`NX_ADMIN_USERNAME`/`NX_ADMIN_PASSWORD` por variable de entorno para que
tampoco pregunte nada pero SÍ cree el admin y arranque.

La interfaz web es opcional; en el camino interactivo se pregunta con
"sí" por defecto (Enter la acepta) porque sin ella no hay forma de usar
NexusCloud sin la línea de comandos. Añadirla más adelante a una
instalación ya hecha, sin reinstalar desde cero:
`nexuscloud/deploy/scripts/enable-web.sh` (o `enable-web.ps1` en Windows).

Para desinstalar o actualizar más adelante, usa
`nexuscloud/deploy/scripts/uninstall.sh`/`update.sh` (o sus equivalentes
`.ps1` en Windows).

### Con Docker

```bash
cd nexuscloud
docker compose up -d
docker compose exec nexuscloud nexuscloud admin create-user --username tu-usuario
curl http://localhost:8080/health
```

### Compilando a mano

Requiere [Go](https://go.dev) 1.25+.

```bash
cd nexuscloud
go build -o nexuscloud ./cmd/nexuscloud
./nexuscloud config init          # genera config.yaml con valores seguros por defecto
./nexuscloud admin create-user --username tu-usuario   # nunca admin/admin
./nexuscloud doctor                # comprueba que todo esté en orden
./nexuscloud start
```

La interfaz web está desactivada por defecto (secure by default). Para
activarla hace falta además Node.js:

```bash
cd nexuscloud/web && npm install && npm run build && cd ..
go build -o nexuscloud ./cmd/nexuscloud   # ahora embebe web/dist
NEXUSCLOUD_WEB_ENABLED=true ./nexuscloud start
```

Sin interfaz web, la [referencia de la API](nexuscloud/docs/api.md) explica
cómo hacer login y subir tu primer archivo directamente.

### Cliente de escritorio (Windows/Linux)

En Windows, descarga `NexusCloud-win-Setup.exe` de la
[última release](https://github.com/porrii/nexuscloud/releases/latest) —
sin certificado de firma (puede avisar SmartScreen la primera vez, "Más
información" → "Ejecutar de todas formas"). Se actualiza solo, sin volver
a descargar nada a mano (ver [ADR-032](nexuscloud/docs/architecture/decisions/ADR-032-client-auto-update-velopack.md)).
En Linux, compila desde código: ver
[cliente-de-escritorio.md](docs/cliente-de-escritorio.md).

## Documentación

**Guías de uso** (todo bajo [`docs/`](docs/)):

- [primeros-pasos.md](docs/primeros-pasos.md) — de cero a tener NexusCloud funcionando
- [comandos.md](docs/comandos.md) — referencia de la CLI
- [administracion.md](docs/administracion.md) — usuarios, grupos, compartición, 2FA, auditoría
- [backup-y-recuperacion.md](docs/backup-y-recuperacion.md)
- [cliente-de-escritorio.md](docs/cliente-de-escritorio.md)
- [mantenimiento.md](docs/mantenimiento.md) — actualizar, verificar RAID/snapshots, resolver problemas
- [preguntas-frecuentes.md](docs/preguntas-frecuentes.md)

**Documentación técnica** (bajo [`nexuscloud/docs/`](nexuscloud/docs/)):

- [architecture.md](nexuscloud/docs/architecture.md) — módulos, árbol de proyecto, fases
- [security.md](nexuscloud/docs/security.md) — modelo de seguridad
- [storage.md](nexuscloud/docs/storage.md) — motor de almacenamiento, base de datos, backup, RAID, snapshots
- [deployment.md](nexuscloud/docs/deployment.md) — Docker, systemd, Windows, reverse proxy
- [api.md](nexuscloud/docs/api.md) — referencia de la API REST
- [architecture/decisions/](nexuscloud/docs/architecture/decisions/) — decisiones de arquitectura (ADRs)
- [`NEXUSCLOUD.md`](nexuscloud/NEXUSCLOUD.md) — especificación completa del proyecto

## Desarrollo

```bash
cd nexuscloud
go build ./...
go vet ./...
go test ./...
gofmt -l .
```

Si no tienes Go instalado localmente, `nexuscloud/scripts/dev.sh` envuelve
lo mismo dentro de un contenedor `golang` (`bash scripts/dev.sh all`).
También añade `test-mysql`/`test-mariadb`/`test-postgres` para correr los
tests contra un motor de base de datos real vía Docker.

## Contribuir

Ver [CONTRIBUTING.md](CONTRIBUTING.md) (PRs van contra `dev`, no contra
`main`) y el [código de conducta](CODE_OF_CONDUCT.md). Vulnerabilidades
de seguridad: [SECURITY.md](SECURITY.md), nunca un issue público.

## Licencia

MIT — ver [LICENSE](LICENSE).
