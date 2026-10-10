package storage

import (
	"context"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// Búsqueda (§33): los tests construyen FileMeta/Directory directamente
// contra el repositorio (mismo patrón que mysql_integration_test.go) en vez
// de pasar por FileService.Upload/Mkdir, porque necesitan control exacto
// sobre CreatedAt/SizeBytes/MimeType para probar cada filtro por separado.

func registerFile(t *testing.T, env *testEnv, ownerID, name, mimeType string, sizeBytes int64, createdAt time.Time) *FileMeta {
	t.Helper()
	ctx := context.Background()
	m := &FileMeta{
		ID: idgen.New(), PoolID: mustDefaultPoolID(ctx, t, env), OwnerID: ownerID, ParentPath: "/", Name: name,
		SizeBytes: sizeBytes, SHA256: "h-" + name, MimeType: mimeType, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := env.files.UpsertFile(ctx, m); err != nil {
		t.Fatalf("registrando archivo %s: %v", name, err)
	}
	return m
}

func registerDirectory(t *testing.T, env *testEnv, ownerID, name string, createdAt time.Time) *Directory {
	t.Helper()
	ctx := context.Background()
	d := &Directory{ID: idgen.New(), PoolID: mustDefaultPoolID(ctx, t, env), OwnerID: ownerID, ParentPath: "/", Name: name, CreatedAt: createdAt}
	if err := env.directories.CreateDirectory(ctx, d); err != nil {
		t.Fatalf("registrando carpeta %s: %v", name, err)
	}
	return d
}

func TestSearchFilesByNameSubstringCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	now := time.Now().UTC()
	registerFile(t, env, owner, "Foto de la Boda.JPG", "image/jpeg", 100, now)
	registerFile(t, env, owner, "informe.pdf", "application/pdf", 100, now)

	got, err := env.files.SearchFiles(ctx, owner, SearchFilters{Query: "boda"})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Foto de la Boda.JPG" {
		t.Errorf("SearchFiles(Query=boda) = %+v, esperado solo la foto (case-insensitive)", got)
	}
}

func TestSearchFilesByNameOrPath(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	now := time.Now().UTC()
	registerFile(t, env, owner, "cualquiera.txt", "text/plain", 10, now)
	m := &FileMeta{
		ID: idgen.New(), PoolID: mustDefaultPoolID(ctx, t, env), OwnerID: owner, ParentPath: "/Contratos/2026", Name: "otro.txt",
		SizeBytes: 10, SHA256: "h", MimeType: "text/plain", CreatedAt: now, UpdatedAt: now,
	}
	if err := env.files.UpsertFile(ctx, m); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	got, err := env.files.SearchFiles(ctx, owner, SearchFilters{Query: "contratos"})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 1 || got[0].Name != "otro.txt" {
		t.Errorf("SearchFiles(Query=contratos) debería encontrar por ruta = %+v", got)
	}
}

func TestSearchFilesByMimeTypePrefix(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	now := time.Now().UTC()
	registerFile(t, env, owner, "a.jpg", "image/jpeg", 100, now)
	registerFile(t, env, owner, "b.png", "image/png", 100, now)
	registerFile(t, env, owner, "c.pdf", "application/pdf", 100, now)

	got, err := env.files.SearchFiles(ctx, owner, SearchFilters{MimeType: "image/"})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("SearchFiles(MimeType=image/) = %d resultados, esperados 2: %+v", len(got), got)
	}
}

func TestSearchFilesByExtensionCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	now := time.Now().UTC()
	registerFile(t, env, owner, "a.pdf", "application/pdf", 100, now)
	registerFile(t, env, owner, "B.PDF", "application/pdf", 100, now)
	registerFile(t, env, owner, "c.txt", "text/plain", 100, now)

	got, err := env.files.SearchFiles(ctx, owner, SearchFilters{Ext: "pdf"})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("SearchFiles(Ext=pdf) = %d resultados, esperados 2 (case-insensitive): %+v", len(got), got)
	}
}

func TestSearchFilesByDateRange(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	registerFile(t, env, owner, "viejo.txt", "text/plain", 10, base)
	registerFile(t, env, owner, "medio.txt", "text/plain", 10, base.AddDate(0, 1, 0))
	registerFile(t, env, owner, "nuevo.txt", "text/plain", 10, base.AddDate(0, 2, 0))

	from := base.AddDate(0, 0, 15)
	to := base.AddDate(0, 1, 15)
	got, err := env.files.SearchFiles(ctx, owner, SearchFilters{DateFrom: &from, DateTo: &to})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 1 || got[0].Name != "medio.txt" {
		t.Errorf("SearchFiles(rango de fechas) = %+v, esperado solo medio.txt", got)
	}
}

func TestSearchFilesBySizeRange(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	now := time.Now().UTC()
	registerFile(t, env, owner, "pequeno.bin", "application/octet-stream", 100, now)
	registerFile(t, env, owner, "mediano.bin", "application/octet-stream", 1000, now)
	registerFile(t, env, owner, "grande.bin", "application/octet-stream", 10000, now)

	min, max := int64(500), int64(5000)
	got, err := env.files.SearchFiles(ctx, owner, SearchFilters{SizeMin: &min, SizeMax: &max})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 1 || got[0].Name != "mediano.bin" {
		t.Errorf("SearchFiles(rango de tamaño) = %+v, esperado solo mediano.bin", got)
	}
}

func TestSearchFilesCombinesFiltersWithAnd(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	now := time.Now().UTC()
	registerFile(t, env, owner, "informe-grande.pdf", "application/pdf", 10000, now)
	registerFile(t, env, owner, "informe-pequeno.pdf", "application/pdf", 10, now)
	registerFile(t, env, owner, "foto-grande.jpg", "image/jpeg", 10000, now)

	min := int64(1000)
	got, err := env.files.SearchFiles(ctx, owner, SearchFilters{Query: "informe", SizeMin: &min})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 1 || got[0].Name != "informe-grande.pdf" {
		t.Errorf("SearchFiles(Query+SizeMin combinados) = %+v, esperado solo informe-grande.pdf", got)
	}
}

func TestSearchFilesIsolatedByOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	now := time.Now().UTC()
	registerFile(t, env, ana, "documento.txt", "text/plain", 10, now)
	registerFile(t, env, bea, "documento.txt", "text/plain", 10, now)

	got, err := env.files.SearchFiles(ctx, ana, SearchFilters{Query: "documento"})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 1 || got[0].OwnerID != ana {
		t.Errorf("SearchFiles de ana vio archivos de otro propietario: %+v", got)
	}
}

func TestSearchFilesExcludesTrashed(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	now := time.Now().UTC()
	f := registerFile(t, env, owner, "borrado.txt", "text/plain", 10, now)
	if err := env.files.SoftDeleteFile(ctx, f.ID, now); err != nil {
		t.Fatalf("SoftDeleteFile: %v", err)
	}
	registerFile(t, env, owner, "activo.txt", "text/plain", 10, now)

	got, err := env.files.SearchFiles(ctx, owner, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(got) != 1 || got[0].Name != "activo.txt" {
		t.Errorf("SearchFiles sin filtro = %+v, esperado solo el activo (nunca lo trasheado)", got)
	}
}

func TestSearchFilesAllOwnersWithoutFilterSeesEveryone(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	now := time.Now().UTC()
	registerFile(t, env, ana, "de-ana.txt", "text/plain", 10, now)
	registerFile(t, env, bea, "de-bea.txt", "text/plain", 10, now)

	got, err := env.files.SearchFilesAllOwners(ctx, SearchFilters{}, "")
	if err != nil {
		t.Fatalf("SearchFilesAllOwners: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("SearchFilesAllOwners(sin owner) = %d resultados, esperados 2 (todos los propietarios): %+v", len(got), got)
	}
}

func TestSearchFilesAllOwnersWithOwnerScopesToOne(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	now := time.Now().UTC()
	registerFile(t, env, ana, "de-ana.txt", "text/plain", 10, now)
	registerFile(t, env, bea, "de-bea.txt", "text/plain", 10, now)

	got, err := env.files.SearchFilesAllOwners(ctx, SearchFilters{}, ana)
	if err != nil {
		t.Fatalf("SearchFilesAllOwners: %v", err)
	}
	if len(got) != 1 || got[0].OwnerID != ana {
		t.Errorf("SearchFilesAllOwners(owner=ana) = %+v, esperado solo lo de ana", got)
	}
}

func TestSearchDirectoriesByNameAndDateRange(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	registerDirectory(t, env, owner, "Proyectos Antiguos", base)
	registerDirectory(t, env, owner, "Proyectos Nuevos", base.AddDate(0, 2, 0))
	registerDirectory(t, env, owner, "Fotos", base.AddDate(0, 2, 0))

	from := base.AddDate(0, 1, 0)
	got, err := env.directories.SearchDirectories(ctx, owner, SearchFilters{Query: "proyectos", DateFrom: &from})
	if err != nil {
		t.Fatalf("SearchDirectories: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Proyectos Nuevos" {
		t.Errorf("SearchDirectories(Query+DateFrom) = %+v, esperado solo Proyectos Nuevos", got)
	}
}

func TestSearchDirectoriesIsolatedByOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	now := time.Now().UTC()
	registerDirectory(t, env, ana, "Carpeta", now)
	registerDirectory(t, env, bea, "Carpeta", now)

	got, err := env.directories.SearchDirectories(ctx, ana, SearchFilters{Query: "carpeta"})
	if err != nil {
		t.Fatalf("SearchDirectories: %v", err)
	}
	if len(got) != 1 || got[0].OwnerID != ana {
		t.Errorf("SearchDirectories de ana vio carpetas de otro propietario: %+v", got)
	}
}

func TestSearchDirectoriesAllOwnersWithoutFilterSeesEveryone(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	now := time.Now().UTC()
	registerDirectory(t, env, ana, "De ana", now)
	registerDirectory(t, env, bea, "De bea", now)

	got, err := env.directories.SearchDirectoriesAllOwners(ctx, SearchFilters{}, "")
	if err != nil {
		t.Fatalf("SearchDirectoriesAllOwners: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("SearchDirectoriesAllOwners(sin owner) = %d resultados, esperados 2: %+v", len(got), got)
	}
}
