package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/dbtest"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

// newRealTestEnv monta el mismo entorno que newTestEnvFull (ver
// file_service_test.go) pero contra una base de datos REAL (mysql o
// postgres, vía dbtest.OpenReal) en vez de sqlite en un directorio
// temporal -- ADR-031. Salta el test (dbtest.OpenReal hace t.Skip) si la
// variable de entorno NEXUSCLOUD_TEST_<DRIVER>_DSN correspondiente no está
// puesta.
//
// A diferencia de newTestEnvFull, la base de datos NO es efímera: el
// mismo contenedor puede reutilizarse entre ejecuciones de
// `go test ./...`, así que cada test genera sus propios identificadores
// únicos (usernames vía idgen.New()) en vez de literales fijos como
// "user-1", para que dos ejecuciones consecutivas nunca colisionen por
// una violación UNIQUE de un run anterior que dejó datos.
func newRealTestEnv(t *testing.T, driver string) *testEnv {
	t.Helper()
	conn := dbtest.OpenReal(t, driver)

	poolDir := t.TempDir()
	pools := NewSQLPoolRepository(conn)
	if _, err := EnsureDefaultPool(context.Background(), pools, poolDir); err != nil {
		t.Fatalf("EnsureDefaultPool falló: %v", err)
	}

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
			true, true, 10, 0, 0, true, true),
		files:       files,
		directories: directories,
		versions:    versions,
		shares:      shares,
		pools:       pools,
		poolDir:     poolDir,
		provider:    provider,
		userSvc:     users.NewService(userRepo),
		userRepo:    userRepo,
	}
}

func TestMySQLCreateDirectoryIdempotentOnNameCollision(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			owner := env.user(t, "u-"+idgen.New()[:8])

			d1, err := env.svc.Mkdir(ctx, owner, "/", "Documentos", "")
			if err != nil {
				t.Fatalf("primer Mkdir: %v", err)
			}

			// Mkdir es idempotente por diseño (CreateDirectory es DO
			// NOTHING/INSERT IGNORE en conflicto, y el propio Mkdir relee la
			// fila real tras el intento) -- un segundo Mkdir con el mismo
			// padre+nombre no debe fallar, debe devolver la carpeta YA
			// EXISTENTE. Esto es lo que ejercita de verdad la rama "INSERT
			// IGNORE" de MySQL (ADR-031) en vez de "ON CONFLICT DO NOTHING".
			d2, err := env.svc.Mkdir(ctx, owner, "/", "Documentos", "")
			if err != nil {
				t.Fatalf("segundo Mkdir (mismo nombre) no debería fallar: %v", err)
			}
			if d2.ID != d1.ID {
				t.Errorf("segundo Mkdir devolvió id = %s, esperado %s (la primera carpeta)", d2.ID, d1.ID)
			}
		})
	}
}

// TestMySQLCreateDirectoryRepositoryIsIdempotent ejercita directamente
// SQLDirectoryRepository.CreateDirectory (sin pasar por FileService.Mkdir,
// que ya rechaza la colisión antes de llegar aquí) para confirmar que la
// rama "INSERT IGNORE" de MySQL (ADR-031) es de verdad un no-op ante una
// colisión de la clave natural, igual que "ON CONFLICT DO NOTHING" en
// sqlite/postgres -- el caso que el propio repositorio documenta como
// idempotente.
func TestMySQLCreateDirectoryRepositoryIsIdempotent(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			owner := env.user(t, "u-"+idgen.New()[:8])
			pool, err := env.pools.DefaultPool(ctx)
			if err != nil {
				t.Fatalf("DefaultPool: %v", err)
			}

			d1 := &Directory{ID: idgen.New(), PoolID: pool.ID, OwnerID: owner, ParentPath: "/", Name: "Fotos", CreatedAt: time.Now().UTC()}
			if err := env.directories.CreateDirectory(ctx, d1); err != nil {
				t.Fatalf("primera CreateDirectory: %v", err)
			}

			d2 := &Directory{ID: idgen.New(), PoolID: pool.ID, OwnerID: owner, ParentPath: "/", Name: "Fotos", CreatedAt: time.Now().UTC()}
			if err := env.directories.CreateDirectory(ctx, d2); err != nil {
				t.Fatalf("segunda CreateDirectory (colisión) no debería devolver error: %v", err)
			}

			got, err := env.directories.GetDirectoryByNaturalKey(ctx, pool.ID, owner, "/", "Fotos")
			if err != nil {
				t.Fatalf("releyendo la carpeta: %v", err)
			}
			if got.ID != d1.ID {
				t.Errorf("tras la colisión debería seguir existiendo la PRIMERA carpeta: id = %s, esperado %s", got.ID, d1.ID)
			}
		})
	}
}

func TestMySQLUpsertFileSameAndDifferentContent(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			owner := env.user(t, "u-"+idgen.New()[:8])

			first, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "notas.txt", Content: bytes.NewReader([]byte("v1"))})
			if err != nil {
				t.Fatalf("primera subida: %v", err)
			}

			// Mismo contenido: no debería crear versión ni cambiar el id.
			same, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "notas.txt", Content: bytes.NewReader([]byte("v1"))})
			if err != nil {
				t.Fatalf("segunda subida (mismo contenido): %v", err)
			}
			if same.ID != first.ID {
				t.Errorf("id tras subir el mismo contenido = %s, esperado %s", same.ID, first.ID)
			}

			// Contenido distinto: DO UPDATE/ON DUPLICATE KEY UPDATE debe
			// actualizar la fila EXISTENTE (mismo id), no duplicarla.
			updated, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "notas.txt", Content: bytes.NewReader([]byte("contenido bastante más largo v2"))})
			if err != nil {
				t.Fatalf("tercera subida (contenido distinto): %v", err)
			}
			if updated.ID != first.ID {
				t.Errorf("id tras subir contenido distinto = %s, esperado %s (misma fila actualizada)", updated.ID, first.ID)
			}
			if updated.SHA256 == first.SHA256 {
				t.Errorf("el sha256 debería haber cambiado tras subir contenido distinto")
			}

			all, err := env.files.ListFiles(ctx, owner, "/")
			if err != nil {
				t.Fatalf("ListFiles: %v", err)
			}
			if len(all) != 1 {
				t.Errorf("ListFiles tras 3 subidas al mismo path = %d archivos, esperado 1 (nunca duplicar)", len(all))
			}
		})
	}
}

func TestMySQLMoveDirectoryTreeRewritesNestedAndAccentedDescendants(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			owner := env.user(t, "u-"+idgen.New()[:8])

			if _, err := env.svc.Mkdir(ctx, owner, "/", "Documentos", ""); err != nil {
				t.Fatalf("Mkdir /Documentos: %v", err)
			}
			// Nombre con acentos y una ñ -- el caso real que ejercita que
			// CONCAT/SUBSTR (rama mysql de MoveDirectoryTree) cuenten
			// CARACTERES y no bytes, igual que ||/substr en sqlite/postgres.
			if _, err := env.svc.Mkdir(ctx, owner, "/Documentos", "áéíóú-ñ", ""); err != nil {
				t.Fatalf("Mkdir /Documentos/áéíóú-ñ: %v", err)
			}
			if _, err := env.svc.Mkdir(ctx, owner, "/Documentos/áéíóú-ñ", "nieta", ""); err != nil {
				t.Fatalf("Mkdir /Documentos/áéíóú-ñ/nieta: %v", err)
			}
			if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Documentos/áéíóú-ñ", Name: "informe.txt", Content: bytes.NewReader([]byte("contenido"))}); err != nil {
				t.Fatalf("subiendo /Documentos/áéíóú-ñ/informe.txt: %v", err)
			}

			root, err := env.directories.GetDirectoryByNaturalKey(ctx, mustDefaultPoolID(ctx, t, env), owner, "/", "Documentos")
			if err != nil {
				t.Fatalf("releyendo /Documentos: %v", err)
			}
			newName := "Renombrada-ñ"
			if _, err := env.svc.MoveDirectory(ctx, owner, root.ID, nil, &newName); err != nil {
				t.Fatalf("MoveDirectory: %v", err)
			}

			// Los 3 descendientes (subcarpeta con acentos, nieta, y el
			// archivo) deben verse ahora bajo el prefijo nuevo.
			if _, err := env.directories.GetDirectoryByNaturalKey(ctx, root.PoolID, owner, "/Renombrada-ñ", "áéíóú-ñ"); err != nil {
				t.Errorf("subcarpeta con acentos no se movió: %v", err)
			}
			if _, err := env.directories.GetDirectoryByNaturalKey(ctx, root.PoolID, owner, "/Renombrada-ñ/áéíóú-ñ", "nieta"); err != nil {
				t.Errorf("nieta no se movió: %v", err)
			}
			if _, err := env.files.GetFileByNaturalKey(ctx, root.PoolID, owner, "/Renombrada-ñ/áéíóú-ñ", "informe.txt"); err != nil {
				t.Errorf("archivo descendiente no se movió: %v", err)
			}
		})
	}
}

// TestMySQLShareFlagsRoundTrip cubre las cuatro combinaciones de
// can_download/can_upload. Son columnas INTEGER en los tres motores, y el
// driver de PostgreSQL (pgx) no acepta un bool de Go como argumento de un int4
// ("unable to encode true into binary format for int4"): crear cualquier share
// fallaba ahí, y ninguna otra prueba lo pillaba porque ninguna creaba shares
// contra un motor real.
func TestMySQLShareFlagsRoundTrip(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			suffix := idgen.New()[:8]
			owner := env.user(t, "o-"+suffix)
			target := env.user(t, "t-"+suffix)
			dir, err := env.svc.Mkdir(ctx, owner, "/", "Compartida", "")
			if err != nil {
				t.Fatalf("Mkdir: %v", err)
			}

			for _, flags := range []struct{ download, upload bool }{{true, false}, {true, true}, {false, true}, {false, false}} {
				now := time.Now().UTC()
				share := &Share{
					ID: idgen.New(), OwnerID: owner, DirectoryID: dir.ID, Type: ShareTypeUser, TargetUserID: target,
					CanDownload: flags.download, CanUpload: flags.upload, CreatedAt: now, UpdatedAt: now,
				}
				if err := env.shares.CreateShare(ctx, share); err != nil {
					t.Fatalf("CreateShare(download=%v, upload=%v): %v", flags.download, flags.upload, err)
				}
				got, err := env.shares.GetShareByID(ctx, share.ID)
				if err != nil {
					t.Fatalf("GetShareByID: %v", err)
				}
				if got.CanDownload != flags.download || got.CanUpload != flags.upload {
					t.Errorf("leído download=%v upload=%v, esperado %v/%v", got.CanDownload, got.CanUpload, flags.download, flags.upload)
				}
			}
		})
	}
}

// TestMySQLPublicLinkShareLifecycle recorre un enlace público de punta a punta
// en MySQL y PostgreSQL reales: creación con contraseña, caducidad y límite de
// descargas; acceso por token; contador de descargas; agotamiento y
// revocación. Es la única superficie de Sharing sin sesión, y sus columnas
// (max_downloads, expires_at, password_hash...) tienen tipos que cada motor
// trata a su manera (ver también TestMySQLShareFlagsRoundTrip).
func TestMySQLPublicLinkShareLifecycle(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			owner := env.user(t, "o-"+idgen.New()[:8])
			meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "enlace.txt", Content: bytes.NewReader([]byte("contenido del enlace"))})
			if err != nil {
				t.Fatalf("subiendo el archivo a compartir: %v", err)
			}

			expires, maxDownloads := time.Now().UTC().Add(time.Hour), 1
			share, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
				ResourceID: meta.ID, Type: ShareTypeLink, CanDownload: true,
				Password: "clave-del-enlace", ExpiresAt: &expires, MaxDownloads: &maxDownloads,
			})
			if err != nil || token == "" {
				t.Fatalf("CreateShare(enlace) = token %q, err %v", token, err)
			}

			stored, err := env.shares.GetShareByID(ctx, share.ID)
			if err != nil {
				t.Fatalf("GetShareByID: %v", err)
			}
			if stored.MaxDownloads == nil || *stored.MaxDownloads != 1 || !stored.HasPassword() || stored.ExpiresAt == nil || stored.DownloadCount != 0 {
				t.Errorf("share leído = %+v, esperado máximo 1 descarga, con contraseña, caducidad y contador a 0", stored)
			}

			if _, _, err := env.svc.DownloadViaPublicShare(ctx, token, "mala", ""); !errors.Is(err, ErrSharePasswordIncorrect) {
				t.Fatalf("con una contraseña incorrecta: err = %v, esperado ErrSharePasswordIncorrect", err)
			}
			_, rc, err := env.svc.DownloadViaPublicShare(ctx, token, "clave-del-enlace", "")
			if err != nil {
				t.Fatalf("descarga con la contraseña correcta: %v", err)
			}
			body, _ := io.ReadAll(rc)
			rc.Close()
			if string(body) != "contenido del enlace" {
				t.Errorf("contenido descargado = %q", body)
			}
			if _, _, err := env.svc.DownloadViaPublicShare(ctx, token, "clave-del-enlace", ""); !errors.Is(err, ErrShareExhausted) {
				t.Fatalf("segunda descarga con máximo 1: err = %v, esperado ErrShareExhausted", err)
			}

			if err := env.svc.RevokeShare(ctx, owner, share.ID); err != nil {
				t.Fatalf("RevokeShare: %v", err)
			}
			if _, _, err := env.svc.DownloadViaPublicShare(ctx, token, "clave-del-enlace", ""); !errors.Is(err, ErrShareRevoked) {
				t.Errorf("tras revocar: err = %v, esperado ErrShareRevoked", err)
			}
		})
	}
}

func mustDefaultPoolID(ctx context.Context, t *testing.T, env *testEnv) string {
	t.Helper()
	pool, err := env.pools.DefaultPool(ctx)
	if err != nil {
		t.Fatalf("DefaultPool: %v", err)
	}
	return pool.ID
}
