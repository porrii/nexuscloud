# AGENTS.md — guía rápida para agentes de código

NexusCloud: nube privada autoalojada. Servidor Go (API + CLI en un solo binario),
web React embebida, cliente de escritorio Flutter. Prioridad: seguridad >
fiabilidad > integridad de datos > el resto.

## Mapa (lo que no se ve a simple vista)
- **`go.mod` NO está en la raíz**: todo el código vive en `nexuscloud/`. Lanza Go,
  npm y Flutter desde ahí (`nexuscloud/`, `nexuscloud/web`, `nexuscloud/client`).
- `nexuscloud/cmd/nexuscloud` → `internal/cli` → `internal/server` (único que
  conoce todo). `internal/api/v1` = handlers. `users` no importa `auth` ni
  `storage`; `storage` no importa ninguno de los dos.
- `internal/storage.FileService` es el ÚNICO acceso a archivos de usuario.
- Migraciones: `internal/db/migrations/<motor>/` (sqlite, postgres, mysql): un
  cambio de esquema va en los tres motores.
- Web: `nexuscloud/web` (React + TS + Vite + Tailwind v4), embebida con
  `//go:embed all:dist`. Cliente: `nexuscloud/client` (`lib/features/<f>/{domain,data,di,presentation}`,
  `get_it` + `Stream` + `setState`, **sin riverpod**).
- Docs: `docs/` (raíz) = manuales de usuario; `nexuscloud/docs/` = técnica;
  decisiones en `nexuscloud/docs/architecture/decisions/ADR-NNN-*.md`.
- **Spec**: `nexuscloud/NEXUSCLOUD.md` (53 KB, secciones «§N»). No lo leas entero:
  busca la sección (`grep -n '^# 24\.' nexuscloud/NEXUSCLOUD.md`) y lee solo ese tramo.

## No leer / no tocar
- Generado o ruido: `web/node_modules`, `web/dist` (solo `.gitkeep` se versiona),
  `client/build`, `client/.dart_tool`, `client/Releases*`, `client/**/generated_plugin*`,
  `go.sum`, `web/package-lock.json`.
- No cambies `CompanyName`/`ProductName` de `client/windows/runner/Runner.rc` ni
  `SetQuitOnClose` de `client/windows/runner/main.cpp` (mueven datos / dejan procesos zombi).

## Validar un cambio (lo mismo que el CI)
```bash
cd nexuscloud
go build ./... && go vet ./... && go test ./... && gofmt -l .   # gofmt -l debe salir vacío
# Sin Go local: bash scripts/dev.sh build|vet|test|all  (Docker, golang:1.25-bookworm)
# SQL/migraciones contra motores reales: bash scripts/dev.sh test-mysql|test-mariadb|test-postgres
cd web && npm run lint && npm run build     # luego: git checkout -- dist/.gitkeep (vite lo borra)
cd client && flutter analyze && flutter test  # analyze limpio no garantiza que compile
```
- `dev.sh fmt` reescribe todo el árbol con `gofmt -w`: úsalo solo si lo pretendes.
- Tests con SQLite y filesystem reales, sin mocks de persistencia. Los de MySQL/PG
  se saltan (y salen «ok») si no hay `NEXUSCLOUD_TEST_<DRIVER>_DSN`.

## Reglas del proyecto
- Idioma: comentarios, commits y docs en **español**; identificadores en inglés.
- Commits: frase en español en imperativo + sección del spec/ADR entre paréntesis,
  p. ej. «Añade búsqueda de metadatos por nombre, tipo, fecha y tamaño (§33, ADR-040)».
  Nada de `feat:`/`fix:`. Mira `git log` antes de escribir uno.
- Ramas: se trabaja en `dev`; `main` solo recibe releases. Los PR van contra `dev`.
- Esquema o lógica central nuevos → ADR (`## Estado / Contexto / Decisión / Consecuencias`)
  antes del código; si contradice uno anterior, enlázalo.
- **Secure by default**: toda superficie nueva sin sesión o costosa va desactivada
  por defecto y, desactivada, sus rutas ni se registran (404, no 401).
- Secretos solo por variables de entorno (`NEXUSCLOUD_*`), nunca en `config.yaml`
  ni en argumentos de CLI. Errores al cliente genéricos; el detalle, en `slog`.
- Cambios quirúrgicos; mejor una pequeña duplicación que una abstracción
  prematura; archivos por debajo de ~800 líneas; fallar cerrado.
