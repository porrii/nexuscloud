package storage

import (
	"context"
	"errors"
	"testing"
)

// fakePoolRepo implementa PoolRepository lo justo para probar el resolver
// (que solo usa GetPoolByID).
type fakePoolRepo struct {
	pools map[string]*Pool
}

func (f *fakePoolRepo) GetPoolByID(_ context.Context, id string) (*Pool, error) {
	if p, ok := f.pools[id]; ok {
		return p, nil
	}
	return nil, ErrPoolNotFound
}
func (f *fakePoolRepo) CreatePool(context.Context, *Pool) error             { return nil }
func (f *fakePoolRepo) ListPools(context.Context) ([]*Pool, error)          { return nil, nil }
func (f *fakePoolRepo) DefaultPool(context.Context) (*Pool, error)          { return nil, ErrPoolNotFound }
func (f *fakePoolRepo) UpdatePool(context.Context, *Pool) error             { return nil }
func (f *fakePoolRepo) SetPoolStatus(context.Context, string, string) error { return nil }
func (f *fakePoolRepo) DeletePool(context.Context, string) error            { return nil }

func TestResolverCachesPerPool(t *testing.T) {
	repo := &fakePoolRepo{pools: map[string]*Pool{
		"p1": {ID: "p1", Type: "local", Path: t.TempDir()},
	}}
	var built int
	r := NewPoolProviderResolverWithFactory(repo, func(root string) (Provider, error) {
		built++
		return NewLocalFilesystemProvider(root)
	})

	a, err := r.For(context.Background(), "p1")
	if err != nil {
		t.Fatalf("For(p1) #1: %v", err)
	}
	b, err := r.For(context.Background(), "p1")
	if err != nil {
		t.Fatalf("For(p1) #2: %v", err)
	}
	if a != b {
		t.Error("el resolver debería devolver el MISMO Provider cacheado para el mismo pool")
	}
	if built != 1 {
		t.Errorf("factory llamada %d veces, esperado 1 (caché)", built)
	}
}

func TestResolverRejectsNonLocalPool(t *testing.T) {
	repo := &fakePoolRepo{pools: map[string]*Pool{
		"s3": {ID: "s3", Type: "s3", Path: "bucket/x"},
	}}
	r := NewPoolProviderResolver(repo)
	_, err := r.For(context.Background(), "s3")
	if !errors.Is(err, ErrUnsupportedPool) {
		t.Errorf("err = %v, esperado ErrUnsupportedPool para un pool de tipo no local", err)
	}
}

func TestResolverPropagatesMissingPool(t *testing.T) {
	r := NewPoolProviderResolver(&fakePoolRepo{pools: map[string]*Pool{}})
	_, err := r.For(context.Background(), "nope")
	if !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("err = %v, esperado ErrPoolNotFound", err)
	}
}
