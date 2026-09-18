# Contribuir a NexusCloud

Gracias por el interés. Antes de nada:

- **Bugs y sugerencias**: abre un [issue](https://github.com/porrii/nexuscloud/issues).
- **Vulnerabilidades de seguridad**: **no** abras un issue público — sigue
  [SECURITY.md](SECURITY.md).
- **Preguntas de uso** (no de desarrollo): revisa antes
  [`docs/preguntas-frecuentes.md`](docs/preguntas-frecuentes.md).

## Idioma

Comentarios de código, mensajes de commit y documentación van en
**español** (el código en sí — nombres de variables/funciones, términos
técnicos — se mantiene en inglés, como es habitual). Los PRs en otro
idioma también se aceptan, pero mantener el resto del repo en español es
la convención establecida.

## Pull requests

- Los PRs van contra **`dev`**, nunca contra `main` — `main` solo recibe
  merges de `dev` en el momento de sacar una release, no se desarrolla
  ahí directamente.
- Antes de abrir el PR, desde `nexuscloud/`:
  ```sh
  go build ./... && go vet ./... && go test ./... && gofmt -l .
  ```
  Sin Go instalado localmente, `nexuscloud/scripts/dev.sh all` hace lo
  mismo dentro de un contenedor.
- Cambios que afecten a una decisión de arquitectura ya tomada (o que
  tomen una nueva) van acompañados de una entrada en
  [`nexuscloud/docs/architecture/decisions/`](nexuscloud/docs/architecture/decisions/)
  (ADR) — mira las que ya existen para el formato.
- PRs pequeños y centrados en un solo cambio se revisan más rápido que
  uno grande con varias cosas mezcladas.

## Estructura del proyecto

Ver [`nexuscloud/docs/architecture.md`](nexuscloud/docs/architecture.md)
para el árbol completo y qué cubre cada fase. Resumen rápido:

- `nexuscloud/internal/` — lógica del servidor (Go)
- `nexuscloud/web/` — interfaz web (React+TS+Vite), embebida en el binario
- `nexuscloud/client/` — cliente de escritorio (Flutter, Windows/Linux)
- `docs/` — manuales de usuario; `nexuscloud/docs/` — documentación técnica
