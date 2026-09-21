package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/idgen"
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
	versions    VersionRepository
	shares      ShareRepository
	pools       *SQLPoolRepository
	poolDir     string
	provider    *LocalFilesystemProvider
	userSvc     *users.Service
	userRepo    users.Repository
	// conn permite a los tests hacer lo que los repositorios no exponen (p.ej.
	// quitar a alguien de un grupo).
	conn *db.Conn
}

// newTestEnv mantiene su firma de dos parámetros (usada por ~20 tests
// preexistentes) con versionado y compartición activados por defecto;
// newTestEnvFull permite controlar también esas dimensiones para los tests
// que las ejercitan directamente.
func newTestEnv(t *testing.T, trashEnabled bool) *testEnv {
	t.Helper()
	return newTestEnvFull(t, trashEnabled, true, 10, 0, 0, true, true)
}

func newTestEnvFull(t *testing.T, trashEnabled, versioningEnabled bool, maxVersionsPerFile, maxVersionAgeDays int, maxVersionsTotalSizeBytes int64, sharingEnabled, publicLinksEnabled bool) *testEnv {
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

	poolDir := t.TempDir()
	pools := NewSQLPoolRepository(conn)
	if _, err := EnsureDefaultPool(context.Background(), pools, poolDir); err != nil {
		t.Fatalf("EnsureDefaultPool falló: %v", err)
	}

	// provider de inspección directa del filesystem, sobre la MISMA ruta que
	// el pool por defecto: el ProviderResolver del servicio resuelve ese
	// mismo poolDir, así que lo que escribe el servicio se ve por aquí.
	provider, err := NewLocalFilesystemProvider(poolDir)
	if err != nil {
		t.Fatalf("NewLocalFilesystemProvider falló: %v", err)
	}
	resolver := NewPoolProviderResolver(pools)
	files := NewSQLFileRepository(conn)
	directories := NewSQLDirectoryRepository(conn)
	versions := NewSQLVersionRepository(conn)
	shares := NewSQLShareRepository(conn)
	userRepo := users.NewSQLRepository(conn)

	return &testEnv{
		svc: NewFileService(files, directories, versions, shares, pools, resolver, &testPasswordHasher{},
			trashEnabled, versioningEnabled, maxVersionsPerFile, maxVersionAgeDays, maxVersionsTotalSizeBytes,
			sharingEnabled, publicLinksEnabled),
		files:       files,
		directories: directories,
		versions:    versions,
		shares:      shares,
		pools:       pools,
		poolDir:     poolDir,
		provider:    provider,
		userSvc:     users.NewService(userRepo),
		userRepo:    userRepo,
		conn:        conn,
	}
}

// testPasswordHasher es un PasswordHasher trivial y determinista para
// pruebas -- evita depender de auth.Hasher (internal/storage no puede
// importar internal/auth, ver share.go) y de la lentitud deliberada de
// Argon2id en un test que crea muchas contraseñas.
type testPasswordHasher struct{}

func (testPasswordHasher) Hash(password string) (string, error) {
	return "test-hash:" + password, nil
}

func (testPasswordHasher) Verify(password, encodedHash string) error {
	if encodedHash != "test-hash:"+password {
		return errors.New("contraseña incorrecta")
	}
	return nil
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

// group crea un grupo real y devuelve su ID, para las pruebas de
// compartición usuario→grupo (§37).
func (e *testEnv) group(t *testing.T, name string) string {
	t.Helper()
	g := &users.Group{ID: idgen.New(), Name: name, CreatedAt: time.Now().UTC()}
	if err := e.userRepo.CreateGroup(context.Background(), g); err != nil {
		t.Fatalf("creando grupo de prueba %q: %v", name, err)
	}
	return g.ID
}

func (e *testEnv) addToGroup(t *testing.T, userID, groupID string) {
	t.Helper()
	if err := e.userRepo.AddUserToGroup(context.Background(), userID, groupID); err != nil {
		t.Fatalf("añadiendo usuario %s al grupo %s: %v", userID, groupID, err)
	}
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
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
	env := newTestEnv(t, true)
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
	env := newTestEnv(t, true)
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
	if _, rc, err := env.svc.Download(ctx, victima, meta.ID); err != nil {
		t.Errorf("el archivo no debería haberse borrado: %v", err)
	} else {
		rc.Close()
	}
}

func TestUploadRejectsPathTraversalInName(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	_, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "../../../etc/passwd", Content: bytes.NewReader([]byte("x"))})
	if !errors.Is(err, ErrInvalidName) {
		t.Errorf("err = %v, esperado ErrInvalidName", err)
	}
}

func TestUploadNormalizesPathTraversalInParentPath(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
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
	if _, rc, err := env.svc.Download(ctx, owner, meta.ID); err != nil {
		t.Errorf("el propietario debería poder descargar su propio archivo: %v", err)
	} else {
		rc.Close()
	}
}

func TestUploadOverwritesExistingPathKeepingSameID(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
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
	env := newTestEnv(t, true)
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

// Con la papelera activada (por defecto), Delete es un soft-delete: el
// archivo desaparece del listado pero sigue existiendo (§16).
func TestDeleteMovesToTrashWhenEnabled(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "borrame.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, meta.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}

	got, err := env.files.GetFileByID(ctx, meta.ID)
	if err != nil {
		t.Fatalf("los metadatos deberían seguir existiendo (en la papelera): %v", err)
	}
	if !got.IsTrashed() {
		t.Error("el archivo debería estar marcado como en la papelera")
	}
	if _, rc, err := env.svc.Download(ctx, owner, meta.ID); err != nil {
		t.Errorf("un archivo en la papelera debería seguir siendo descargable por su dueño: %v", err)
	} else {
		rc.Close()
	}

	list, err := env.svc.List(ctx, owner, "/")
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(list.Files) != 0 {
		t.Errorf("un archivo en la papelera no debería aparecer en el listado normal: %+v", list.Files)
	}

	trash, err := env.svc.ListTrash(ctx, owner)
	if err != nil {
		t.Fatalf("ListTrash falló: %v", err)
	}
	if len(trash.Files) != 1 || trash.Files[0].ID != meta.ID {
		t.Errorf("ListTrash = %+v, esperado solo %s", trash.Files, meta.ID)
	}
}

// Con la papelera desactivada, Delete borra de inmediato y para siempre
// (comportamiento original de la Fase 1/2a).
func TestDeletePermanentWhenTrashDisabled(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, false)
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
		t.Error("descargar un archivo borrado para siempre debería fallar")
	}
}

func TestRestoreFileBringsItBackToListing(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "recupera.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, meta.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}
	if err := env.svc.RestoreFile(ctx, owner, meta.ID); err != nil {
		t.Fatalf("RestoreFile falló: %v", err)
	}

	list, err := env.svc.List(ctx, owner, "/")
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(list.Files) != 1 || list.Files[0].ID != meta.ID {
		t.Errorf("List(/) tras restaurar = %+v, esperado solo %s", list.Files, meta.ID)
	}
}

func TestUploadRejectsNameOccupiedByTrash(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "notas.txt", Content: bytes.NewReader([]byte("original"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, meta.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}

	_, err = env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "notas.txt", Content: bytes.NewReader([]byte("nuevo"))})
	if !errors.Is(err, ErrNameOccupiedByTrash) {
		t.Errorf("err = %v, esperado ErrNameOccupiedByTrash (no debe resucitar/sobrescribir en silencio, §128)", err)
	}

	// El contenido original en la papelera debe seguir intacto.
	_, rc, err := env.svc.Download(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("Download del archivo en papelera falló: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "original" {
		t.Errorf("el intento de subida rechazado no debería haber tocado el contenido en papelera: got %q", got)
	}
}

func TestPermanentlyDeleteFileRemovesItForGood(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "adios.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, meta.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}
	if err := env.svc.PermanentlyDeleteFile(ctx, owner, meta.ID); err != nil {
		t.Fatalf("PermanentlyDeleteFile falló: %v", err)
	}
	if _, err := env.files.GetFileByID(ctx, meta.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("err = %v, esperado ErrFileNotFound tras el borrado definitivo", err)
	}
}

func TestPurgeExpiredTrashRemovesOnlyOldEnoughItems(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	oldMeta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "viejo.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	recentMeta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "reciente.txt", Content: bytes.NewReader([]byte("y"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, oldMeta.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, recentMeta.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}
	// Retrocedemos artificialmente la fecha de borrado del primero para
	// simular que lleva mucho tiempo en la papelera.
	if err := env.files.SoftDeleteFile(ctx, oldMeta.ID, time.Now().UTC().Add(-48*time.Hour)); err != nil {
		t.Fatalf("SoftDeleteFile falló: %v", err)
	}

	purgedFiles, _, err := env.svc.PurgeExpiredTrash(ctx, 24*time.Hour, 0)
	if err != nil {
		t.Fatalf("PurgeExpiredTrash falló: %v", err)
	}
	if purgedFiles != 1 {
		t.Errorf("purgedFiles = %d, esperado 1", purgedFiles)
	}
	if _, err := env.files.GetFileByID(ctx, oldMeta.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("el archivo viejo debería haberse purgado: err = %v", err)
	}
	if _, err := env.files.GetFileByID(ctx, recentMeta.ID); err != nil {
		t.Errorf("el archivo reciente no debería haberse purgado todavía: %v", err)
	}
}

// TestPurgeExpiredTrashPrunesBySizeAloneEvenWhenRecentlyDeleted prueba la
// mitad de la composición "el más restrictivo gana" (ADR-023) que el test
// de arriba no cubre: con una retención por antigüedad generosísima (nada
// se purgaría por antigüedad), un límite de tamaño ajustado debe purgar
// igualmente el archivo más antiguo de la papelera para hacer sitio,
// aunque ese archivo esté muy lejos de cumplir los 30 días.
func TestPurgeExpiredTrashPrunesBySizeAloneEvenWhenRecentlyDeleted(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	grande, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "grande.bin", Content: bytes.NewReader([]byte("0123456789"))}) // 10 bytes
	if err != nil {
		t.Fatalf("upload grande falló: %v", err)
	}
	pequeno, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "pequeno.bin", Content: bytes.NewReader([]byte("abcde"))}) // 5 bytes
	if err != nil {
		t.Fatalf("upload pequeno falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, grande.ID); err != nil {
		t.Fatalf("Delete grande falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, pequeno.ID); err != nil {
		t.Fatalf("Delete pequeno falló: %v", err)
	}
	// Separamos los borrados para que quede claro cuál es el más antiguo de
	// los dos -- ambos siguen muy lejos de los 30 días de retención.
	if err := env.files.SoftDeleteFile(ctx, grande.ID, time.Now().UTC().Add(-1*time.Hour)); err != nil {
		t.Fatalf("SoftDeleteFile falló: %v", err)
	}

	// Retención de 30 días (nadie la cumple) + límite de 12 bytes (los 15
	// bytes totales no caben): el más antiguo (grande.bin, 10 bytes) debe
	// podarse para que sobrevivan los 5 bytes más recientes (pequeno.bin).
	purgedFiles, _, err := env.svc.PurgeExpiredTrash(ctx, 30*24*time.Hour, 12)
	if err != nil {
		t.Fatalf("PurgeExpiredTrash falló: %v", err)
	}
	if purgedFiles != 1 {
		t.Fatalf("purgedFiles = %d, esperado 1 (grande.bin, por tamaño)", purgedFiles)
	}
	if _, err := env.files.GetFileByID(ctx, grande.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("grande.bin (el más antiguo) debería haberse purgado por tamaño pese a no cumplir la retención por antigüedad: err = %v", err)
	}
	if _, err := env.files.GetFileByID(ctx, pequeno.ID); err != nil {
		t.Errorf("pequeno.bin (el más reciente) no debería haberse purgado: %v", err)
	}
}

// TestPurgeExpiredTrashAgePrunesEvenWhenWellWithinSizeLimit es la otra
// mitad: con un límite de tamaño generosísimo (nada se purgaría por
// tamaño), la retención por antigüedad debe seguir purgando lo que lleve
// más tiempo del permitido -- confirma que un límite de tamaño amplio
// nunca "protege" de la purga por antigüedad.
func TestPurgeExpiredTrashAgePrunesEvenWhenWellWithinSizeLimit(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	oldMeta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "viejo.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, oldMeta.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}
	if err := env.files.SoftDeleteFile(ctx, oldMeta.ID, time.Now().UTC().Add(-48*time.Hour)); err != nil {
		t.Fatalf("SoftDeleteFile falló: %v", err)
	}

	purgedFiles, _, err := env.svc.PurgeExpiredTrash(ctx, 24*time.Hour, 1024*1024)
	if err != nil {
		t.Fatalf("PurgeExpiredTrash falló: %v", err)
	}
	if purgedFiles != 1 {
		t.Errorf("purgedFiles = %d, esperado 1 (por antigüedad, pese al límite de tamaño amplio)", purgedFiles)
	}
	if _, err := env.files.GetFileByID(ctx, oldMeta.ID); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("el archivo viejo debería haberse purgado por antigüedad: err = %v", err)
	}
}

func TestListReturnsOnlyFilesInThatParentPath(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
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
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", "")
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
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	if _, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", ""); err != nil {
		t.Fatalf("primer Mkdir falló: %v", err)
	}
	if _, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", ""); err != nil {
		t.Errorf("crear una carpeta ya existente no debería fallar: %v", err)
	}
}

func TestDeleteDirectoryRejectsNonEmpty(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", "")
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
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Vacia", "")
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

// Con la papelera activa, Delete de un archivo es solo una marca en base de
// datos: el contenido sigue físicamente en su carpeta. DeleteDirectory
// documenta que una carpeta con solo elementos en la papelera "cuenta como
// vacía", pero retirar su marcador físico con os.Remove fallaba con
// "directorio no vacío" -- el usuario no podía borrar una carpeta después de
// borrar todo lo que había dentro.
func TestDeleteDirectoryWhoseFilesAreAllInTheTrash(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	file, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Proyectos", Name: "notas.txt", Content: bytes.NewReader([]byte("contenido"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, file.ID); err != nil {
		t.Fatalf("Delete del archivo falló: %v", err)
	}

	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory de una carpeta cuyo único contenido está en la papelera falló: %v", err)
	}

	// Nada se ha destruido: restaurar carpeta y archivo devuelve el contenido.
	if err := env.svc.RestoreDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("RestoreDirectory falló: %v", err)
	}
	if err := env.svc.RestoreFile(ctx, owner, file.ID); err != nil {
		t.Fatalf("RestoreFile falló: %v", err)
	}
	_, rc, err := env.svc.Download(ctx, owner, file.ID)
	if err != nil {
		t.Fatalf("Download tras restaurar falló: %v", err)
	}
	if got := string(readAll(t, rc)); got != "contenido" {
		t.Errorf("contenido tras restaurar = %q", got)
	}
}

// Borrar para siempre lo que hay en la papelera no debe dejar carpetas
// físicas huérfanas en el pool.
func TestPermanentlyDeletingATrashedDirectoryLeavesNoOrphanFolderOnDisk(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	file, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Proyectos", Name: "notas.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, file.ID); err != nil {
		t.Fatalf("Delete del archivo falló: %v", err)
	}
	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory falló: %v", err)
	}

	if err := env.svc.PermanentlyDeleteFile(ctx, owner, file.ID); err != nil {
		t.Fatalf("PermanentlyDeleteFile falló: %v", err)
	}
	if err := env.svc.PermanentlyDeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("PermanentlyDeleteDirectory falló: %v", err)
	}

	if _, err := os.Stat(filepath.Join(env.poolDir, owner, "Proyectos")); !os.IsNotExist(err) {
		t.Errorf("la carpeta física debería haberse retirado del pool: err = %v", err)
	}
}

func TestDeleteDirectoryRejectsNonOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	victima := env.user(t, "victima")
	atacante := env.user(t, "atacante")

	dir, err := env.svc.Mkdir(ctx, victima, "/", "Privada", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if err := env.svc.DeleteDirectory(ctx, atacante, dir.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, esperado ErrForbidden (§198 IDOR)", err)
	}
}

func TestRestoreDirectoryRecreatesPhysicalMarkerAndAllowsUploads(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory falló: %v", err)
	}
	if err := env.svc.RestoreDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("RestoreDirectory falló: %v", err)
	}

	list, err := env.svc.List(ctx, owner, "/")
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(list.Directories) != 1 || list.Directories[0].ID != dir.ID {
		t.Errorf("List(/) tras restaurar = %+v, esperado solo %s", list.Directories, dir.ID)
	}

	// El marcador físico debe haberse recreado: subir dentro no debería fallar.
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Proyectos", Name: "notas.txt", Content: bytes.NewReader([]byte("x"))}); err != nil {
		t.Errorf("subir dentro de la carpeta restaurada falló: %v", err)
	}
}

func TestMkdirRejectsNameOccupiedByTrash(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory falló: %v", err)
	}

	if _, err := env.svc.Mkdir(ctx, owner, "/", "Proyectos", ""); !errors.Is(err, ErrNameOccupiedByTrash) {
		t.Errorf("err = %v, esperado ErrNameOccupiedByTrash", err)
	}
}

func TestPermanentlyDeleteDirectoryRemovesItForGood(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	dir, err := env.svc.Mkdir(ctx, owner, "/", "Efimera", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory falló: %v", err)
	}
	if err := env.svc.PermanentlyDeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("PermanentlyDeleteDirectory falló: %v", err)
	}
	if _, err := env.directories.GetDirectoryByID(ctx, dir.ID); !errors.Is(err, ErrDirectoryNotFound) {
		t.Errorf("err = %v, esperado ErrDirectoryNotFound tras el borrado definitivo", err)
	}
}

func TestUploadCreatesVersionWhenContentChanges(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("v1"))}); err != nil {
		t.Fatalf("primer Upload falló: %v", err)
	}
	second, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("versión dos"))})
	if err != nil {
		t.Fatalf("segundo Upload falló: %v", err)
	}

	versions, err := env.svc.ListVersions(ctx, owner, second.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 1 || versions[0].VersionNum != 1 {
		t.Fatalf("versions = %+v, esperado exactamente la versión 1", versions)
	}

	_, rc, err := env.svc.DownloadVersion(ctx, owner, second.ID, 1)
	if err != nil {
		t.Fatalf("DownloadVersion falló: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "v1" {
		t.Errorf("contenido de la versión 1 = %q, esperado v1", got)
	}

	// El contenido actual sigue siendo el de la segunda subida.
	_, rc, err = env.svc.Download(ctx, owner, second.ID)
	if err != nil {
		t.Fatalf("Download falló: %v", err)
	}
	got, _ = io.ReadAll(rc)
	rc.Close()
	if string(got) != "versión dos" {
		t.Errorf("contenido actual = %q, esperado 'versión dos'", got)
	}
}

func TestUploadSkipsVersionWhenContentIdentical(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	first, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("igual"))})
	if err != nil {
		t.Fatalf("primer Upload falló: %v", err)
	}
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("igual"))}); err != nil {
		t.Fatalf("segundo Upload falló: %v", err)
	}

	versions, err := env.svc.ListVersions(ctx, owner, first.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("volver a subir contenido idéntico no debería crear versión: %+v", versions)
	}
}

func TestUploadSkipsVersioningWhenDisabled(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, false, 10, 0, 0, true, true)
	owner := env.user(t, "user-1")

	first, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("v1"))})
	if err != nil {
		t.Fatalf("primer Upload falló: %v", err)
	}
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("v2"))}); err != nil {
		t.Fatalf("segundo Upload falló: %v", err)
	}

	versions, err := env.svc.ListVersions(ctx, owner, first.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("con versioning.enabled=false no debería crearse historial: %+v", versions)
	}
}

func TestRestoreVersionSwapsContentAndKeepsHistory(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("original"))}); err != nil {
		t.Fatalf("primer Upload falló: %v", err)
	}
	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("editado"))})
	if err != nil {
		t.Fatalf("segundo Upload falló: %v", err)
	}

	restored, err := env.svc.RestoreVersion(ctx, owner, meta.ID, 1)
	if err != nil {
		t.Fatalf("RestoreVersion falló: %v", err)
	}
	if restored.ID != meta.ID {
		t.Errorf("restaurar una versión no debería cambiar el ID del archivo: %q != %q", restored.ID, meta.ID)
	}

	_, rc, err := env.svc.Download(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("Download falló: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "original" {
		t.Errorf("tras restaurar, el contenido actual = %q, esperado 'original'", got)
	}

	// La versión "editado" (la que estaba vigente antes de restaurar) debe
	// haber pasado, a su vez, a formar parte del historial.
	versions, err := env.svc.ListVersions(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %+v, esperado exactamente 1 (el 'editado' recién desplazado)", versions)
	}
	_, rc, err = env.svc.DownloadVersion(ctx, owner, meta.ID, versions[0].VersionNum)
	if err != nil {
		t.Fatalf("DownloadVersion falló: %v", err)
	}
	got, _ = io.ReadAll(rc)
	rc.Close()
	if string(got) != "editado" {
		t.Errorf("la versión desplazada por el restore = %q, esperado 'editado'", got)
	}
}

func TestEnforceMaxVersionsPurgesOldest(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, true, 2, 0, 0, true, true) // como mucho 2 versiones en el historial
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("v1"))})
	if err != nil {
		t.Fatalf("upload v1 falló: %v", err)
	}
	for _, content := range []string{"v2", "v3", "v4"} {
		meta, err = env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte(content))})
		if err != nil {
			t.Fatalf("upload %s falló: %v", content, err)
		}
	}
	// Contenido actual: v4. Historial esperado, tras purgar por el límite
	// de 2: solo las dos versiones más recientes antes de v4 (v2 y v3);
	// v1 debería haberse purgado.
	versions, err := env.svc.ListVersions(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %+v, esperadas exactamente 2 (límite maxVersionsPerFile)", versions)
	}
	for _, v := range versions {
		if _, rc, err := env.svc.DownloadVersion(ctx, owner, meta.ID, v.VersionNum); err != nil {
			t.Errorf("la versión %d debería seguir descargable: %v", v.VersionNum, err)
		} else {
			got, _ := io.ReadAll(rc)
			rc.Close()
			if string(got) == "v1" {
				t.Error("v1 debería haberse purgado por exceder maxVersionsPerFile, pero sigue presente")
			}
		}
	}
}

// createSyntheticVersion inserta una fila de historial con un CreatedAt
// controlado, saltándose Upload (que siempre usa time.Now() -- no puede
// producir versiones "antiguas" a través de su API normal). Mismo criterio
// que createSyntheticCompletedJob en internal/backup para backdatar jobs.
// El contenido físico es real (mismo LocalFilesystemProvider que usa el
// servicio, mismo layout que snapshotVersion), así que la purga
// (prov.Delete) opera sobre un fichero real, no un StorageKey huérfano.
func createSyntheticVersion(t *testing.T, env *testEnv, ownerID, fileID string, versionNum int, content string, createdAt time.Time) *FileVersion {
	t.Helper()
	ctx := context.Background()
	key := versionStoragePath(ownerID, fileID, versionNum)
	size, sha, err := env.provider.Write(ctx, key, bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("escribiendo contenido sintético de versión: %v", err)
	}
	v := &FileVersion{
		ID:         idgen.New(),
		FileID:     fileID,
		VersionNum: versionNum,
		SizeBytes:  size,
		SHA256:     sha,
		MimeType:   "text/plain",
		StorageKey: key,
		CreatedAt:  createdAt,
	}
	if err := env.versions.CreateVersion(ctx, v); err != nil {
		t.Fatalf("creando versión sintética: %v", err)
	}
	return v
}

// TestEnforceMaxVersionsPrunesByAgeAlone prueba la política de antigüedad
// (MaxVersionAgeDays) en aislado -- cantidad y espacio desactivados (0).
func TestEnforceMaxVersionsPrunesByAgeAlone(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, true, 0, 5, 0, true, true) // solo antigüedad: 5 días
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("actual"))})
	if err != nil {
		t.Fatalf("upload falló: %v", err)
	}
	now := time.Now().UTC()
	createSyntheticVersion(t, env, owner, meta.ID, 1, "vieja", now.AddDate(0, 0, -10))
	createSyntheticVersion(t, env, owner, meta.ID, 2, "reciente", now.AddDate(0, 0, -1))

	if err := env.svc.enforceMaxVersions(ctx, meta.PoolID, meta.ID); err != nil {
		t.Fatalf("enforceMaxVersions falló: %v", err)
	}
	versions, err := env.svc.ListVersions(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 1 || versions[0].VersionNum != 2 {
		t.Fatalf("versions = %+v, esperada solo la versión 2 (la de 10 días debía podarse por antigüedad)", versions)
	}
}

// TestEnforceMaxVersionsPrunesBySizeAlone prueba la política de espacio
// total (MaxVersionsTotalSizeBytes) en aislado -- cantidad y antigüedad
// desactivadas (0).
func TestEnforceMaxVersionsPrunesBySizeAlone(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, true, 0, 0, 12, true, true) // solo espacio: 12 bytes totales
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("actual"))})
	if err != nil {
		t.Fatalf("upload falló: %v", err)
	}
	now := time.Now().UTC()
	createSyntheticVersion(t, env, owner, meta.ID, 1, "0123456789", now) // 10 bytes, más antigua (version_num menor)
	createSyntheticVersion(t, env, owner, meta.ID, 2, "abcde", now)      // 5 bytes, más nueva

	if err := env.svc.enforceMaxVersions(ctx, meta.PoolID, meta.ID); err != nil {
		t.Fatalf("enforceMaxVersions falló: %v", err)
	}
	versions, err := env.svc.ListVersions(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	// Recorrido nuevo->antiguo: v2 (5 bytes, acumulado=5, cabe) sobrevive;
	// v1 (10 bytes, acumulado=15>12) se poda para hacer sitio.
	if len(versions) != 1 || versions[0].VersionNum != 2 {
		t.Fatalf("versions = %+v, esperada solo la versión 2 (la de 10 bytes debía podarse por espacio)", versions)
	}
}

// TestEnforceMaxVersionsAgePrunesEvenWhenWellWithinSizeLimit confirma "el
// más restrictivo gana" (ADR-024) en un sentido: con un presupuesto de
// espacio generosísimo (nada se podría por tamaño), la antigüedad debe
// seguir podando lo que lleve más tiempo del permitido.
func TestEnforceMaxVersionsAgePrunesEvenWhenWellWithinSizeLimit(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, true, 0, 5, 1024*1024, true, true) // antigüedad estricta, espacio amplísimo
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("actual"))})
	if err != nil {
		t.Fatalf("upload falló: %v", err)
	}
	now := time.Now().UTC()
	createSyntheticVersion(t, env, owner, meta.ID, 1, "vieja", now.AddDate(0, 0, -10))

	if err := env.svc.enforceMaxVersions(ctx, meta.PoolID, meta.ID); err != nil {
		t.Fatalf("enforceMaxVersions falló: %v", err)
	}
	versions, err := env.svc.ListVersions(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("versions = %+v, esperado que la versión de 10 días se podara por antigüedad pese al espacio de sobra", versions)
	}
}

// TestEnforceMaxVersionsSizePrunesEvenWhenWellWithinAgeLimit es la otra
// mitad: con una antigüedad máxima generosísima (nada se podría por
// antigüedad), el límite de espacio debe seguir podando lo que sobre para
// caber en el presupuesto, aunque sea reciente.
func TestEnforceMaxVersionsSizePrunesEvenWhenWellWithinAgeLimit(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, true, 0, 3650, 12, true, true) // antigüedad amplísima (10 años), espacio estricto
	owner := env.user(t, "user-1")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("actual"))})
	if err != nil {
		t.Fatalf("upload falló: %v", err)
	}
	now := time.Now().UTC()
	createSyntheticVersion(t, env, owner, meta.ID, 1, "0123456789", now) // 10 bytes, recién creada -- pero es la más antigua de las dos
	createSyntheticVersion(t, env, owner, meta.ID, 2, "abcde", now)      // 5 bytes, recién creada

	if err := env.svc.enforceMaxVersions(ctx, meta.PoolID, meta.ID); err != nil {
		t.Fatalf("enforceMaxVersions falló: %v", err)
	}
	versions, err := env.svc.ListVersions(ctx, owner, meta.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 1 || versions[0].VersionNum != 2 {
		t.Fatalf("versions = %+v, esperada solo la versión 2 (la de 10 bytes debía podarse por espacio pese a ser reciente)", versions)
	}
}

func TestVersionOperationsRejectNonOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	victima := env.user(t, "victima")
	atacante := env.user(t, "atacante")

	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: victima, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("v1"))}); err != nil {
		t.Fatalf("primer Upload falló: %v", err)
	}
	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: victima, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("v2"))})
	if err != nil {
		t.Fatalf("segundo Upload falló: %v", err)
	}

	if _, err := env.svc.ListVersions(ctx, atacante, meta.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListVersions: err = %v, esperado ErrForbidden", err)
	}
	if _, _, err := env.svc.DownloadVersion(ctx, atacante, meta.ID, 1); !errors.Is(err, ErrForbidden) {
		t.Errorf("DownloadVersion: err = %v, esperado ErrForbidden", err)
	}
	if _, err := env.svc.RestoreVersion(ctx, atacante, meta.ID, 1); !errors.Is(err, ErrForbidden) {
		t.Errorf("RestoreVersion: err = %v, esperado ErrForbidden (§198 IDOR)", err)
	}
}

func TestPermanentlyDeleteFilePurgesVersionHistory(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "user-1")

	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("v1"))}); err != nil {
		t.Fatalf("primer Upload falló: %v", err)
	}
	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("v2"))})
	if err != nil {
		t.Fatalf("segundo Upload falló: %v", err)
	}

	if err := env.svc.PermanentlyDeleteFile(ctx, owner, meta.ID); err != nil {
		t.Fatalf("PermanentlyDeleteFile falló: %v", err)
	}
	versions, err := env.versions.ListVersions(ctx, meta.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("el historial debería haberse purgado junto con el archivo: %+v", versions)
	}
}
