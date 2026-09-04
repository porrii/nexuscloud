package storage

import (
	"context"
	"errors"
	"time"
)

var ErrPoolNotFound = errors.New("storage: storage pool no encontrado")

// Pool representa un Storage Pool (§10). Fase 1 solo soporta pools locales
// de tipo "local"; RAID/discos/SMART (§11-12) llegan en Fase 5.
type Pool struct {
	ID        string
	Name      string
	Type      string // "local"
	Path      string
	Priority  int
	Status    string // "active" | "disabled"
	CreatedAt time.Time
}

type PoolRepository interface {
	CreatePool(ctx context.Context, p *Pool) error
	GetPoolByID(ctx context.Context, id string) (*Pool, error)
	ListPools(ctx context.Context) ([]*Pool, error)
	// DefaultPool devuelve el pool activo de mayor prioridad. No crea
	// ninguno: eso es responsabilidad de EnsureDefaultPool en el arranque
	// del servidor (bootstrap.go), para mantener el repositorio libre de
	// efectos secundarios implícitos.
	DefaultPool(ctx context.Context) (*Pool, error)
}
