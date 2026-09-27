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
		conn:        conn,
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

// TestMySQLSharedUploadGrantsOnRealEngines ejercita en MySQL y PostgreSQL
// reales la consulta de permisos de subida a carpetas compartidas (ADR-035):
// la subconsulta sobre user_groups, la comparación de expires_at (TEXT en los
// tres motores) y las banderas can_upload/can_download tienen que comportarse
// igual que en sqlite; y la subida de extremo a extremo no debe sobrescribir.
func TestMySQLSharedUploadGrantsOnRealEngines(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			suffix := idgen.New()[:8]
			owner := env.user(t, "o-"+suffix)
			member := env.user(t, "m-"+suffix)
			stranger := env.user(t, "x-"+suffix)
			team := env.group(t, "g-"+suffix)
			env.addToGroup(t, member, team)
			dir := mkFolder(t, env, owner, "/", "Entregas")

			grantsOf := func(user string) []*Share {
				t.Helper()
				got, err := env.shares.ListUploadSharesForDirectory(ctx, user, dir.ID, time.Now().UTC())
				if err != nil {
					t.Fatalf("ListUploadSharesForDirectory(%s): %v", user, err)
				}
				return got
			}
			wantGrants := func(step, user string, want ...string) {
				t.Helper()
				got := grantsOf(user)
				ids := make(map[string]bool, len(got))
				for _, s := range got {
					ids[s.ID] = true
				}
				if len(got) != len(want) {
					t.Fatalf("%s: %d permisos de subida, esperados %d", step, len(got), len(want))
				}
				for _, id := range want {
					if !ids[id] {
						t.Fatalf("%s: falta el permiso %s entre %v", step, id, ids)
					}
				}
			}

			// Un permiso de solo lectura no es un permiso de subida.
			readOnly := grant(t, env, owner, dir, member, "", false, nil)
			wantGrants("solo lectura", member)

			// Permiso de grupo con límite: lo recibe el miembro (por la
			// subconsulta a user_groups) y no un ajeno; el límite viaja intacto.
			viaGroup := grant(t, env, owner, dir, "", team, true, ptrInt64(1024))
			wantGrants("por grupo", member, viaGroup.ID)
			wantGrants("por grupo, ajeno", stranger)
			if got := grantsOf(member); got[0].MaxUploadSizeBytes == nil || *got[0].MaxUploadSizeBytes != 1024 {
				t.Errorf("el límite del permiso de grupo no se conservó: %v", got[0].MaxUploadSizeBytes)
			}

			// Permiso directo sin límite (NULL) además del de grupo.
			direct := grant(t, env, owner, dir, member, "", true, nil)
			wantGrants("directo + grupo", member, viaGroup.ID, direct.ID)
			for _, s := range grantsOf(member) {
				if s.ID == direct.ID && s.MaxUploadSizeBytes != nil {
					t.Errorf("el permiso sin límite debería leerse como NULL, no %d", *s.MaxUploadSizeBytes)
				}
			}

			// can_download/can_upload son INTEGER en los tres motores (PostgreSQL
			// no acepta un bool de Go como argumento ahí): los permisos tienen
			// que sobrevivir al viaje de ida y vuelta por la base de datos.
			for _, tc := range []struct {
				share      *Share
				wantUpload bool
			}{{readOnly, false}, {direct, true}} {
				got, err := env.shares.GetShareByID(ctx, tc.share.ID)
				if err != nil {
					t.Fatalf("GetShareByID(%s): %v", tc.share.ID, err)
				}
				if !got.CanDownload || got.CanUpload != tc.wantUpload {
					t.Errorf("share %s leído con can_download=%v can_upload=%v, esperado true/%v", got.ID, got.CanDownload, got.CanUpload, tc.wantUpload)
				}
			}
			if ok, err := env.shares.HasDirectoryAccess(ctx, member, dir.ID, time.Now().UTC()); err != nil || !ok {
				t.Errorf("HasDirectoryAccess(miembro) = %v, %v; esperado true", ok, err)
			}
			if received, err := env.shares.ListSharesForUser(ctx, member, time.Now().UTC()); err != nil || len(received) != 3 {
				t.Errorf("ListSharesForUser(miembro) = %d shares, %v; esperados 3 (solo lectura, grupo y directo)", len(received), err)
			}

			// Salir del grupo quita solo el permiso que venía por el grupo.
			env.removeFromGroup(t, member, team)
			wantGrants("tras salir del grupo", member, direct.ID)

			// Un permiso caducado no cuenta; uno con caducidad futura sí (la
			// comparación de expires_at es de texto en los tres motores).
			past, future := time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour)
			if err := env.shares.CreateShare(ctx, &Share{
				ID: idgen.New(), OwnerID: owner, DirectoryID: dir.ID, Type: ShareTypeUser, TargetUserID: member,
				CanDownload: true, CanUpload: true, ExpiresAt: &past, CreatedAt: past, UpdatedAt: past,
			}); err != nil {
				t.Fatalf("creando el permiso caducado: %v", err)
			}
			wantGrants("con uno caducado", member, direct.ID)
			notYet := idgen.New()
			if err := env.shares.CreateShare(ctx, &Share{
				ID: notYet, OwnerID: owner, DirectoryID: dir.ID, Type: ShareTypeUser, TargetUserID: member,
				CanDownload: true, CanUpload: true, ExpiresAt: &future, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatalf("creando el permiso con caducidad futura: %v", err)
			}
			wantGrants("con uno que aún no caduca", member, direct.ID, notYet)

			// Revocar el directo deja solo el de caducidad futura.
			if err := env.svc.RevokeShare(ctx, owner, direct.ID); err != nil {
				t.Fatalf("RevokeShare: %v", err)
			}
			wantGrants("tras revocar", member, notYet)

			// De extremo a extremo: sube, queda en el árbol del propietario y
			// una segunda subida con el mismo nombre no lo sobrescribe.
			meta, shareID, err := sharedUpload(env, member, dir, "entrega.txt", "primera")
			if err != nil {
				t.Fatalf("subida a la carpeta compartida: %v", err)
			}
			if shareID != notYet {
				t.Errorf("share usado = %s, esperado %s", shareID, notYet)
			}
			if meta.OwnerID != owner {
				t.Errorf("el archivo pertenece a %s, esperado el propietario %s", meta.OwnerID, owner)
			}
			if _, _, err := sharedUpload(env, member, dir, "entrega.txt", "segunda"); !errors.Is(err, ErrDestinationOccupied) {
				t.Errorf("segunda subida con el mismo nombre: err = %v, esperado ErrDestinationOccupied", err)
			}
			stored, err := env.files.GetFileByNaturalKey(ctx, mustDefaultPoolID(ctx, t, env), owner, "/Entregas", "entrega.txt")
			if err != nil {
				t.Fatalf("releyendo el archivo subido: %v", err)
			}
			if stored.SizeBytes != int64(len("primera")) {
				t.Errorf("el contenido original cambió: %d bytes, esperados %d", stored.SizeBytes, len("primera"))
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

// TestMySQLUsageAndQuotaOnRealEngines (ADR-036) ejercita contra MySQL y
// PostgreSQL reales lo que sqlite no puede ver: las sumas de tamaño (SUM
// devuelve DECIMAL/NUMERIC y hay que devolverlo como entero) con valores de más
// de 2 GiB -- que solo caben desde la migración 0011 --, el reparto entre
// activos, papelera y versiones, el JOIN con file_versions y la comprobación
// de cuota de extremo a extremo con el motor real.
func TestMySQLUsageAndQuotaOnRealEngines(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			env := newRealTestEnv(t, driver)
			env.withQuotas()
			suffix := idgen.New()[:8]
			owner := env.user(t, "cuota-"+suffix)
			other := env.user(t, "otro-"+suffix)
			pool, err := env.pools.DefaultPool(ctx)
			if err != nil {
				t.Fatalf("DefaultPool: %v", err)
			}

			const gib = int64(1) << 30
			now := time.Now().UTC()
			register := func(ownerID, name string, size int64) *FileMeta {
				t.Helper()
				m := &FileMeta{
					ID: idgen.New(), PoolID: pool.ID, OwnerID: ownerID, ParentPath: "/", Name: name,
					SizeBytes: size, SHA256: "h-" + name, MimeType: "application/octet-stream", CreatedAt: now, UpdatedAt: now,
				}
				if err := env.files.UpsertFile(ctx, m); err != nil {
					t.Fatalf("registrando %s de %d bytes: %v", name, size, err)
				}
				return m
			}
			big := register(owner, "enorme.bin", 5*gib)
			trashed := register(owner, "borrado.bin", 3*gib)
			if err := env.files.SoftDeleteFile(ctx, trashed.ID, now); err != nil {
				t.Fatalf("SoftDeleteFile: %v", err)
			}
			if err := env.versions.CreateVersion(ctx, &FileVersion{
				ID: idgen.New(), FileID: big.ID, VersionNum: 1, SizeBytes: gib, SHA256: "v", MimeType: "application/octet-stream",
				StorageKey: "k-" + suffix, CreatedAt: now,
			}); err != nil {
				t.Fatalf("CreateVersion: %v", err)
			}
			register(other, "ajeno.bin", 7)

			want := Usage{FilesBytes: 5 * gib, TrashBytes: 3 * gib, VersionsBytes: gib}
			if got := mustUsage(t, env, owner); got != want {
				t.Errorf("Usage = %+v, esperado %+v", got, want)
			}
			all, err := NewSQLUsageRepository(env.conn).AllOwnersUsage(ctx)
			if err != nil {
				t.Fatalf("AllOwnersUsage: %v", err)
			}
			if all[owner] != want || all[other] != (Usage{FilesBytes: 7}) {
				t.Errorf("AllOwnersUsage[owner] = %+v, [other] = %+v", all[owner], all[other])
			}

			// Cuota de más de 2 GiB de extremo a extremo (usuario -> resolver -> Upload).
			env.setQuota(t, owner, 10*gib)
			if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "cabe.txt", Content: bytes.NewReader(blob(1, 1024))}); err != nil {
				t.Errorf("con 9 GiB usados de 10 GiB, 1 KiB debe caber: %v", err)
			}
			env.setQuota(t, owner, 9*gib+512)
			if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "no-cabe.txt", Content: bytes.NewReader(blob(2, 1024))}); !errors.Is(err, ErrQuotaExceeded) {
				t.Errorf("quedan 511 bytes y se suben 1024 = %v, esperado ErrQuotaExceeded", err)
			}
		})
	}
}
