package webdav

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

// fsEnv monta un FileService REAL sobre sqlite (con papelera y versionado, los
// valores por defecto del producto) más repositorio de auditoría y servicio
// de tokens -- no hay dobles: lo que se prueba es la integración del adaptador
// con las reglas reales de almacenamiento.
type fsEnv struct {
	files     *storage.FileService
	users     users.Repository
	auditRepo audit.Repository
	rec       *audit.Recorder
	tokens    *TokenService
}

func newFSEnv(t *testing.T, trashEnabled bool) *fsEnv {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "webdav-fs-test.db")

	sqlDB, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open falló: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(cfg, sqlDB); err != nil {
		t.Fatalf("db.Migrate falló: %v", err)
	}
	conn := db.Wrap("sqlite", sqlDB)

	pools := storage.NewSQLPoolRepository(conn)
	if _, err := storage.EnsureDefaultPool(context.Background(), pools, t.TempDir()); err != nil {
		t.Fatalf("EnsureDefaultPool falló: %v", err)
	}
	files := storage.NewFileService(
		storage.NewSQLFileRepository(conn), storage.NewSQLDirectoryRepository(conn),
		storage.NewSQLVersionRepository(conn), storage.NewSQLShareRepository(conn),
		pools, storage.NewPoolProviderResolver(pools),
		auth.NewHasher(config.Argon2Config{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}),
		trashEnabled, true, 10, 0, 0, true, true)

	userRepo := users.NewSQLRepository(conn)
	auditRepo := audit.NewSQLRepository(conn)
	return &fsEnv{
		files:     files,
		users:     userRepo,
		auditRepo: auditRepo,
		rec:       audit.NewRecorder(auditRepo, nil),
		tokens:    NewTokenService(NewSQLTokenRepository(conn), userRepo, nil),
	}
}

func (e *fsEnv) user(t *testing.T, username string) *users.User {
	t.Helper()
	u, err := users.NewService(e.users).CreateUser(context.Background(), users.CreateUserInput{
		Username: username, PasswordHash: "hash-de-prueba",
	})
	if err != nil {
		t.Fatalf("CreateUser(%s) falló: %v", username, err)
	}
	return u
}

func (e *fsEnv) fs(u *users.User) *fileSystem { return newFileSystem(e.files, u, e.rec) }

const writeFlags = os.O_RDWR | os.O_CREATE | os.O_TRUNC

// putFile hace lo mismo que el handler de x/net en un PUT completo.
func putFile(t *testing.T, fs *fileSystem, ctx context.Context, name, content string) {
	t.Helper()
	f, err := fs.OpenFile(ctx, name, writeFlags, 0o666)
	if err != nil {
		t.Fatalf("OpenFile(%s) para escribir falló: %v", name, err)
	}
	if _, err := io.WriteString(f, content); err != nil {
		t.Fatalf("escribiendo %s: %v", name, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close de %s falló: %v", name, err)
	}
}

func readContent(t *testing.T, fs *fileSystem, ctx context.Context, name string) string {
	t.Helper()
	f, err := fs.OpenFile(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile(%s) para leer falló: %v", name, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("leyendo %s: %v", name, err)
	}
	return string(b)
}
