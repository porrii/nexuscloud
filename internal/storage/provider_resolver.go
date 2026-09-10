package storage

import (
	"context"
	"fmt"
	"sync"
)

// ProviderResolver devuelve el Provider físico de un Storage Pool a partir
// de su id. Es lo que usa FileService para no depender de un único
// Provider global: cada fichero sabe en qué pool vive (files.pool_id) y su
// I/O físico va al Provider de ese pool.
type ProviderResolver interface {
	For(ctx context.Context, poolID string) (Provider, error)
}

// ProviderFactory construye un Provider a partir de la ruta raíz de un
// pool. Por defecto NewLocalFilesystemProvider; inyectable en tests o para
// futuros tipos de pool.
type ProviderFactory func(root string) (Provider, error)

// poolProviderResolver resuelve providers locales por pool y los cachea.
// Seguro para uso concurrente.
type poolProviderResolver struct {
	pools   PoolRepository
	factory ProviderFactory

	mu    sync.Mutex
	cache map[string]Provider
}

// NewPoolProviderResolver crea un resolver que construye
// LocalFilesystemProvider por pool. Solo soporta pools de tipo "local".
func NewPoolProviderResolver(pools PoolRepository) ProviderResolver {
	return NewPoolProviderResolverWithFactory(pools, func(root string) (Provider, error) {
		return NewLocalFilesystemProvider(root)
	})
}

// NewPoolProviderResolverWithFactory permite inyectar la factoría (tests,
// tipos de pool no locales en el futuro).
func NewPoolProviderResolverWithFactory(pools PoolRepository, factory ProviderFactory) ProviderResolver {
	return &poolProviderResolver{
		pools:   pools,
		factory: factory,
		cache:   make(map[string]Provider),
	}
}

func (r *poolProviderResolver) For(ctx context.Context, poolID string) (Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, ok := r.cache[poolID]; ok {
		return p, nil
	}

	pool, err := r.pools.GetPoolByID(ctx, poolID)
	if err != nil {
		return nil, err
	}
	if pool.Type != "local" {
		return nil, fmt.Errorf("%w: %q (pool %s)", ErrUnsupportedPool, pool.Type, poolID)
	}

	p, err := r.factory(pool.Path)
	if err != nil {
		return nil, fmt.Errorf("preparando proveedor del pool %s: %w", poolID, err)
	}
	r.cache[poolID] = p
	return p, nil
}
