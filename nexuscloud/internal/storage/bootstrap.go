package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// EnsureDefaultPool garantiza que exista al menos un Storage Pool activo,
// creando uno llamado "default" en dataDir si la instalación está vacía.
// Se llama una vez al arrancar el servidor; el repositorio en sí no crea
// pools implícitamente (§10).
func EnsureDefaultPool(ctx context.Context, repo PoolRepository, dataDir string) (*Pool, error) {
	pool, err := repo.DefaultPool(ctx)
	if err == nil {
		return pool, nil
	}
	if !errors.Is(err, ErrPoolNotFound) {
		return nil, fmt.Errorf("comprobando storage pool por defecto: %w", err)
	}

	pool = &Pool{
		ID:        idgen.New(),
		Name:      "default",
		Type:      "local",
		Path:      dataDir,
		Priority:  0,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	if err := repo.CreatePool(ctx, pool); err != nil {
		return nil, fmt.Errorf("creando storage pool por defecto: %w", err)
	}
	return pool, nil
}
