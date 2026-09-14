// Package version expone metadatos de build, sobreescritos en tiempo de
// compilación vía -ldflags (p.ej. desde el Dockerfile o CI).
package version

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// String devuelve una representación legible de una línea para --version y
// para el arranque del servidor.
func String() string {
	return "NexusCloud " + Version + " (commit " + GitCommit + ", build " + BuildDate + ")"
}
