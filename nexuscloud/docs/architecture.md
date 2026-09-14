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
web/                  → interfaz web (React+TS+Vite); web/embed.go la embebe en el binario
```

### Regla de dependencia

`users` no importa `auth` ni `storage`; `auth` importa `users`; `storage` no importa ninguno de los dos; `api/v1` importa todo lo anterior; `server` es el único paquete que conoce todos los demás a la vez. Esto evita ciclos y hace que cada paquete sea testable de forma aislada (todos los paquetes de dominio tienen tests con una base de datos sqlite real en un directorio temporal, no mocks de la capa de persistencia).

### Por qué no exactamente el árbol del §7 de NEXUSCLOUD.md

- Los tests unitarios viven junto al código (`_test.go`, convención idiomática de Go) en vez de en un `/tests` global; `/tests/integration` se reserva para pruebas end-to-end que arrancan el servidor completo por HTTP real.
- Las migraciones SQL viven en `internal/db/migrations/` (no en `/migrations` en la raíz) porque `go:embed` no puede referenciar rutas fuera del árbol del paquete que las embebe.
- No existe una carpeta `/pkg`: todo el código es interno a este binario (`/internal`), coherente con que NexusCloud no expone (todavía) un SDK Go público.

## Modelo de datos

`users`, `roles`+`user_roles` (RBAC, semillas: `super_admin`/`administrator`/`user`/`read_only`), `groups`+`user_groups`, `sessions` (tokens opacos, solo se guarda su hash SHA-256), `invitations` (única vía de alta además del CLI — no hay registro público, §21), `storage_pools`, `files`/`directories` (solo metadatos; el contenido vive en el filesystem; `directories` desde la migración 0002, ver [ADR-002](architecture/decisions/ADR-002-storage.md); ambas con `deleted_at` desde la migración 0003 para la papelera, §16 — ver `docs/storage.md#papelera`), `file_versions` (historial de versiones, migración 0004, §15 — ver `docs/storage.md#versionado-15` y [ADR-007](architecture/decisions/ADR-007-versioning.md)), `shares` (comparticiones usuario/grupo/enlace, migración 0005, §37 — ver `docs/storage.md#compartición-37` y [ADR-008](architecture/decisions/ADR-008-sharing.md)), `audit_events`. Todas las columnas de timestamp se guardan como `TEXT` en RFC3339 UTC en ambos dialectos (ver [ADR-003](architecture/decisions/ADR-003-database.md)) para evitar diferencias de escaneo de tipos entre drivers.

## Superficie de API

Ver [docs/api.md](api.md) para la referencia completa. Resumen: `/health`, `/ready` en la raíz; todo lo demás bajo `/api/v1`, versionado (§42) para poder añadir sharing/trash/versionado/búsqueda más adelante sin romper compatibilidad.

## Fases

El desarrollo sigue el roadmap de 7 fases descrito en `NEXUSCLOUD.md` §163. **Las Fases 1, 2, 3 y 5 están completas**:

| Fase | Contenido | Estado |
|---|---|---|
| 1 | Core, Config, DB, Users, Auth, Storage básico, API, seguridad de base | ✅ Completa (con algunos gaps conocidos menores, ver más abajo) |
| 2 | Web UI (React+TS+Vite embebido) + File manager + Papelera + Versionado + Sharing | ✅ Completa — ver `web/README.md`, `docs/storage.md#papelera`, `docs/storage.md#versionado-15`, `docs/storage.md#compartición-37` |
| 3 | Cliente Desktop (Windows/Linux) | ✅ Completa (18 slices) — paridad funcional con la web, sync bidireccional con varios pares, auto-sync, descargas reanudables. Único pendiente: auto-actualización del MSIX, bloqueada en la obtención de un certificado de firma de código. Ver `client/README.md`, [ADR-005](architecture/decisions/ADR-005-multiplatform-strategy.md), [ADR-009](architecture/decisions/ADR-009-flutter-client-foundation.md) |
| 4 | Cliente Android | Pendiente — mismo código Flutter que Fase 3 |
| 5 | Backup Manager, Snapshots, gestión de discos/RAID | ✅ Completa (9 slices) — ver `docs/storage.md#backup-manager-18`, `docs/storage.md#raid-12`, `docs/storage.md#snapshots-17` |
| 6 | Seguridad avanzada: 2FA reforzado, Passkeys/WebAuthn, WebDAV | Pendiente — `security`/`storage`/`backup` ya reservados en el CLI |
| 7 | Integración real con el ecosistema Nexus (NexusWorkspace, etc.) | Pendiente |

### Explorador de archivos web: alcance real

El explorador (`web/`) cubre navegación por carpetas, subida (con progreso, arrastrar y soltar), descarga, creación y borrado de carpetas vacías, historial de versiones, compartición (usuario/grupo/enlace) y gestión de sesiones — todo respaldado por endpoints reales del backend. §143 (dashboard de usuario) menciona además "Favoritos" y "Recientes": **deliberadamente no se construyó ninguna pantalla para estas**, porque el backend todavía no las soporta — una UI para una funcionalidad inexistente sería peor que no tenerla. "Compartido conmigo"/"Compartido por mí"/"Papelera", también mencionadas en §143, sí están implementadas (`web/src/pages/SharedPage.tsx`, `TrashPage.tsx`).

### Gaps conocidos dentro de la propia Fase 1

- **Windows Service**: implementado -- `nexuscloud service install|uninstall|start|stop|restart|status` (`internal/cli/service_cmd.go`) envuelve el binario con `kardianos/service`, multiplataforma (Windows Service Control Manager / systemd / launchd) sin depender de Docker. Linux además conserva la unidad `systemd` de ejemplo (`deploy/systemd/nexuscloud.service`) como alternativa nativa sin pasar por el propio gestor de NexusCloud. **Housekeeping detectado (2026-09-13): esta entrada llevaba desactualizada, describiendo un gap ya cerrado, sin que ningún ADR ni entrada de `docs/storage.md#qué-falta` lo hubiera corregido -- verificar siempre contra el código, no solo contra la documentación existente, antes de dar un gap por bueno.**
- **Range requests / descargas reanudables**: implementado en Fase 3 slice 18 (§41) — `GET /api/v1/files/{id}` (y version/public-share) soportan `Range: bytes=N-` con `206 Partial Content`/`416`. Ver `docs/storage.md` y [ADR-010](architecture/decisions/ADR-010-file-transfer.md) y siguientes.
- **MySQL/MariaDB**: implementado ([ADR-031](architecture/decisions/ADR-031-mysql-mariadb.md)) -- tercer dialecto además de SQLite/PostgreSQL, con `go-sql-driver/mysql`. La abstracción de ADR-003 NO estaba tan desacoplada del dialecto como afirmaba: hicieron falta ramas de SQL específicas en `internal/storage` (UPSERT, concatenación de cadenas) además del driver y las migraciones.
