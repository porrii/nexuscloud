package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// Encrypted y Salt (ADR-028): con Encrypted=false (cero-valor de Go,
	// así que un manifiesto de antes de este ADR se sigue leyendo como
	// "sin cifrar" sin ninguna migración), Salt se ignora. Con
	// Encrypted=true, Salt (aleatorio, generado una vez por job, en claro
	// -- no es secreto, solo la passphrase lo es) es el que hay que
	// combinar con NEXUSCLOUD_BACKUP_PASSPHRASE vía Argon2id para
	// reconstruir la misma clave AES-256 usada al respaldar.
	Encrypted bool   `json:"encrypted,omitempty"`
	Salt      string `json:"salt,omitempty"`
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
	SHA256     string `json:"sha256"` // siempre el hash del PLAINTEXT, cifrado o no
	// IV (ADR-028): vacío si el job no está cifrado. Con Manifest.Encrypted,
	// cada fichero tiene el suyo propio (aleatorio, 16 bytes) -- (clave, IV)
	// nunca debe repetirse en AES-CTR, y la clave es la misma para todo el
	// job (derivada una vez de Salt+passphrase).
	IV string `json:"iv,omitempty"`
}

// manifestRelPath (ADR-029): el manifiesto es, para Destination, un
// fichero más de jobID -- ningún tratamiento especial frente a cualquier
// otro relPath.
const manifestRelPath = "manifest.json"

func writeManifest(ctx context.Context, dest Destination, jobID string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("serializando manifest.json: %w", err)
	}
	if _, err := dest.WriteFile(ctx, jobID, manifestRelPath, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("escribiendo manifest.json: %w", err)
	}
	return nil
}

// ReadManifest (ADR-029) expone readManifest fuera del paquete -- lo usa
// internal/api/v1 para leer el manifiesto ya recibido de un backup remoto
// al procesar el paso "complete" (ver el ADR), reutilizando la misma
// lógica de deserialización que el resto del paquete en vez de
// duplicarla en la capa HTTP.
func ReadManifest(ctx context.Context, dest Destination, jobID string) (*Manifest, error) {
	return readManifest(ctx, dest, jobID)
}

func readManifest(ctx context.Context, dest Destination, jobID string) (*Manifest, error) {
	r, err := dest.OpenFile(ctx, jobID, manifestRelPath)
	if err != nil {
		return nil, fmt.Errorf("leyendo manifest.json: %w", err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("leyendo manifest.json: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parseando manifest.json: %w", err)
	}
	return &m, nil
}
