package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// --- MoveFile -----------------------------------------------------------

func TestMoveFileRenamesInPlace(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	original, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "viejo.txt", Content: bytes.NewReader([]byte("contenido"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	newName := "nuevo.txt"
	moved, err := env.svc.MoveFile(ctx, owner, original.ID, nil, &newName)
	if err != nil {
		t.Fatalf("MoveFile falló: %v", err)
	}
	if moved.ID != original.ID {
		t.Errorf("MoveFile no debe cambiar el ID: %q != %q", moved.ID, original.ID)
	}
	if moved.ParentPath != "/" || moved.Name != "nuevo.txt" {
		t.Errorf("moved = %+v, esperado ParentPath=/ Name=nuevo.txt", moved)
	}
	if _, _, err := env.svc.Download(ctx, owner, original.ID); err != nil {
		t.Errorf("el archivo debía seguir siendo descargable por el mismo ID: %v", err)
	}
}

func TestMoveFileToNewFolder(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")
	content := []byte("contenido a mover de carpeta")

	original, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "archivo.txt", Content: bytes.NewReader(content)})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	newParent := "/Documentos"
	moved, err := env.svc.MoveFile(ctx, owner, original.ID, &newParent, nil)
	if err != nil {
		t.Fatalf("MoveFile falló: %v", err)
	}
	if moved.ParentPath != "/Documentos" || moved.Name != "archivo.txt" {
		t.Errorf("moved = %+v, esperado ParentPath=/Documentos Name=archivo.txt", moved)
	}

	_, rc, err := env.svc.Download(ctx, owner, original.ID)
	if err != nil {
		t.Fatalf("Download tras mover falló: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, content) {
		t.Errorf("el contenido físico no sobrevivió al Move: got %q, want %q", got, content)
	}
}

// TestMoveFilePreservesVersionHistory confirma la ganancia real de mover
// de verdad frente a borrar+volver a subir (ADR-030): el ID nunca cambia,
// así que el historial de versiones (indexado por file_id) sigue intacto.
func TestMoveFilePreservesVersionHistory(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	original, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "informe.txt", Content: bytes.NewReader([]byte("v1"))})
	if err != nil {
		t.Fatalf("primer Upload falló: %v", err)
	}
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "informe.txt", Content: bytes.NewReader([]byte("v2, más larga"))}); err != nil {
		t.Fatalf("segundo Upload falló: %v", err)
	}
	versionsBefore, err := env.svc.ListVersions(ctx, owner, original.ID)
	if err != nil || len(versionsBefore) != 1 {
		t.Fatalf("versionsBefore = %v, %d, esperado 1 versión antes de mover", err, len(versionsBefore))
	}

	newName := "informe-renombrado.txt"
	if _, err := env.svc.MoveFile(ctx, owner, original.ID, nil, &newName); err != nil {
		t.Fatalf("MoveFile falló: %v", err)
	}

	versionsAfter, err := env.svc.ListVersions(ctx, owner, original.ID)
	if err != nil {
		t.Fatalf("ListVersions tras mover falló: %v", err)
	}
	if len(versionsAfter) != 1 {
		t.Errorf("len(versionsAfter) = %d, esperado 1 -- el historial debía sobrevivir al Move", len(versionsAfter))
	}
}

func TestMoveFileRejectsNonOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	victima := env.user(t, "victima")
	atacante := env.user(t, "atacante")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: victima, ParentPath: "/", Name: "secreto.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	newName := "robado.txt"
	if _, err := env.svc.MoveFile(ctx, atacante, meta.ID, nil, &newName); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, esperado ErrForbidden", err)
	}
}

func TestMoveFileRejectsWhenDestinationOccupiedByActiveFile(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	a, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("a"))})
	if err != nil {
		t.Fatalf("Upload a.txt falló: %v", err)
	}
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "b.txt", Content: bytes.NewReader([]byte("b"))}); err != nil {
		t.Fatalf("Upload b.txt falló: %v", err)
	}

	nameB := "b.txt"
	if _, err := env.svc.MoveFile(ctx, owner, a.ID, nil, &nameB); !errors.Is(err, ErrDestinationOccupied) {
		t.Errorf("err = %v, esperado ErrDestinationOccupied", err)
	}
}

func TestMoveFileRejectsWhenDestinationOccupiedByTrash(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	a, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("a"))})
	if err != nil {
		t.Fatalf("Upload a.txt falló: %v", err)
	}
	b, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "b.txt", Content: bytes.NewReader([]byte("b"))})
	if err != nil {
		t.Fatalf("Upload b.txt falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, b.ID); err != nil {
		t.Fatalf("Delete b.txt falló: %v", err)
	}

	nameB := "b.txt"
	if _, err := env.svc.MoveFile(ctx, owner, a.ID, nil, &nameB); !errors.Is(err, ErrDestinationOccupied) {
		t.Errorf("err = %v, esperado ErrDestinationOccupied (ocupado por la papelera)", err)
	}
}

func TestMoveFileRejectsTrashedSource(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	a, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("a"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, a.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}

	newName := "b.txt"
	if _, err := env.svc.MoveFile(ctx, owner, a.ID, nil, &newName); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("err = %v, esperado ErrFileNotFound -- Move no opera sobre archivos en la papelera", err)
	}
}

func TestMoveFileIsNoopWhenDestinationEqualsSource(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	a, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("a"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	sameName := "a.txt"
	moved, err := env.svc.MoveFile(ctx, owner, a.ID, nil, &sameName)
	if err != nil {
		t.Fatalf("mover al mismo sitio no debía fallar: %v", err)
	}
	if moved.ParentPath != "/" || moved.Name != "a.txt" {
		t.Errorf("moved = %+v, esperado sin cambios", moved)
	}
}

// --- MoveDirectory --------------------------------------------------------

// TestMoveDirectoryTreeRewritesAllDescendantPaths es el test central de
// ADR-030: mueve una carpeta con archivos propios Y una subcarpeta
// anidada con su propio archivo, confirmando que TODOS los descendientes
// (cualquier profundidad) quedan con el parent_path correcto Y que su
// contenido físico sigue siendo descargable (prov.Move movió el árbol
// completo, MoveDirectoryTree reescribió la BD completa).
func TestMoveDirectoryTreeRewritesAllDescendantPaths(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	root, err := env.svc.Mkdir(ctx, owner, "/", "Proyecto", "")
	if err != nil {
		t.Fatalf("Mkdir /Proyecto falló: %v", err)
	}
	if _, err := env.svc.Mkdir(ctx, owner, "/Proyecto", "Sub", ""); err != nil {
		t.Fatalf("Mkdir /Proyecto/Sub falló: %v", err)
	}
	directFile, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Proyecto", Name: "raiz.txt", Content: bytes.NewReader([]byte("raiz"))})
	if err != nil {
		t.Fatalf("Upload raiz.txt falló: %v", err)
	}
	nestedFile, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Proyecto/Sub", Name: "anidado.txt", Content: bytes.NewReader([]byte("anidado"))})
	if err != nil {
		t.Fatalf("Upload anidado.txt falló: %v", err)
	}

	newParent := "/Archivo"
	newName := "ProyectoViejo"
	if _, err := env.svc.MoveDirectory(ctx, owner, root.ID, &newParent, &newName); err != nil {
		t.Fatalf("MoveDirectory falló: %v", err)
	}

	// El archivo directo debe colgar de la nueva ruta de la carpeta.
	movedDirectFile, err := env.files.GetFileByID(ctx, directFile.ID)
	if err != nil {
		t.Fatalf("GetFileByID(directFile) falló: %v", err)
	}
	if movedDirectFile.ParentPath != "/Archivo/ProyectoViejo" {
		t.Errorf("directFile.ParentPath = %q, esperado /Archivo/ProyectoViejo", movedDirectFile.ParentPath)
	}
	// El archivo anidado (dos niveles bajo la carpeta movida) también.
	movedNestedFile, err := env.files.GetFileByID(ctx, nestedFile.ID)
	if err != nil {
		t.Fatalf("GetFileByID(nestedFile) falló: %v", err)
	}
	if movedNestedFile.ParentPath != "/Archivo/ProyectoViejo/Sub" {
		t.Errorf("nestedFile.ParentPath = %q, esperado /Archivo/ProyectoViejo/Sub", movedNestedFile.ParentPath)
	}
	// La subcarpeta en sí también debe haberse reescrito.
	defaultPool, err := env.pools.DefaultPool(ctx)
	if err != nil {
		t.Fatalf("DefaultPool falló: %v", err)
	}
	subDir, err := env.directories.GetDirectoryByNaturalKey(ctx, defaultPool.ID, owner, "/Archivo/ProyectoViejo", "Sub")
	if err != nil {
		t.Fatalf("la subcarpeta no aparece en su nueva ubicación esperada: %v", err)
	}
	if subDir.IsTrashed() {
		t.Error("la subcarpeta movida no debía quedar marcada como borrada")
	}

	// El contenido físico de AMBOS ficheros debe seguir siendo descargable
	// -- confirma que prov.Move movió el árbol físico completo, no solo la
	// carpeta de primer nivel.
	if _, rc, err := env.svc.Download(ctx, owner, directFile.ID); err != nil {
		t.Errorf("Download(directFile) tras mover la carpeta falló: %v", err)
	} else {
		got, _ := io.ReadAll(rc)
		rc.Close()
		if string(got) != "raiz" {
			t.Errorf("contenido de directFile = %q, esperado %q", got, "raiz")
		}
	}
	if _, rc, err := env.svc.Download(ctx, owner, nestedFile.ID); err != nil {
		t.Errorf("Download(nestedFile) tras mover la carpeta falló: %v", err)
	} else {
		got, _ := io.ReadAll(rc)
		rc.Close()
		if string(got) != "anidado" {
			t.Errorf("contenido de nestedFile = %q, esperado %q", got, "anidado")
		}
	}
}

func TestMoveDirectoryRejectsMovingIntoOwnDescendant(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	parent, err := env.svc.Mkdir(ctx, owner, "/", "A", "")
	if err != nil {
		t.Fatalf("Mkdir /A falló: %v", err)
	}
	if _, err := env.svc.Mkdir(ctx, owner, "/A", "B", ""); err != nil {
		t.Fatalf("Mkdir /A/B falló: %v", err)
	}

	destParent := "/A/B"
	if _, err := env.svc.MoveDirectory(ctx, owner, parent.ID, &destParent, nil); !errors.Is(err, ErrInvalidMoveDestination) {
		t.Errorf("err = %v, esperado ErrInvalidMoveDestination (mover /A dentro de /A/B, su propia subcarpeta)", err)
	}
}

func TestMoveDirectoryIsNoopWhenMovingIntoItself(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "A", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	sameParent := "/"
	sameName := "A"
	moved, err := env.svc.MoveDirectory(ctx, owner, dir.ID, &sameParent, &sameName)
	if err != nil {
		t.Fatalf("mover una carpeta a su propia ubicación no debía fallar: %v", err)
	}
	if moved.ParentPath != "/" || moved.Name != "A" {
		t.Errorf("moved = %+v, esperado sin cambios", moved)
	}
}

func TestMoveDirectoryRejectsWhenDestinationOccupied(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	a, err := env.svc.Mkdir(ctx, owner, "/", "A", "")
	if err != nil {
		t.Fatalf("Mkdir /A falló: %v", err)
	}
	if _, err := env.svc.Mkdir(ctx, owner, "/", "B", ""); err != nil {
		t.Fatalf("Mkdir /B falló: %v", err)
	}

	nameB := "B"
	if _, err := env.svc.MoveDirectory(ctx, owner, a.ID, nil, &nameB); !errors.Is(err, ErrDestinationOccupied) {
		t.Errorf("err = %v, esperado ErrDestinationOccupied", err)
	}
}

func TestMoveDirectoryRejectsNonOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	victima := env.user(t, "victima")
	atacante := env.user(t, "atacante")

	dir, err := env.svc.Mkdir(ctx, victima, "/", "Privado", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	newName := "Robado"
	if _, err := env.svc.MoveDirectory(ctx, atacante, dir.ID, nil, &newName); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, esperado ErrForbidden", err)
	}
}
