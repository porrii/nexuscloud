// Package logging configura el logger estructurado de NexusCloud sobre
// log/slog (stdlib, cero dependencias extra). Nunca registrar secretos,
// contraseñas ni contenido de archivos de usuario (§32, §172).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/porrii/nexuscloud/internal/config"
)

// slog solo define Debug/Info/Warn/Error de forma nativa. §32 exige además
// TRACE y FATAL, así que se registran como niveles personalizados.
const (
	LevelTrace = slog.Level(-8)
	LevelFatal = slog.Level(12)
)

var levelNames = map[slog.Level]string{
	LevelTrace: "TRACE",
	LevelFatal: "FATAL",
}

// New construye el logger raíz a partir de la configuración de logging.
// Devuelve también un io.Closer: si Output apunta a un fichero, hay que
// cerrarlo al finalizar el proceso.
func New(cfg config.LoggingConfig) (*slog.Logger, io.Closer, error) {
	var out io.Writer = os.Stdout
	var closer io.Closer = nopCloser{}

	if cfg.Output != "" && cfg.Output != "stdout" && cfg.Output != "-" {
		if dir := filepath.Dir(cfg.Output); dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, nil, fmt.Errorf("creando directorio de logs %s: %w", dir, err)
			}
		}
		f, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
		if err != nil {
			return nil, nil, fmt.Errorf("abriendo fichero de log %s: %w", cfg.Output, err)
		}
		out = f
		closer = f
	}

	opts := &slog.HandlerOptions{
		Level: parseLevel(cfg.Level),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				if lvl, ok := a.Value.Any().(slog.Level); ok {
					if name, ok := levelNames[lvl]; ok {
						a.Value = slog.StringValue(name)
					}
				}
			}
			return a
		},
	}

	var handler slog.Handler
	if strings.EqualFold(cfg.Format, "json") {
		handler = slog.NewJSONHandler(out, opts)
	} else {
		handler = slog.NewTextHandler(out, opts)
	}
	return slog.New(handler), closer, nil
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "trace":
		return LevelTrace
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "fatal":
		return LevelFatal
	default:
		return slog.LevelInfo
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
