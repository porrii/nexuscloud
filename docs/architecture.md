# Arquitectura

## Visión general

NexusCloud es un binario único (`nexuscloud`) que combina servidor y CLI de administración. La arquitectura es modular por capas dentro de un mismo módulo Go — no microservicios: la complejidad operativa de microservicios no está justificada a la escala objetivo (~100 usuarios, §160) y contradice el principio de simplicidad (§162).

```
cmd/nexuscloud        → entrypoint (cobra root)
internal/cli          → subcomandos del CLI (§98)
internal/server       → wiring: config → DB → repositorios → servicios → router HTTP
internal/api/v1       → handlers REST, middleware de auth/authz, DTOs
internal/config       → carga/validación/schema de configuración (§55, §151)
internal/logging      → logging estructurado (slog)
internal/db           → conexión + migraciones multi-motor
internal/users        → usuarios, roles, grupos (sin saber nada de contraseñas)
internal/auth         → hashing, sesiones, TOTP, invitaciones (depende de users)
internal/storage      → FileService, StorageProvider, Storage Pools
internal/security     → rate limiting, CORS, cabeceras, IP de cliente
internal/audit        → registro de eventos de auditoría
internal/idgen        → IDs y tokens aleatorios (UUID v4, tokens opacos)
internal/version      → metadatos de build
```

### Regla de dependencia

`users` no importa `auth` ni `storage`; `auth` importa `users`; `storage` no importa ninguno de los dos; `api/v1` importa todo lo anterior; `server` es el único paquete que conoce todos los demás a la vez. Esto evita ciclos y hace que cada paquete sea testable de forma aislada (todos los paquetes de dominio tienen tests con una base de datos sqlite real en un directorio temporal, no mocks de la capa de persistencia).

### Por qué no exactamente el árbol del §7 de NEXUSCLOUD.md

- Los tests unitarios viven junto al código (`_test.go`, convención idiomática de Go) en vez de en un `/tests` global; `/tests/integration` se reserva para pruebas end-to-end que arrancan el servidor completo por HTTP real.
- Las migraciones SQL viven en `internal/db/migrations/` (no en `/migrations` en la raíz) porque `go:embed` no puede referenciar rutas fuera del árbol del paquete que las embebe.
- No existe una carpeta `/pkg`: todo el código es interno a este binario (`/internal`), coherente con que NexusCloud no expone (todavía) un SDK Go público.

## Modelo de datos (Fase 1)

`users`, `roles`+`user_roles` (RBAC, semillas: `super_admin`/`administrator`/`user`/`read_only`), `groups`+`user_groups`, `sessions` (tokens opacos, solo se guarda su hash SHA-256), `invitations` (única vía de alta además del CLI — no hay registro público, §21), `storage_pools`, `files` (solo metadatos; el contenido vive en el filesystem), `audit_events`. Todas las columnas de timestamp se guardan como `TEXT` en RFC3339 UTC en ambos dialectos (ver [ADR-003](architecture/decisions/ADR-003-database.md)) para evitar diferencias de escaneo de tipos entre drivers.

## Superficie de API

Ver [docs/api.md](api.md) para la referencia completa. Resumen: `/health`, `/ready` en la raíz; todo lo demás bajo `/api/v1`, versionado (§42) para poder añadir sharing/trash/versionado/búsqueda más adelante sin romper compatibilidad.

## Fases

El desarrollo sigue el roadmap de 7 fases descrito en `NEXUSCLOUD.md` §163. **Este repositorio implementa la Fase 1 por completo** y dejó decididas (pero no implementadas) varias piezas de fases posteriores para no tener que rediseñar:

| Fase | Contenido | Estado |
|---|---|---|
| 1 | Core, Config, DB, Users, Auth, Storage básico, API, seguridad de base | ✅ Completa |
| 2 | Web UI (React+TS+Vite embebido), File manager, Sharing, Trash, Versionado | Pendiente — stack ya decidido |
| 3 | Cliente Desktop (Windows/Linux) | Pendiente — Flutter, Clean Architecture (ver ADR-005) |
| 4 | Cliente Android | Pendiente — mismo código Flutter que Fase 3 |
| 5 | Backup Manager, Snapshots, gestión de discos/RAID | Pendiente |
| 6 | Seguridad avanzada: 2FA reforzado, Passkeys/WebAuthn, WebDAV | Pendiente — `security`/`storage`/`backup` ya reservados en el CLI |
| 7 | Integración real con el ecosistema Nexus (NexusWorkspace, etc.) | Pendiente |

### Gaps conocidos dentro de la propia Fase 1

- **Windows Service**: el plan inicial preveía envolver el binario con `kardianos/service` para que `nexuscloud start` pudiera instalarse como servicio nativo de Windows. No se implementó en esta pasada — hoy el binario corre en primer plano tanto en Windows como en Linux (con apagado ordenado por señal). Linux sí tiene una unidad `systemd` real y funcional (`deploy/systemd/nexuscloud.service`). Instalar como servicio de Windows hoy requiere un envoltorio externo (NSSM, Tarea Programada) hasta que se añada soporte nativo.
- **Range requests / descargas reanudables**: `GET /api/v1/files/{id}` transmite el contenido completo en streaming, pero no soporta cabecera `Range` todavía (§41 queda para una fase posterior sin romper el contrato de la API).
- **MySQL/MariaDB**: la capa de abstracción de base de datos está lista para añadirlo (interfaces ya desacopladas del dialecto), pero no se implementó el driver/migraciones en esta pasada — solo SQLite y PostgreSQL, que son el mínimo exigido por §8.
