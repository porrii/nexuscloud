package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/users"
)

// testEnv monta una base de datos sqlite real (migrada) + un
// LocalFilesystemProvider en un directorio temporal. files.owner_id tiene
// FK contra users(id) (integridad, §14), así que los tests deben crear
// usuarios reales en vez de usar strings arbitrarios como "user-1".
type testEnv struct {
	svc         *FileService
	files       FileRepository
	directories DirectoryRepository
	provider    *LocalFilesystemProvider
	userSvc     *users.Service
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "storage-test.db")

	sqlDB, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open falló: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(cfg, sqlDB); err != nil {
		t.Fatalf("db.Migrate falló: %v", err)
	}
	conn := db.Wrap(cfg.Database.Driver, sqlDB)

	pools := NewSQLPoolRepository(conn)
	if _, err := EnsureDefaultPool(context.Background(), pools, t.TempDir()); err != nil {
		t.Fatalf("EnsureDefaultPool falló: %v", err)
	}

	provider, err := NewLocalFilesystemProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalFilesystemProvider falló: %v", err)
	}
	files := NewSQLFileRepository(conn)
	directories := NewSQLDirectoryRepository(conn)

	return &testEnv{
		svc:         NewFileService(files, directories, pools, provider),
		files:       files,
		directories: directories,
		provider:    provider,
		userSvc:     users.NewService(users.NewSQLRepository(conn)),
	}
}

// user crea (si hace falta) un usuario real con ese username y devuelve su
// ID, para satisfacer la FK files.owner_id -> users.id.
func (e *testEnv) user(t *testing.T, username string) string {
	t.Helper()
	u, err := e.userSvc.CreateUser(context.Background(), users.CreateUserInput{Username: username, PasswordHash: "hash-de-prueba"})
	if err != nil {
		t.Fatalf("creando usuario de prueba %q: %v", username, err)
	}
	return u.ID
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")
	content := []byte("contenido de prueba con ñ y áéíóú")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Documentos", Name: "informe.txt", Content: bytes.NewReader(content)})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if meta.SizeBytes != int64(len(content)) {
		t.Errorf("SizeBytes = %d, esperado %d", meta.SizeBytes, len(content))
	}

	_, rc, err := env.svc.Download(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("Download falló: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("leyendo contenido descargado: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("contenido descargado no coincide con el subido: got %q, want %q", got, content)
	}
}

func TestDownloadRejectsNonOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	victima := env.user(t, "victima")
	atacante := env.user(t, "atacante")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: victima, ParentPath: "/", Name: "secreto.txt", Content: bytes.NewReader([]byte("secreto"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	// El atacante conoce (adivina) el ID del archivo pero no es su
	// propietario: debe rechazarse (§198 IDOR).
	if _, _, err := env.svc.Download(ctx, atacante, meta.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, esperado ErrForbidden", err)
	}
}

func TestDeleteRejectsNonOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	victima := env.user(t, "victima")
	atacante := env.user(t, "atacante")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: victima, ParentPath: "/", Name: "secreto.txt", Content: bytes.NewReader([]byte("secreto"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, atacante, meta.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, esperado ErrForbidden", err)
	}
	// El archivo debe seguir accesible para su propietario legítimo.
	if _, _, err := env.svc.Download(ctx, victima, meta.ID); err != nil {
		t.Errorf("el archivo no debería haberse borrado: %v", err)
	}
}

func TestUploadRejectsPathTraversalInName(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	_, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "../../../etc/passwd", Content: bytes.NewReader([]byte("x"))})
	if !errors.Is(err, ErrInvalidName) {
		t.Errorf("err = %v, esperado ErrInvalidName", err)
	}
}

func TestUploadNormalizesPathTraversalInParentPath(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/../../etc", Name: "passwd.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload no debería fallar, debería normalizar la ruta: %v", err)
	}
	// normalizeParentPath debe haber colapsado el "../../" dentro del árbol
	// lógico del propio usuario: el contenido termina en el árbol de owner,
	// nunca fuera de la raíz del pool.
	if meta.ParentPath != "/etc" {
		t.Errorf("ParentPath = %q, esperado /etc (normalizado dentro del árbol lógico)", meta.ParentPath)
	}
	if _, _, err := env.svc.Download(ctx, owner, meta.ID); err != nil {
		t.Errorf("el propietario debería poder descargar su propio archivo: %v", err)
	}
}

func TestUploadOverwritesExistingPathKeepingSameID(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	first, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "notas.txt", Content: bytes.NewReader([]byte("v1"))})
	if err != nil {
		t.Fatalf("primer Upload falló: %v", err)
	}
	second, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "notas.txt", Content: bytes.NewReader([]byte("versión dos, más larga"))})
	if err != nil {
		t.Fatalf("segundo Upload falló: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("subir al mismo path debería conservar el mismo ID: %q != %q", first.ID, second.ID)
	}

	_, rc, err := env.svc.Download(ctx, owner, second.ID)
	if err != nil {
		t.Fatalf("Download falló: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "versión dos, más larga" {
		t.Errorf("contenido tras sobrescribir = %q, esperado la segunda versión", got)
	}
}

func TestTwoUsersCanUseTheSamePathWithoutColliding(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	userA := env.user(t, "user-a")
	userB := env.user(t, "user-b")

	a, err := env.svc.Upload(ctx, UploadInput{OwnerID: userA, ParentPath: "/", Name: "mismo-nombre.txt", Content: bytes.NewReader([]byte("de A"))})
	if err != nil {
		t.Fatalf("Upload de A falló: %v", err)
	}
	b, err := env.svc.Upload(ctx, UploadInput{OwnerID: userB, ParentPath: "/", Name: "mismo-nombre.txt", Content: bytes.NewReader([]byte("de B"))})
	if err != nil {
		t.Fatalf("Upload de B falló: %v", err)
	}
	if a.ID == b.ID {
		t.Fatal("dos usuarios distintos no deberían compartir fila de metadatos por tener el mismo nombre de archivo")
	}

	_, rcA, _ := env.svc.Download(ctx, userA, a.ID)
	gotA, _ := io.ReadAll(rcA)
	rcA.Close()
	_, rcB, _ := env.svc.Download(ctx, userB, b.ID)
	gotB, _ := io.ReadAll(rcB)
	rcB.Close()

	if string(gotA) != "de A" || string(gotB) != "de B" {
		t.Errorf("el contenido de los dos usuarios se mezcló: A=%q B=%q", gotA, gotB)
	}
}

func TestDeleteRemovesContentAndMetadata(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "borrame.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, meta.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}
	if _, err := env.files.GetFileByID(ctx, meta.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("los metadatos deberían haber desaparecido: err = %v", err)
	}
	if _, _, err := env.svc.Download(ctx, owner, meta.ID); err == nil {
		t.Error("descargar un archivo borrado debería fallar")
	}
}

func TestListReturnsOnlyFilesInThatParentPath(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Fotos", Name: "a.jpg", Content: bytes.NewReader([]byte("a"))}); err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Documentos", Name: "b.txt", Content: bytes.NewReader([]byte("b"))}); err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	got, err := env.svc.List(ctx, owner, "/Fotos")
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(got.Files) != 1 || got.Files[0].Name != "a.jpg" {
		t.Errorf("List(/Fotos).Files = %+v, esperado solo a.jpg", got.Files)
	}
}

func TestMkdirCreatesAListableDirectory(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if dir.Name != "Proyectos" {
		t.Errorf("Name = %q, esperado Proyectos", dir.Name)
	}

	got, err := env.svc.List(ctx, owner, "/")
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(got.Directories) != 1 || got.Directories[0].Name != "Proyectos" {
		t.Errorf("List(/).Directories = %+v, esperado solo Proyectos", got.Directories)
	}
}

func TestMkdirIsIdempotent(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	if _, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos"); err != nil {
		t.Fatalf("primer Mkdir falló: %v", err)
	}
	if _, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos"); err != nil {
		t.Errorf("crear una carpeta ya existente no debería fallar: %v", err)
	}
}

func TestDeleteDirectoryRejectsNonEmpty(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Proyectos", Name: "notas.txt", Content: bytes.NewReader([]byte("x"))}); err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); !errors.Is(err, ErrDirectoryNotEmpty) {
		t.Errorf("err = %v, esperado ErrDirectoryNotEmpty", err)
	}
}

func TestDeleteDirectoryRemovesEmptyDirectory(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Vacia")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory falló: %v", err)
	}

	got, err := env.svc.List(ctx, owner, "/")
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(got.Directories) != 0 {
		t.Errorf("la carpeta debería haber desaparecido del listado: %+v", got.Directories)
	}
}

func TestDeleteDirectoryRejectsNonOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	victima := env.user(t, "victima")
	atacante := env.user(t, "atacante")

	dir, err := env.svc.Mkdir(ctx, victima, "/", "Privada")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if err := env.svc.DeleteDirectory(ctx, atacante, dir.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, esperado ErrForbidden (§198 IDOR)", err)
	}
}
