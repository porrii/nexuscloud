# ADR-001: Backend en Go

## Estado

Aceptado.

## Contexto

NexusCloud necesita correr eficientemente en hardware muy heterogéneo: desde un PC doméstico hasta una Raspberry Pi, pasando por un servidor Linux o un contenedor Docker (§133, §160). Debe distribuirse como un binario fácil de instalar (§60), con concurrencia real para servir a varios usuarios subiendo/descargando a la vez, y con la menor huella de memoria posible en dispositivos limitados.

Alternativas consideradas:

- **Node.js/TypeScript**: ecosistema enorme, pero un runtime con mayor huella de memoria en reposo y un modelo de concurrencia (event loop) menos natural para I/O de archivos intensivo con backpressure real; el equipo también valoró la seguridad de tipos en tiempo de compilación como preferible a TypeScript para código que maneja archivos y permisos de usuarios.
- **Rust**: rendimiento y seguridad de memoria excelentes, pero una curva de aprendizaje y velocidad de iteración peor ajustadas a un proyecto que necesita crecer con contribuciones externas (MIT, distribuible) y a un ritmo de desarrollo alto en sus primeras fases.
- **C#/.NET**: descartado por consistencia — es el stack de NexusWorkspace (app de escritorio, no servidor), y no aporta ninguna ventaja específica para un servicio de red multiplataforma sobre Go.

## Decisión

Backend en **Go** (`go.mod` con `go 1.25`), siguiendo el mandato de la especificación (§5). Sin framework HTTP pesado: `net/http` de la librería estándar + `go-chi/chi/v5` (router idiomático, mínimas dependencias).

## Consecuencias

- Compilación cruzada trivial (`GOOS`/`GOARCH`) sin toolchain adicional — verificado en CI para Linux amd64/arm64 (Raspberry Pi) y Windows amd64.
- `CGO_ENABLED=0` es viable gracias a drivers de base de datos en Go puro (ver [ADR-003](ADR-003-database.md)), lo que simplifica enormemente la distribución de binarios estáticos.
- `log/slog`, `crypto/*`, `net/http` de la librería estándar cubren gran parte de las necesidades sin dependencias externas — coherente con "pocas dependencias, auditables" (§5).
- Contrapartida: menor disponibilidad de contribuidores con experiencia previa en Go frente a JS/Python, y un ecosistema de librerías más pequeño para casos muy específicos (p.ej. procesamiento de imágenes avanzado, relevante en fases futuras de miniaturas/previsualización).
