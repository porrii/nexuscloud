# Despliegue

## Docker (recomendado)

```bash
docker compose up -d
docker compose exec nexuscloud nexuscloud admin create-user --username tu-usuario
```

La imagen (`Dockerfile`) es multi-stage: build en `golang:1.25-bookworm`, runtime en `gcr.io/distroless/static-debian12:nonroot` — sin shell, sin gestor de paquetes, corre como usuario no-root (uid 65532), sin `privileged` y sin montar `/` del host (§103-104). El binario es estático (`CGO_ENABLED=0`, driver SQLite en Go puro) así que no depende de glibc/musl en runtime.

Para usar PostgreSQL en vez de SQLite, edita `docker-compose.yml`: cambia `NEXUSCLOUD_DB_DRIVER` a `postgres`, añade `NEXUSCLOUD_DB_DSN`, y descomenta el servicio `postgres` (§8).

## Binario nativo

```bash
go build -o nexuscloud ./cmd/nexuscloud   # o descarga un release cuando existan
./nexuscloud config init
./nexuscloud admin create-user --username tu-usuario
./nexuscloud doctor
./nexuscloud start
```

CI compila y verifica el binario para Linux amd64/arm64 (Raspberry Pi) y Windows amd64 en cada push (`.github/workflows/ci.yml`).

### Linux — systemd

```bash
sudo cp deploy/systemd/nexuscloud.service /etc/systemd/system/
sudo useradd --system --home /var/lib/nexuscloud nexuscloud
sudo mkdir -p /var/lib/nexuscloud /etc/nexuscloud
sudo cp config.example.yaml /etc/nexuscloud/config.yaml   # y edítalo
sudo chown -R nexuscloud:nexuscloud /var/lib/nexuscloud
sudo systemctl daemon-reload
sudo systemctl enable --now nexuscloud
```

La unidad (`deploy/systemd/nexuscloud.service`) aplica hardening estándar (`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`) y mínimo privilegio (§103) — ajusta `ReadWritePaths` si cambias `storage.dataDir`.

### Windows

Hoy el binario corre en primer plano (`nexuscloud.exe start`), con apagado ordenado ante Ctrl+C. **No hay todavía integración nativa como servicio de Windows** (se evaluó `kardianos/service` en el diseño pero no se implementó en esta fase — ver `docs/architecture.md#gaps-conocidos-dentro-de-la-propia-fase-1`). Para correrlo como servicio hoy, usa un envoltorio externo como [NSSM](https://nssm.cc/) o una Tarea Programada configurada para reiniciar ante fallo.

## Primer arranque (§140)

1. `nexuscloud config init` — genera `config.yaml` con valores seguros por defecto.
2. `nexuscloud admin create-user --username ...` — crea el primer usuario, que recibe automáticamente el rol `super_admin`. Nunca se permite `admin`/`admin` ni una contraseña de menos de 8 caracteres.
3. `nexuscloud doctor` — verifica configuración, base de datos, almacenamiento, puerto y TLS antes de exponer la instancia.
4. `nexuscloud start`.

## Exponer NexusCloud a Internet (§67)

**No** abras el puerto de NexusCloud directamente a Internet. El patrón recomendado:

```
Internet → Firewall → Reverse Proxy (TLS aquí) → NexusCloud (HTTP en LAN/loopback)
```

- El reverse proxy (Nginx/Caddy/Traefik) termina TLS y reenvía a NexusCloud.
- Configura `server.trustedProxies` con la IP/CIDR del proxy — NexusCloud no confía en `X-Forwarded-For`/`X-Forwarded-Proto` de ningún origen no listado (§50, §118).
- Alternativa: rellenar `server.tlsCertFile`/`tlsKeyFile` para que NexusCloud sirva HTTPS directamente, sin proxy delante — razonable para LAN, no recomendado como única capa en Internet.

## Variables de entorno

Toda opción de `config.yaml` tiene su equivalente `NEXUSCLOUD_*` (mayor prioridad que el fichero; ver `internal/config/env.go` para la lista completa) — útil para Docker/systemd sin tocar el fichero de configuración.
