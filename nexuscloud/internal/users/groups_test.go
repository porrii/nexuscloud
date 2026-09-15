package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// GetGroupByID/GetGroupByName completan el mismo par que ya existía para
// User (GetUserByID/GetUserByUsername) -- añadidos para que el handler HTTP
// de "añadir miembro" (POST /groups/{id}/members) pueda devolver 404 real.
func TestGetGroupByIDAndName(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)

	g := &Group{ID: idgen.New(), Name: "Contabilidad", CreatedAt: time.Now().UTC()}
	if err := repo.CreateGroup(ctx, g); err != nil {
		t.Fatalf("CreateGroup falló: %v", err)
	}

	byID, err := repo.GetGroupByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("GetGroupByID falló: %v", err)
	}
	if byID.Name != "Contabilidad" {
		t.Errorf("Name = %q, esperado Contabilidad", byID.Name)
	}

	byName, err := repo.GetGroupByName(ctx, "Contabilidad")
	if err != nil {
		t.Fatalf("GetGroupByName falló: %v", err)
	}
	if byName.ID != g.ID {
		t.Errorf("ID = %q, esperado %q", byName.ID, g.ID)
	}
}

func TestGetGroupByIDNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)

	if _, err := repo.GetGroupByID(ctx, "no-existe"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, esperado ErrNotFound", err)
	}
}

func TestGetGroupByNameNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)

	if _, err := repo.GetGroupByName(ctx, "no-existe"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, esperado ErrNotFound", err)
	}
}
