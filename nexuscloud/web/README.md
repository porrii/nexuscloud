# Interfaz Web

Explorador de archivos web de NexusCloud (§44, §83): React 18 + TypeScript + Vite + Tailwind CSS v4, servido por el propio binario Go vía `embed.FS` (ver `embed.go` en este mismo directorio y `internal/server/server.go`).

## Qué incluye

- Login (con soporte de código TOTP si el usuario tiene 2FA activado)
- Explorador de archivos: navegación por carpetas con breadcrumbs, subir (botón o arrastrar y soltar, con progreso real), descargar, crear carpeta, eliminar archivos y carpetas vacías
- Página de cuenta: perfil básico y gestión de sesiones activas (revocar sesiones remotamente, §26)

Todo consume exclusivamente la API documentada en [`../openapi.yaml`](../openapi.yaml) — no hay ningún acceso directo a base de datos ni al filesystem del servidor desde el frontend.

**Fuera de esta pasada** (porque el backend todavía no los soporta): búsqueda, previsualización de contenido, panel de administración de usuarios/invitaciones. Solo se construyó UI para lo que el backend ya ofrece de verdad — nada de pantallas que aparenten una función inexistente. (Compartición, papelera, versionado y favoritos/actividad reciente, mencionados aquí como pendientes en una versión anterior de este documento, ya están implementados — ver `docs/architecture.md` y los ADR correspondientes.)

## Autenticación en el navegador

El login guarda la sesión en una cookie `HttpOnly` que ya emite el backend (`nexuscloud_session`) — este frontend nunca lee ni guarda el token en `localStorage`: todas las peticiones usan `credentials: 'include'` y el navegador adjunta la cookie automáticamente. Recargar la página mantiene la sesión mientras la cookie siga siendo válida.

## Desarrollo

```bash
npm install
npm run dev
```

`vite.config.ts` reenvía `/api`, `/health` y `/ready` a `http://localhost:8080` (backend Go corriendo aparte, `nexuscloud start`), así el navegador ve todo como same-origin en desarrollo igual que en producción, cookies de sesión incluidas.

```bash
npm run build   # tsc -b && vite build -> web/dist/
npm run lint    # oxlint
```

## Activarla en el backend

`web.enabled` está desactivado por defecto (secure by default, §3/§47). Para servirla:

```bash
npm run build            # genera web/dist/
go build -o nexuscloud ./cmd/nexuscloud   # embebe web/dist en el binario
NEXUSCLOUD_WEB_ENABLED=true ./nexuscloud start
```

`web/dist/` no se versiona (se regenera con `npm run build`); solo se mantiene en git un `.gitkeep` para que `go:embed` tenga un directorio válido que referenciar incluso en un checkout limpio antes de la primera build del frontend.
