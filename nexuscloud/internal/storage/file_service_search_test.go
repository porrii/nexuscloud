package storage

import (
	"context"
	"testing"
)

// Los filtros en sí (nombre/tipo/extensión/fecha/tamaño) ya están probados a
// fondo en search_test.go contra el repositorio; estos tests solo cubren
// que FileService.Search/SearchAsAdmin combinan archivos+carpetas y
// delegan la autorización correctamente.

func TestFileServiceSearchCombinesFilesAndDirectories(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	uploadFile(t, env, owner, "informe-2026.pdf")
	mkdir(t, env, owner, "Informes")

	result, err := env.svc.Search(ctx, owner, SearchFilters{Query: "informe"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Files) != 1 || len(result.Directories) != 1 {
		t.Errorf("Search debería combinar archivos y carpetas: %+v", result)
	}
}

func TestFileServiceSearchIsolatedByOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	uploadFile(t, env, ana, "documento.txt")
	uploadFile(t, env, bea, "documento.txt")

	result, err := env.svc.Search(ctx, ana, SearchFilters{Query: "documento"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].OwnerID != ana {
		t.Errorf("Search de ana vio archivos de otro propietario: %+v", result.Files)
	}
}

func TestFileServiceSearchAsAdminSeesAllWithoutOwnerFilter(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	uploadFile(t, env, ana, "de-ana.txt")
	uploadFile(t, env, bea, "de-bea.txt")

	result, err := env.svc.SearchAsAdmin(ctx, SearchFilters{}, "")
	if err != nil {
		t.Fatalf("SearchAsAdmin: %v", err)
	}
	if len(result.Files) != 2 {
		t.Errorf("SearchAsAdmin(sin owner) = %d archivos, esperados 2: %+v", len(result.Files), result.Files)
	}
}

func TestFileServiceSearchAsAdminScopesToOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	uploadFile(t, env, ana, "de-ana.txt")
	uploadFile(t, env, bea, "de-bea.txt")

	result, err := env.svc.SearchAsAdmin(ctx, SearchFilters{}, ana)
	if err != nil {
		t.Fatalf("SearchAsAdmin: %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].OwnerID != ana {
		t.Errorf("SearchAsAdmin(owner=ana) = %+v, esperado solo lo de ana", result.Files)
	}
}
