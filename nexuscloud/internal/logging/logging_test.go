package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
)

func TestNewWritesToFileWhenOutputIsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "nexuscloud.log")
	logger, closer, err := New(config.LoggingConfig{Level: "info", Format: "text", Output: path})
	if err != nil {
		t.Fatalf("New falló: %v", err)
	}
	defer closer.Close()

	logger.Info("evento de prueba")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("leyendo fichero de log: %v", err)
	}
	if !bytes.Contains(data, []byte("evento de prueba")) {
		t.Errorf("el fichero de log no contiene el mensaje esperado: %s", data)
	}
}

func TestParseLevelCoversAllSixLevels(t *testing.T) {
	cases := map[string]slog.Level{
		"trace": LevelTrace,
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"fatal": LevelFatal,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, esperado %v", in, got, want)
		}
	}
}
