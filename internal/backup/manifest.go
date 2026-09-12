package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Manifest describe exactamente qué se copió en un backup. Vive como
// manifest.json autocontenido dentro de la propia carpeta del job (nunca en
// la base de datos, ver job.go) -- un backup debe poder inspeccionarse y
// restaurarse aunque la base de datos de origen ya no esté disponible, que
// es precisamente el escenario que un Backup Manager existe para cubrir.
// Se escribe una única vez, al terminar con éxito: nunca queda a medias.
type Manifest struct {
	JobID     string         `json:"job_id"`
	CreatedAt time.Time      `json:"created_at"`
	Pools     []PoolManifest `json:"pools"`
}

type PoolManifest struct {
	PoolID   string         `json:"pool_id"`
	PoolName string         `json:"pool_name"`
	Files    []FileManifest `json:"files"`
}

type FileManifest struct {
	OwnerID    string `json:"owner_id"`
	ParentPath string `json:"parent_path"`
	Name       string `json:"name"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
}

func manifestPath(jobDir string) string {
	return filepath.Join(jobDir, "manifest.json")
}

func writeManifest(jobDir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("serializando manifest.json: %w", err)
	}
	if err := os.WriteFile(manifestPath(jobDir), data, 0o600); err != nil {
		return fmt.Errorf("escribiendo manifest.json: %w", err)
	}
	return nil
}

func readManifest(jobDir string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath(jobDir))
	if err != nil {
		return nil, fmt.Errorf("leyendo manifest.json: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parseando manifest.json: %w", err)
	}
	return &m, nil
}
