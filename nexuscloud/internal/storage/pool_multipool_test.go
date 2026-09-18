package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// addPool crea un pool local extra en un directorio temporal nuevo y lo
// devuelve, junto con su ruta.
func (e *testEnv) addPool(t *testing.T, name string, priority int) (*Pool, string) {
	t.Helper()
	dir := t.TempDir()
	p := &Pool{
		ID: idgen.New(), Name: name, Type: "local", Path: dir,
		Priority: priority, Status: "active", CreatedAt: time.Now().UTC(),
	}
	if err := e.pools.CreatePool(context.Background(), p); err != nil {
		t.Fatalf("CreatePool(%s): %v", name, err)
	}
	return p, dir
}

func readAll(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("leyendo contenido: %v", err)
	}
	return b
}

func TestMultiPoolWritesAndReadsPerPool(t *testing.T) {
	e := newTestEnv(t, true)
	ctx := context.Background()
	owner := e.user(t, "multipool")

	// Fichero 1: va al pool por defecto (el que crea newTestEnv, poolDir).
	f1, err := e.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: strings.NewReader("en el pool por defecto")})
	if err != nil {
		t.Fatalf("Upload f1: %v", err)
	}

	// Nuevo pool con prioridad más alta (número menor) -> pasa a ser el
	// DefaultPool para los siguientes ficheros.
	poolB, dirB := e.addPool(t, "rapido", -1)

	f2, err := e.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "b.txt", Content: strings.NewReader("en el pool rapido")})
	if err != nil {
		t.Fatalf("Upload f2: %v", err)
	}
	if f2.PoolID != poolB.ID {
		t.Fatalf("f2.PoolID = %s, esperado el pool nuevo %s", f2.PoolID, poolB.ID)
	}
	if f1.PoolID == f2.PoolID {
		t.Fatal("f1 y f2 deberían estar en pools distintos")
	}

	// Ambos se descargan bien (cada uno resuelto a su pool).
	_, rc1, err := e.svc.Download(ctx, owner, f1.ID)
	if err != nil {
		t.Fatalf("Download f1: %v", err)
	}
	if got := string(readAll(t, rc1)); got != "en el pool por defecto" {
		t.Errorf("contenido f1 = %q", got)
	}
	_, rc2, err := e.svc.Download(ctx, owner, f2.ID)
	if err != nil {
		t.Fatalf("Download f2: %v", err)
	}
	if got := string(readAll(t, rc2)); got != "en el pool rapido" {
		t.Errorf("contenido f2 = %q", got)
	}

	// Verificación física: el byte-tree de f2 vive bajo dirB, no bajo el
	// pool por defecto.
	if !fileExistsUnder(dirB, owner) {
		t.Errorf("no se encontró el árbol de %s bajo el pool nuevo %s", owner, dirB)
	}
	if fileExistsUnder(e.poolDir, "b.txt") {
		t.Error("b.txt no debería haberse escrito en el pool por defecto")
	}

	// Borrar f1 no debe afectar a f2.
	if err := e.svc.PermanentlyDeleteFile(ctx, owner, f1.ID); err != nil {
		t.Fatalf("PermanentlyDeleteFile f1: %v", err)
	}
	// El ReadCloser hay que cerrarlo explícitamente: en Windows un fichero
	// con un handle abierto no se puede borrar, así que dejarlo sin cerrar
	// aquí hace que la limpieza de t.TempDir() falle al final del test
	// (en Linux no se nota porque unlink() sí borra ficheros abiertos).
	if _, rc, err := e.svc.Download(ctx, owner, f2.ID); err != nil {
		t.Errorf("f2 debería seguir descargándose tras borrar f1: %v", err)
	} else {
		rc.Close()
	}
}

func fileExistsUnder(root, needle string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.Contains(path, needle) {
			found = true
		}
		return nil
	})
	return found
}

func TestPoolRepositoryUpdateAndStatus(t *testing.T) {
	e := newTestEnv(t, true)
	ctx := context.Background()
	p, _ := e.addPool(t, "editable", 100)

	p.Name = "renombrado"
	p.Priority = 42
	p.VersioningPolicy = PolicyOff
	if err := e.pools.UpdatePool(ctx, p); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	got, err := e.pools.GetPoolByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renombrado" || got.Priority != 42 || got.VersioningPolicy != PolicyOff {
		t.Errorf("update no persistió: %+v", got)
	}

	if err := e.pools.SetPoolStatus(ctx, p.ID, "disabled"); err != nil {
		t.Fatalf("SetPoolStatus: %v", err)
	}
	got, _ = e.pools.GetPoolByID(ctx, p.ID)
	if got.Status != "disabled" {
		t.Errorf("status = %q, esperado disabled", got.Status)
	}

	// Política inválida -> rechazada.
	bad := *got
	bad.UtilizationPolicy = "inventada"
	if err := e.pools.UpdatePool(ctx, &bad); !errors.Is(err, ErrInvalidPoolPolicy) {
		t.Errorf("UpdatePool con política inválida: err = %v, esperado ErrInvalidPoolPolicy", err)
	}
}

func TestPoolRepositoryDeleteRejectsPoolInUse(t *testing.T) {
	e := newTestEnv(t, true)
	ctx := context.Background()
	owner := e.user(t, "dueno")

	// Un pool con un fichero dentro no se puede borrar (FK ON DELETE RESTRICT).
	poolB, _ := e.addPool(t, "conarchivos", -1) // -1 = pasa a ser el default
	if _, err := e.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "x.txt", Content: bytes.NewReader([]byte("x"))}); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	if err := e.pools.DeletePool(ctx, poolB.ID); !errors.Is(err, ErrPoolInUse) {
		t.Errorf("DeletePool de un pool con ficheros: err = %v, esperado ErrPoolInUse", err)
	}

	// Un pool vacío sí se borra.
	empty, _ := e.addPool(t, "vacio", 200)
	if err := e.pools.DeletePool(ctx, empty.ID); err != nil {
		t.Errorf("DeletePool de un pool vacío no debería fallar: %v", err)
	}
	if _, err := e.pools.GetPoolByID(ctx, empty.ID); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("el pool vacío debería haber desaparecido: %v", err)
	}
}
