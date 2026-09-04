# NexusCloud

Plataforma de almacenamiento en nube privada, autoalojada, modular y multiplataforma. MIT. Escrita en Go.

NexusCloud es el primer componente con servidor del ecosistema **Nexus** (self-hosted, offline-capable), pensado para funcionar tanto en una red local sin Internet como, más adelante, detrás de un dominio propio.

> **Estado del proyecto**: Fase 1 completada (ver [docs/architecture.md](docs/architecture.md#fases)). Hay un backend real, probado y funcional — API REST, autenticación, almacenamiento de archivos, seguridad de base — pero **todavía no hay interfaz web ni clientes de escritorio/móvil**. Consulta la sección "Qué funciona hoy" más abajo antes de asumir cualquier funcionalidad de las fases futuras.

## Qué funciona hoy (Fase 1)

- Usuarios, roles (RBAC), grupos, cuotas, invitaciones (sin registro público, §21)
- Autenticación: contraseña + Argon2id, sesiones revocables, TOTP (2FA)
- Almacenamiento de archivos: subida/descarga en streaming, protección activa contra path traversal e IDOR
- API REST versionada (`/api/v1`) — ver [docs/api.md](docs/api.md)
- Seguridad transversal: rate limiting, CORS estricto, cabeceras defensivas, auditoría
- Multi-base de datos: SQLite (por defecto) o PostgreSQL, sin cambiar código
- CLI (`nexuscloud`) con `start`, `doctor`, `migrate`, `admin`, `users`, `config`
- Docker, systemd, CI multiplataforma (Linux/Windows/ARM64)

**Explícitamente fuera de esta fase**: interfaz web, clientes Flutter (Windows/Linux/Android), compartición, papelera, versionado, sincronización, WebDAV, backups automáticos, snapshots, detección de discos/RAID, Passkeys/WebAuthn, sistema de plugins. Ver [docs/architecture.md](docs/architecture.md#fases) para el roadmap completo.

## Inicio rápido

### Con Docker (recomendado)

```bash
docker compose up -d
docker compose exec nexuscloud nexuscloud admin create-user --username tu-usuario
curl http://localhost:8080/health
```

### Compilando desde código fuente

Requiere [Go](https://go.dev) 1.25+.

```bash
go build -o nexuscloud ./cmd/nexuscloud
./nexuscloud config init          # genera config.yaml con valores seguros por defecto
./nexuscloud admin create-user --username tu-usuario   # nunca admin/admin (§140)
./nexuscloud doctor                # comprueba que todo esté en orden
./nexuscloud start
```

La interfaz web está desactivada por defecto (secure by default, §3/§47); esta fase solo expone la API REST. Consulta [docs/api.md](docs/api.md) para hacer login y subir tu primer archivo por API.

## Documentación

- [docs/architecture.md](docs/architecture.md) — módulos, árbol de proyecto, fases
- [docs/security.md](docs/security.md) — modelo de seguridad
- [docs/storage.md](docs/storage.md) — motor de almacenamiento
- [docs/deployment.md](docs/deployment.md) — Docker, systemd, Windows, reverse proxy
- [docs/api.md](docs/api.md) — referencia de la API REST
- [docs/security/threat-model.md](docs/security/threat-model.md) — modelo de amenazas
- [docs/architecture/decisions/](docs/architecture/decisions/) — decisiones de arquitectura (ADRs)

## Desarrollo

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .
```

Este repositorio no requiere Docker para desarrollar si tienes Go instalado localmente; se usó Docker durante el desarrollo inicial únicamente porque la máquina de referencia no tenía Go nativo.

## Licencia

MIT — ver [LICENSE](LICENSE).
