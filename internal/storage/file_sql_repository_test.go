package storage

import (
	"bytes"
	"context"
	"testing"
)

// TestListFilesByPool cubre el método nuevo del Backup Manager (Fase 5
// slice 1, ADR-015): a diferencia de ListFiles (una carpeta de un
// propietario), debe devolver TODOS los archivos activos de un pool sin
// importar propietario ni carpeta, y nunca los que están en la papelera.
// Vive en un fichero propio (no en file_service_test.go, que ya roza las
// 800 líneas de golden-principles.md #5) siguiendo el mismo criterio de
// tamaño ya aplicado en otros slices de esta sesión.
func TestListFilesByPool(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	poolID := env.defaultPoolID(t)

	alice := env.user(t, "alice")
	bob := env.user(t, "bob")

	m1, err := env.svc.Upload(ctx, UploadInput{OwnerID: alice, ParentPath: "/", Name: "uno.txt", Content: bytes.NewReader([]byte("uno"))})
	if err != nil {
		t.Fatalf("Upload (uno) falló: %v", err)
	}
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: bob, ParentPath: "/Fotos", Name: "dos.txt", Content: bytes.NewReader([]byte("dos"))}); err != nil {
		t.Fatalf("Upload (dos) falló: %v", err)
	}
	trashed, err := env.svc.Upload(ctx, UploadInput{OwnerID: alice, ParentPath: "/", Name: "tres.txt", Content: bytes.NewReader([]byte("tres"))})
	if err != nil {
		t.Fatalf("Upload (tres) falló: %v", err)
	}
	if err := env.svc.Delete(ctx, alice, trashed.ID); err != nil {
		t.Fatalf("Delete (tres) falló: %v", err)
	}

	got, err := env.files.ListFilesByPool(ctx, poolID)
	if err != nil {
		t.Fatalf("ListFilesByPool falló: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, esperado 2 (el archivo en la papelera no debe aparecer): %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, f := range got {
		names[f.Name] = true
		if f.ID == m1.ID && f.OwnerID != alice {
			t.Errorf("uno.txt: OwnerID = %q, esperado %q", f.OwnerID, alice)
		}
	}
	if !names["uno.txt"] || !names["dos.txt"] {
		t.Errorf("nombres devueltos = %v, esperados uno.txt y dos.txt", names)
	}
	if names["tres.txt"] {
		t.Errorf("tres.txt está en la papelera y no debería aparecer")
	}
}

func TestListFilesByPoolPoolVacio(t *testing.T) {
	env := newTestEnv(t, true)
	poolID := env.defaultPoolID(t)

	got, err := env.files.ListFilesByPool(context.Background(), poolID)
	if err != nil {
		t.Fatalf("ListFilesByPool falló: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, esperado 0 en un pool sin archivos", len(got))
	}
}

func (e *testEnv) defaultPoolID(t *testing.T) string {
	t.Helper()
	p, err := e.pools.DefaultPool(context.Background())
	if err != nil {
		t.Fatalf("DefaultPool falló: %v", err)
	}
	return p.ID
}
