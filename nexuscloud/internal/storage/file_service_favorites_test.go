package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// Favoritos (§87, ADR-038): calcan el patrón polimórfico de shares, pero
// solo sobre el árbol PROPIO del usuario -- no hay concepto de "favorito
// sobre algo compartido" en este esquema, así que ni se prueba.

func uploadFile(t *testing.T, env *testEnv, ownerID, name string) *FileMeta {
	t.Helper()
	meta, err := env.svc.Upload(context.Background(), UploadInput{
		OwnerID: ownerID, ParentPath: "/", Name: name, Content: bytes.NewReader(blob(1, 10)),
	})
	if err != nil {
		t.Fatalf("Upload %s: %v", name, err)
	}
	return meta
}

func mkdir(t *testing.T, env *testEnv, ownerID, name string) *Directory {
	t.Helper()
	dir, err := env.svc.Mkdir(context.Background(), ownerID, "/", name, "")
	if err != nil {
		t.Fatalf("Mkdir %s: %v", name, err)
	}
	return dir
}

func TestAddFavoriteFileAndDirectory(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "informe.pdf")
	d := mkdir(t, env, owner, "Fotos")

	favFile, err := env.svc.AddFavorite(ctx, owner, false, f.ID)
	if err != nil {
		t.Fatalf("AddFavorite archivo: %v", err)
	}
	if favFile.IsDirectoryFavorite() || favFile.FileID != f.ID {
		t.Errorf("favorito de archivo = %+v", favFile)
	}

	favDir, err := env.svc.AddFavorite(ctx, owner, true, d.ID)
	if err != nil {
		t.Fatalf("AddFavorite carpeta: %v", err)
	}
	if !favDir.IsDirectoryFavorite() || favDir.DirectoryID != d.ID {
		t.Errorf("favorito de carpeta = %+v", favDir)
	}

	files, dirs, err := env.svc.ListFavorites(ctx, owner)
	if err != nil {
		t.Fatalf("ListFavorites: %v", err)
	}
	if len(files) != 1 || files[0].ID != f.ID {
		t.Errorf("archivos favoritos = %+v", files)
	}
	if len(dirs) != 1 || dirs[0].ID != d.ID {
		t.Errorf("carpetas favoritas = %+v", dirs)
	}
}

func TestAddFavoriteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "informe.pdf")

	first, err := env.svc.AddFavorite(ctx, owner, false, f.ID)
	if err != nil {
		t.Fatalf("primer AddFavorite: %v", err)
	}
	second, err := env.svc.AddFavorite(ctx, owner, false, f.ID)
	if err != nil {
		t.Fatalf("segundo AddFavorite (debe ser un no-op, no un error): %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("segundo AddFavorite devolvió un ID distinto: %q vs %q", second.ID, first.ID)
	}

	files, _, err := env.svc.ListFavorites(ctx, owner)
	if err != nil {
		t.Fatalf("ListFavorites: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("favoritar dos veces no debe duplicar la entrada: %+v", files)
	}
}

func TestAddFavoriteRejectsResourceOwnedByAnotherUser(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	stranger := env.user(t, "extrana")
	f := uploadFile(t, env, owner, "informe.pdf")

	if _, err := env.svc.AddFavorite(ctx, stranger, false, f.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("favoritar el archivo de otro = %v, esperado ErrForbidden", err)
	}
}

func TestAddFavoriteRejectsMissingResource(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")

	if _, err := env.svc.AddFavorite(ctx, owner, false, "no-existe"); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("favoritar un archivo inexistente = %v, esperado ErrFileNotFound", err)
	}
	if _, err := env.svc.AddFavorite(ctx, owner, true, "no-existe"); !errors.Is(err, ErrDirectoryNotFound) {
		t.Errorf("favoritar una carpeta inexistente = %v, esperado ErrDirectoryNotFound", err)
	}
}

func TestRemoveFavoriteRequiresOwnerMatch(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	stranger := env.user(t, "extrana")
	f := uploadFile(t, env, owner, "informe.pdf")
	fav, err := env.svc.AddFavorite(ctx, owner, false, f.ID)
	if err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}

	if err := env.svc.RemoveFavorite(ctx, stranger, fav.ID); !errors.Is(err, ErrFavoriteNotFound) {
		t.Errorf("quitar el favorito de otro = %v, esperado ErrFavoriteNotFound", err)
	}
	files, _, _ := env.svc.ListFavorites(ctx, owner)
	if len(files) != 1 {
		t.Error("tras el intento fallido, el favorito debería seguir ahí")
	}

	if err := env.svc.RemoveFavorite(ctx, owner, fav.ID); err != nil {
		t.Fatalf("RemoveFavorite por el propietario: %v", err)
	}
	files, _, _ = env.svc.ListFavorites(ctx, owner)
	if len(files) != 0 {
		t.Errorf("tras quitarlo, ListFavorites = %+v, esperado vacío", files)
	}
}

func TestListFavoritesExcludesTrashedAndReappearsOnRestore(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "informe.pdf")
	if _, err := env.svc.AddFavorite(ctx, owner, false, f.ID); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}

	if err := env.svc.Delete(ctx, owner, f.ID); err != nil {
		t.Fatalf("Delete (a la papelera): %v", err)
	}
	files, _, err := env.svc.ListFavorites(ctx, owner)
	if err != nil {
		t.Fatalf("ListFavorites tras trashear: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("un favorito trasheado debe ocultarse: %+v", files)
	}

	if err := env.svc.RestoreFile(ctx, owner, f.ID); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	files, _, err = env.svc.ListFavorites(ctx, owner)
	if err != nil {
		t.Fatalf("ListFavorites tras restaurar: %v", err)
	}
	if len(files) != 1 || files[0].ID != f.ID {
		t.Errorf("tras restaurar, el favorito debería reaparecer: %+v", files)
	}
}

func TestListFavoritesIsIsolatedPerUser(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	fa := uploadFile(t, env, ana, "a.txt")
	fb := uploadFile(t, env, bea, "b.txt")
	if _, err := env.svc.AddFavorite(ctx, ana, false, fa.ID); err != nil {
		t.Fatalf("AddFavorite ana: %v", err)
	}
	if _, err := env.svc.AddFavorite(ctx, bea, false, fb.ID); err != nil {
		t.Fatalf("AddFavorite bea: %v", err)
	}

	anaFiles, _, err := env.svc.ListFavorites(ctx, ana)
	if err != nil || len(anaFiles) != 1 || anaFiles[0].ID != fa.ID {
		t.Errorf("favoritos de ana = %+v (%v), esperado solo a.txt", anaFiles, err)
	}

	anaFileIDs, anaDirIDs, err := env.svc.FavoriteIDsForOwner(ctx, ana)
	if err != nil {
		t.Fatalf("FavoriteIDsForOwner: %v", err)
	}
	if _, ok := anaFileIDs[fa.ID]; !ok {
		t.Errorf("FavoriteIDsForOwner(ana) = %+v, esperaba %q", anaFileIDs, fa.ID)
	}
	if _, ok := anaFileIDs[fb.ID]; ok {
		t.Error("FavoriteIDsForOwner(ana) no debe incluir el favorito de bea")
	}
	if len(anaDirIDs) != 0 {
		t.Errorf("anaDirIDs = %+v, esperado vacío (sin carpetas favoritas)", anaDirIDs)
	}
}

func TestFavoriteCascadesOnPermanentDelete(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "informe.pdf")
	fav, err := env.svc.AddFavorite(ctx, owner, false, f.ID)
	if err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}

	if err := env.svc.PermanentlyDeleteFile(ctx, owner, f.ID); err != nil {
		t.Fatalf("PermanentlyDeleteFile: %v", err)
	}

	// ON DELETE CASCADE debe haber borrado la fila del favorito -- quitarlo
	// ahora debe dar "no encontrado", no un fallo por FK huérfana.
	if err := env.favorites.RemoveFavorite(ctx, fav.ID, owner); !errors.Is(err, ErrFavoriteNotFound) {
		t.Errorf("tras el borrado permanente del archivo, el favorito debería haber desaparecido: err = %v", err)
	}
}

func TestFavoritesWithoutWithFavoritesReturnsUnavailable(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "informe.pdf")

	// Un FileService construido sin WithFavorites (mismo patrón que
	// ErrUsageUnavailable sin WithQuotas).
	bareSvc := NewFileService(env.files, env.directories, env.versions, env.shares, env.pools,
		NewPoolProviderResolver(env.pools), &testPasswordHasher{}, true, true, 10, 0, 0, true, true)

	if _, err := bareSvc.AddFavorite(ctx, owner, false, f.ID); !errors.Is(err, ErrFavoritesUnavailable) {
		t.Errorf("AddFavorite sin WithFavorites = %v, esperado ErrFavoritesUnavailable", err)
	}
	if err := bareSvc.RemoveFavorite(ctx, owner, "x"); !errors.Is(err, ErrFavoritesUnavailable) {
		t.Errorf("RemoveFavorite sin WithFavorites = %v, esperado ErrFavoritesUnavailable", err)
	}
	if _, _, err := bareSvc.ListFavorites(ctx, owner); !errors.Is(err, ErrFavoritesUnavailable) {
		t.Errorf("ListFavorites sin WithFavorites = %v, esperado ErrFavoritesUnavailable", err)
	}
	if _, _, err := bareSvc.FavoriteIDsForOwner(ctx, owner); !errors.Is(err, ErrFavoritesUnavailable) {
		t.Errorf("FavoriteIDsForOwner sin WithFavorites = %v, esperado ErrFavoritesUnavailable", err)
	}
}
