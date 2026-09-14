package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// backupTestEnv monta una BD sqlite real (migrada) + un FileService real
// sobre un directorio temporal -- mismo criterio que internal/storage/
// file_service_test.go (nunca mocks): el Backup Manager se prueba leyendo a
// través del mismo FileService/Provider real que usa el resto de la app.
type backupTestEnv struct {
	t          *testing.T
	svc        *storage.FileService
	files      storage.FileRepository
	pools      storage.PoolRepository
	resolver   storage.ProviderResolver
	backupRepo Repository
	manager    *Manager
	userSvc    *users.Service
	poolDir    string // ruta física del pool por defecto (para corromper bytes a mano en los tests)
	destDir    string
}

func newBackupTestEnv(t *testing.T) *backupTestEnv {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "backup-test.db")

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
	pools := storage.NewSQLPoolRepository(conn)
	if _, err := storage.EnsureDefaultPool(context.Background(), pools, poolDir); err != nil {
		t.Fatalf("EnsureDefaultPool falló: %v", err)
	}
	resolver := storage.NewPoolProviderResolver(pools)
	files := storage.NewSQLFileRepository(conn)
	directories := storage.NewSQLDirectoryRepository(conn)
	versions := storage.NewSQLVersionRepository(conn)
	shares := storage.NewSQLShareRepository(conn)
	userRepo := users.NewSQLRepository(conn)

	svc := storage.NewFileService(files, directories, versions, shares, pools, resolver,
		&testPasswordHasher{}, true, true, 10, 0, 0, true, true)
	backupRepo := NewSQLRepository(conn)

	return &backupTestEnv{
		t: t, svc: svc, files: files, pools: pools, resolver: resolver,
		backupRepo: backupRepo,
		manager:    NewManager(pools, files, resolver, backupRepo, svc, cfg.Security.Argon2),
		userSvc:    users.NewService(userRepo),
		poolDir:    poolDir,
		destDir:    t.TempDir(),
	}
}

func (e *backupTestEnv) user(username string) string {
	e.t.Helper()
	u, err := e.userSvc.CreateUser(context.Background(), users.CreateUserInput{Username: username, PasswordHash: "hash-de-prueba"})
	if err != nil {
		e.t.Fatalf("creando usuario de prueba %q: %v", username, err)
	}
	return u.ID
}

func (e *backupTestEnv) upload(ownerID, parentPath, name, content string) *storage.FileMeta {
	e.t.Helper()
	meta, err := e.svc.Upload(context.Background(), storage.UploadInput{
		OwnerID: ownerID, ParentPath: parentPath, Name: name, Content: bytes.NewReader([]byte(content)),
	})
	if err != nil {
		e.t.Fatalf("Upload(%s) falló: %v", name, err)
	}
	return meta
}

func (e *backupTestEnv) defaultPoolID() string {
	e.t.Helper()
	p, err := e.pools.DefaultPool(context.Background())
	if err != nil {
		e.t.Fatalf("DefaultPool falló: %v", err)
	}
	return p.ID
}

// newPool crea un segundo Storage Pool local real, con su propia carpeta
// física, y le fija backupPolicy si no está vacía.
func (e *backupTestEnv) newPool(name, backupPolicy string) *storage.Pool {
	e.t.Helper()
	p := &storage.Pool{
		ID: idgen.New(), Name: name, Type: "local", Path: e.t.TempDir(),
		Status: "active", CreatedAt: time.Now().UTC(),
	}
	if err := e.pools.CreatePool(context.Background(), p); err != nil {
		e.t.Fatalf("creando pool %q: %v", name, err)
	}
	if backupPolicy != "" {
		p.BackupPolicy = backupPolicy
		if err := e.pools.UpdatePool(context.Background(), p); err != nil {
			e.t.Fatalf("fijando backup_policy de %q: %v", name, err)
		}
	}
	return p
}

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

// --- Run --------------------------------------------------------------

func TestRunBackupsMultiplePoolsMultipleOwners(t *testing.T) {
	env := newBackupTestEnv(t)
	alice := env.user("alice")
	bob := env.user("bob")
	secundario := env.newPool("secundario", "")

	m1 := env.upload(alice, "/", "uno.txt", "contenido uno")
	m2 := env.upload(bob, "/Fotos", "dos.txt", "contenido dos")
	writeFileToPool(t, env, secundario.ID, alice, "/", "tres.txt", "contenido tres")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("Status = %q, esperado %q", job.Status, StatusCompleted)
	}
	if job.FileCount != 3 {
		t.Errorf("FileCount = %d, esperado 3 (uno.txt + dos.txt + tres.txt, en 2 pools distintos)", job.FileCount)
	}

	manifest, err := readManifest(context.Background(), NewLocalDestination(env.destDir), job.ID)
	if err != nil {
		t.Fatalf("leyendo manifest.json: %v", err)
	}
	if len(manifest.Pools) != 2 {
		t.Fatalf("len(manifest.Pools) = %d, esperado 2", len(manifest.Pools))
	}
	var poolPorDefecto *PoolManifest
	for i := range manifest.Pools {
		if manifest.Pools[i].PoolID == env.defaultPoolID() {
			poolPorDefecto = &manifest.Pools[i]
		}
	}
	if poolPorDefecto == nil {
		t.Fatalf("el pool por defecto no aparece en el manifiesto: %+v", manifest.Pools)
	}
	for _, fm := range poolPorDefecto.Files {
		switch fm.Name {
		case "uno.txt":
			if fm.OwnerID != alice || fm.SHA256 != m1.SHA256 {
				t.Errorf("uno.txt en el manifiesto no coincide con lo subido: %+v", fm)
			}
		case "dos.txt":
			if fm.OwnerID != bob || fm.ParentPath != "/Fotos" || fm.SHA256 != m2.SHA256 {
				t.Errorf("dos.txt en el manifiesto no coincide con lo subido: %+v", fm)
			}
		default:
			t.Errorf("fichero inesperado en el pool por defecto: %+v", fm)
		}
	}
	if len(poolPorDefecto.Files) != 2 {
		t.Errorf("len(poolPorDefecto.Files) = %d, esperado 2", len(poolPorDefecto.Files))
	}

	gotBytes, err := os.ReadFile(filepath.Join(env.destDir, job.ID, "data", env.defaultPoolID(), alice, "uno.txt"))
	if err != nil {
		t.Fatalf("leyendo la copia de uno.txt: %v", err)
	}
	if string(gotBytes) != "contenido uno" {
		t.Errorf("contenido copiado = %q, esperado %q", gotBytes, "contenido uno")
	}
}

func TestRunExcludesPoolWithBackupOffButIncludesInherit(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("carol")
	off := env.newPool("sin-backup", storage.PolicyOff)
	heredado := env.newPool("heredado", "") // defaultPolicies() deja backup_policy=inherit

	writeFileToPool(t, env, off.ID, owner, "/", "excluido.txt", "no debe respaldarse")

	// Sube un archivo directamente al pool "heredado" usando su Provider (el
	// FileService de este test siempre usa el pool activo de mayor
	// prioridad -- para dirigir un archivo a un pool concreto sin más
	// infraestructura, se usa el propio Provider + FileRepository).
	writeFileToPool(t, env, heredado.ID, owner, "/", "incluido.txt", "sí debe respaldarse")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}
	manifest, err := readManifest(context.Background(), NewLocalDestination(env.destDir), job.ID)
	if err != nil {
		t.Fatalf("leyendo manifest.json: %v", err)
	}
	for _, pm := range manifest.Pools {
		if pm.PoolID == off.ID {
			t.Errorf("el pool con backup_policy=off no debía aparecer en el manifiesto: %+v", pm)
		}
	}
	foundIncluido := false
	for _, pm := range manifest.Pools {
		if pm.PoolID != heredado.ID {
			continue
		}
		for _, fm := range pm.Files {
			if fm.Name == "incluido.txt" {
				foundIncluido = true
			}
		}
	}
	if !foundIncluido {
		t.Errorf("un pool en backup_policy=inherit debía incluirse; manifiesto: %+v", manifest.Pools)
	}
}

func TestRunExcludesTrashedFiles(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("dave")
	env.upload(owner, "/", "activo.txt", "se queda")
	papelera := env.upload(owner, "/", "papelera.txt", "se borra")
	if err := env.svc.Delete(context.Background(), owner, papelera.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}
	manifest, _ := readManifest(context.Background(), NewLocalDestination(env.destDir), job.ID)
	names := map[string]bool{}
	for _, pm := range manifest.Pools {
		for _, fm := range pm.Files {
			names[fm.Name] = true
		}
	}
	if !names["activo.txt"] {
		t.Errorf("activo.txt debía estar en el manifiesto")
	}
	if names["papelera.txt"] {
		t.Errorf("papelera.txt está en la papelera y no debía respaldarse")
	}
}

// TestRunSucceedsWithZeroEligibleFiles cubre un caso encontrado de verdad
// vía E2E (no en la suite unitaria original): un pool recién creado o ya
// vaciado no tiene ningún efecto secundario que cree jobDir (eso ocurría
// solo como consecuencia de copyVerified al respaldar el primer fichero),
// así que writeManifest fallaba con "no such file or directory" en vez de
// completar un backup vacío pero válido.
func TestRunSucceedsWithZeroEligibleFiles(t *testing.T) {
	env := newBackupTestEnv(t)

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run con cero ficheros elegibles falló: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("Status = %q, esperado %q", job.Status, StatusCompleted)
	}
	if job.FileCount != 0 {
		t.Errorf("FileCount = %d, esperado 0", job.FileCount)
	}
	manifest, err := readManifest(context.Background(), NewLocalDestination(env.destDir), job.ID)
	if err != nil {
		t.Fatalf("leyendo manifest.json: %v", err)
	}
	if len(manifest.Pools) != 1 || len(manifest.Pools[0].Files) != 0 {
		t.Errorf("manifest.Pools = %+v, esperado 1 pool sin ficheros", manifest.Pools)
	}
}

// --- Incremental (ADR-026) ----------------------------------------------

func TestRunIncrementalWithoutPreviousBackupActsAsFull(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("jack")
	original := env.upload(owner, "/", "a.txt", "contenido")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, Incremental: true})
	if err != nil {
		t.Fatalf("Run incremental sin backup previo falló: %v", err)
	}
	if job.FileCount != 1 {
		t.Fatalf("FileCount = %d, esperado 1", job.FileCount)
	}
	restoreDir := t.TempDir()
	if err := env.manager.Restore(context.Background(), RestoreOptions{JobID: job.ID, DestinationPath: restoreDir}); err != nil {
		t.Fatalf("Restore falló: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(restoreDir, env.defaultPoolID(), owner, "a.txt"))
	if err != nil {
		t.Fatalf("leyendo el archivo restaurado: %v", err)
	}
	if sha256Hex(got) != original.SHA256 {
		t.Errorf("sha256 restaurado no coincide con el original")
	}
}

// TestRunIncrementalHardlinksUnchangedRecopiesChangedCopiesNew cubre los
// tres casos centrales de ADR-026 en un único escenario realista: dos
// backups incrementales consecutivos, con un fichero que no cambia entre
// medias, uno que sí cambia, y uno que se sube después del primer backup.
func TestRunIncrementalHardlinksUnchangedRecopiesChangedCopiesNew(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("kate")
	env.upload(owner, "/", "sin-cambios.txt", "contenido estable")
	env.upload(owner, "/", "cambia.txt", "contenido original")

	job1, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, Incremental: true})
	if err != nil {
		t.Fatalf("primer Run incremental falló: %v", err)
	}

	// Cambia el contenido de un fichero y sube uno nuevo antes del segundo
	// backup -- exactamente lo que un incremental real tiene que
	// distinguir de "sin cambios".
	env.upload(owner, "/", "cambia.txt", "contenido MODIFICADO")
	env.upload(owner, "/", "nuevo.txt", "recién llegado")

	job2, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, Incremental: true})
	if err != nil {
		t.Fatalf("segundo Run incremental falló: %v", err)
	}
	if job2.FileCount != 3 {
		t.Fatalf("FileCount del segundo job = %d, esperado 3 (el manifiesto sigue siendo completo)", job2.FileCount)
	}

	poolID := env.defaultPoolID()
	pathIn := func(jobID, name string) string {
		return filepath.Join(env.destDir, jobID, "data", poolID, owner, name)
	}
	sameInode := func(t *testing.T, pathA, pathB string) bool {
		t.Helper()
		fiA, err := os.Stat(pathA)
		if err != nil {
			t.Fatalf("Stat(%s): %v", pathA, err)
		}
		fiB, err := os.Stat(pathB)
		if err != nil {
			t.Fatalf("Stat(%s): %v", pathB, err)
		}
		return os.SameFile(fiA, fiB)
	}

	// sin-cambios.txt: el segundo job debe COMPARTIR inodo con el primero
	// (enlazado, no recopiado) -- la prueba real de que la deduplicación
	// funcionó, no solo que el contenido coincide por casualidad.
	if !sameInode(t, pathIn(job1.ID, "sin-cambios.txt"), pathIn(job2.ID, "sin-cambios.txt")) {
		t.Error("sin-cambios.txt debía compartir inodo entre ambos backups (enlazado), no recopiarse")
	}
	// cambia.txt: el segundo job debe tener un inodo DISTINTO (se
	// recopió de verdad, no se enlazó al contenido antiguo).
	if sameInode(t, pathIn(job1.ID, "cambia.txt"), pathIn(job2.ID, "cambia.txt")) {
		t.Error("cambia.txt no debía compartir inodo: su contenido cambió, tenía que recopiarse")
	}
	gotCambiado, err := os.ReadFile(pathIn(job2.ID, "cambia.txt"))
	if err != nil {
		t.Fatalf("leyendo cambia.txt del segundo backup: %v", err)
	}
	if string(gotCambiado) != "contenido MODIFICADO" {
		t.Errorf("cambia.txt en el segundo backup = %q, esperado el contenido modificado", gotCambiado)
	}
	// nuevo.txt: no existía en el primer backup -- debe existir en el
	// segundo con su propio contenido (copia normal, no hay nada que
	// enlazar).
	gotNuevo, err := os.ReadFile(pathIn(job2.ID, "nuevo.txt"))
	if err != nil {
		t.Fatalf("leyendo nuevo.txt del segundo backup: %v", err)
	}
	if string(gotNuevo) != "recién llegado" {
		t.Errorf("nuevo.txt en el segundo backup = %q, esperado el contenido subido", gotNuevo)
	}
}

// TestRunIncrementalFallsBackToCopyWhenLinkSourceMissing confirma que un
// enlace que no se puede hacer (aquí, forzado borrando a mano la copia del
// backup anterior) nunca aborta el job -- la incrementalidad es una
// optimización, no una condición de éxito (ADR-026).
func TestRunIncrementalFallsBackToCopyWhenLinkSourceMissing(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("liam")
	env.upload(owner, "/", "a.txt", "contenido")

	job1, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, Incremental: true})
	if err != nil {
		t.Fatalf("primer Run incremental falló: %v", err)
	}
	// Borra a mano la copia del primer backup -- simula que ya no está
	// disponible para enlazar (disco parcialmente dañado, movido, etc.).
	firstCopy := filepath.Join(env.destDir, job1.ID, "data", env.defaultPoolID(), owner, "a.txt")
	if err := os.Remove(firstCopy); err != nil {
		t.Fatalf("borrando la copia del primer backup: %v", err)
	}

	job2, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, Incremental: true})
	if err != nil {
		t.Fatalf("segundo Run incremental debía completarse pese al enlace fallido: %v", err)
	}
	if job2.Status != StatusCompleted || job2.FileCount != 1 {
		t.Fatalf("job2 = %+v, esperado completado con 1 fichero (recopiado tras fallar el enlace)", job2)
	}
	if _, err := os.Stat(filepath.Join(env.destDir, job2.ID, "data", env.defaultPoolID(), owner, "a.txt")); err != nil {
		t.Errorf("a.txt debía existir en el segundo backup (recopiado): %v", err)
	}
}

func TestRunWithExplicitPoolIDsLimitsScope(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("erin")
	otro := env.newPool("otro", "")
	env.upload(owner, "/", "en-default.txt", "por-defecto")
	writeFileToPool(t, env, otro.ID, owner, "/", "en-otro.txt", "otro-pool")

	job, err := env.manager.Run(context.Background(), RunOptions{
		DestinationPath: env.destDir,
		PoolIDs:         []string{env.defaultPoolID()},
	})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}
	if len(job.PoolIDs) != 1 || job.PoolIDs[0] != env.defaultPoolID() {
		t.Errorf("PoolIDs = %v, esperado solo el pool por defecto", job.PoolIDs)
	}
	manifest, _ := readManifest(context.Background(), NewLocalDestination(env.destDir), job.ID)
	for _, pm := range manifest.Pools {
		if pm.PoolID == otro.ID {
			t.Errorf("el pool no pedido explícitamente no debía aparecer: %+v", pm)
		}
	}
}

func TestRunFailsWholeJobOnHashMismatch(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("frank")
	env.upload(owner, "/", "bueno.txt", "contenido intacto")
	env.upload(owner, "/", "roto.txt", "contenido original")

	// Corrompe el contenido físico DIRECTAMENTE en disco, sin pasar por
	// FileService -- simula bitrot/corrupción del disco de origen: la fila
	// de metadatos sigue diciendo el hash antiguo, pero el byte real ya no
	// coincide.
	onDisk := filepath.Join(env.poolDir, owner, "roto.txt")
	if err := os.WriteFile(onDisk, []byte("contenido CORROMPIDO"), 0o600); err != nil {
		t.Fatalf("corrompiendo el fichero de origen: %v", err)
	}

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err == nil {
		t.Fatalf("Run debía fallar por el hash que no coincide, job=%+v", job)
	}
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Errorf("err = %v, esperado que envuelva ErrIntegrityMismatch", err)
	}

	jobs, listErr := env.manager.List(context.Background())
	if listErr != nil {
		t.Fatalf("List falló: %v", listErr)
	}
	if len(jobs) != 1 || jobs[0].Status != StatusFailed {
		t.Fatalf("jobs = %+v, esperado 1 job con status=failed", jobs)
	}
	if _, statErr := os.Stat(filepath.Join(env.destDir, jobs[0].ID, "manifest.json")); !os.IsNotExist(statErr) {
		t.Errorf("manifest.json no debía escribirse en un job fallido (err=%v)", statErr)
	}
}

// --- List ---------------------------------------------------------------

func TestListReturnsJobsOrderedMostRecentFirst(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("grace")
	env.upload(owner, "/", "a.txt", "a")
	job1, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run 1 falló: %v", err)
	}
	env.upload(owner, "/", "b.txt", "b")
	job2, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run 2 falló: %v", err)
	}

	jobs, err := env.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(jobs) != 2 || jobs[0].ID != job2.ID || jobs[1].ID != job1.ID {
		t.Fatalf("List = %+v, esperado [job2, job1] (más reciente primero)", jobs)
	}
	if jobs[0].FileCount != 2 || jobs[1].FileCount != 1 {
		t.Errorf("FileCount de cada job no coincide: job2=%d (esperado 2), job1=%d (esperado 1)", jobs[0].FileCount, jobs[1].FileCount)
	}
}

// --- Restore --------------------------------------------------------------

func TestRestoreReconstructsTreeByteForByte(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("heidi")
	original := env.upload(owner, "/Documentos", "informe.txt", "contenido del informe")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	restoreDir := t.TempDir()
	if err := env.manager.Restore(context.Background(), RestoreOptions{JobID: job.ID, DestinationPath: restoreDir}); err != nil {
		t.Fatalf("Restore falló: %v", err)
	}

	restoredPath := filepath.Join(restoreDir, env.defaultPoolID(), owner, "Documentos", "informe.txt")
	got, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("leyendo el archivo restaurado (%s): %v", restoredPath, err)
	}
	if string(got) != "contenido del informe" {
		t.Errorf("contenido restaurado = %q, esperado %q", got, "contenido del informe")
	}
	gotHash := sha256Hex(got)
	if gotHash != original.SHA256 {
		t.Errorf("sha256 restaurado = %s, esperado %s (el del original subido)", gotHash, original.SHA256)
	}
}

func TestRestoreDetectsBitrotInBackupDestination(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("ivan")
	env.upload(owner, "/", "bueno.txt", "intacto")
	env.upload(owner, "/", "afectado.txt", "contenido original")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	// Simula bitrot: corrompe la COPIA ya hecha dentro de la carpeta de
	// backup (no el origen).
	backedUpPath := filepath.Join(env.destDir, job.ID, "data", env.defaultPoolID(), owner, "afectado.txt")
	if err := os.WriteFile(backedUpPath, []byte("bytes distintos tras el backup"), 0o600); err != nil {
		t.Fatalf("corrompiendo la copia de backup: %v", err)
	}

	restoreDir := t.TempDir()
	err = env.manager.Restore(context.Background(), RestoreOptions{JobID: job.ID, DestinationPath: restoreDir})
	if err == nil {
		t.Fatal("Restore debía devolver un error señalando el fichero corrompido")
	}
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Errorf("err = %v, esperado que envuelva ErrIntegrityMismatch", err)
	}

	// El fichero bueno SÍ debe haberse restaurado pese al fallo del otro
	// (Restore continúa y acumula errores, no aborta todo el trabajo).
	if _, err := os.Stat(filepath.Join(restoreDir, env.defaultPoolID(), owner, "bueno.txt")); err != nil {
		t.Errorf("bueno.txt debía restaurarse igualmente: %v", err)
	}
	// El fichero afectado NO debe quedar a medias/corrompido en el destino.
	if _, err := os.Stat(filepath.Join(restoreDir, env.defaultPoolID(), owner, "afectado.txt")); !os.IsNotExist(err) {
		t.Errorf("afectado.txt no debía quedar en el destino de la restauración (err=%v)", err)
	}
}

// --- RestoreToPool (ADR-025) --------------------------------------------

func TestRestoreToPoolReconstructsFilesInTargetPool(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("carol")
	original := env.upload(owner, "/", "informe.txt", "contenido del informe")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	destino := env.newPool("destino-restauracion", "")
	result, err := env.manager.RestoreToPool(context.Background(), RestoreToPoolOptions{JobID: job.ID, PoolID: destino.ID})
	if err != nil {
		t.Fatalf("RestoreToPool falló: %v", err)
	}
	if result.Restored != 1 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, esperado 1 restaurado sin errores", result)
	}

	meta, err := env.files.GetFileByNaturalKey(context.Background(), destino.ID, owner, "/", "informe.txt")
	if err != nil {
		t.Fatalf("el archivo restaurado no aparece en el pool destino: %v", err)
	}
	if meta.SHA256 != original.SHA256 {
		t.Errorf("SHA256 restaurado = %s, esperado %s (el del original)", meta.SHA256, original.SHA256)
	}
	if _, rc, err := env.svc.Download(context.Background(), owner, meta.ID); err != nil {
		t.Errorf("Download del archivo restaurado falló: %v", err)
	} else {
		rc.Close()
	}
}

func TestRestoreToPoolMaterializesNestedDirectories(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("dave")
	original := env.upload(owner, "/a/b", "archivo.txt", "contenido anidado")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}
	// Simula el escenario real de esta feature (el pool original ya no
	// tiene el archivo, p.ej. se perdió) -- si se dejara activo, List (que
	// no filtra por pool: la unicidad de nombre es POR POOL, §10) mostraría
	// dos ficheros con el mismo owner+ruta, uno por pool, y el assert de
	// abajo dejaría de ser inequívoco. No es un fallo de RestoreToPool, es
	// una consecuencia del propio multi-pool ya existente.
	if err := env.svc.Delete(context.Background(), owner, original.ID); err != nil {
		t.Fatalf("Delete del original falló: %v", err)
	}
	destino := env.newPool("destino-anidado", "")
	result, err := env.manager.RestoreToPool(context.Background(), RestoreToPoolOptions{JobID: job.ID, PoolID: destino.ID})
	if err != nil {
		t.Fatalf("RestoreToPool falló: %v", err)
	}
	if result.Restored != 1 {
		t.Fatalf("result = %+v, esperado 1 restaurado", result)
	}

	// El fichero restaurado debe ser NAVEGABLE carpeta a carpeta desde la
	// raíz, no solo estar presente en la base de datos con un parent_path
	// que apunte a carpetas sin fila propia (mismo problema que ADR-012
	// punto 8 resolvió en el cliente Flutter).
	top, err := env.svc.List(context.Background(), owner, "/")
	if err != nil {
		t.Fatalf("List(/) falló: %v", err)
	}
	if len(top.Directories) != 1 || top.Directories[0].Name != "a" {
		t.Fatalf("List(/) = %+v, esperada la carpeta \"a\" navegable", top.Directories)
	}
	sub, err := env.svc.List(context.Background(), owner, "/a")
	if err != nil {
		t.Fatalf("List(/a) falló: %v", err)
	}
	if len(sub.Directories) != 1 || sub.Directories[0].Name != "b" {
		t.Fatalf("List(/a) = %+v, esperada la carpeta \"b\" navegable", sub.Directories)
	}
	nested, err := env.svc.List(context.Background(), owner, "/a/b")
	if err != nil {
		t.Fatalf("List(/a/b) falló: %v", err)
	}
	if len(nested.Files) != 1 || nested.Files[0].Name != "archivo.txt" {
		t.Fatalf("List(/a/b) = %+v, esperado archivo.txt", nested.Files)
	}
}

func TestRestoreToPoolVersionsAnAlreadyOccupiedPath(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("erin")
	env.upload(owner, "/", "doc.txt", "contenido de backup")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	// El pool destino ya tiene un archivo ACTIVO en ese mismo path, con
	// contenido distinto -- FileService.Upload (ADR-007) debe versionarlo
	// en vez de perderlo, exactamente igual que cualquier subida normal.
	destino := env.newPool("destino-conflicto", "")
	vigente, err := env.svc.Upload(context.Background(), storage.UploadInput{
		OwnerID: owner, ParentPath: "/", Name: "doc.txt", Content: bytes.NewReader([]byte("contenido ya vigente en destino")), PoolID: destino.ID,
	})
	if err != nil {
		t.Fatalf("Upload previo al pool destino falló: %v", err)
	}

	result, err := env.manager.RestoreToPool(context.Background(), RestoreToPoolOptions{JobID: job.ID, PoolID: destino.ID})
	if err != nil {
		t.Fatalf("RestoreToPool falló: %v", err)
	}
	if result.Restored != 1 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, esperado 1 restaurado sin errores", result)
	}

	versions, err := env.svc.ListVersions(context.Background(), owner, vigente.ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %+v, esperada 1 versión (el contenido que había antes de restaurar)", versions)
	}
}

func TestRestoreToPoolRejectsInactivePool(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("frank")
	env.upload(owner, "/", "a.txt", "x")
	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	inactivo := env.newPool("inactivo", "")
	if err := env.pools.SetPoolStatus(context.Background(), inactivo.ID, "disabled"); err != nil {
		t.Fatalf("desactivando el pool: %v", err)
	}

	if _, err := env.manager.RestoreToPool(context.Background(), RestoreToPoolOptions{JobID: job.ID, PoolID: inactivo.ID}); !errors.Is(err, ErrPoolNotActive) {
		t.Errorf("err = %v, esperado ErrPoolNotActive", err)
	}
	// No debe haber tocado ningún fichero: el archivo original sigue
	// siendo el único con ese nombre (ninguna versión nueva creada).
	if _, err := env.files.GetFileByNaturalKey(context.Background(), inactivo.ID, owner, "/", "a.txt"); err == nil {
		t.Error("no debía haberse escrito nada en el pool inactivo")
	}
}

func TestRestoreToPoolRejectsUnknownPool(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("grace")
	env.upload(owner, "/", "a.txt", "x")
	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}
	if _, err := env.manager.RestoreToPool(context.Background(), RestoreToPoolOptions{JobID: job.ID, PoolID: "no-existe"}); err == nil {
		t.Error("esperado un error al resolver un pool inexistente")
	}
}

func TestRestoreToPoolFailsOnlyTheFileWithUnknownOwner(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("heidi2")
	env.upload(owner, "/", "bueno.txt", "contenido bueno")
	env.upload(owner, "/", "huerfano.txt", "contenido huérfano")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	// Simula que el propietario de un fichero ya no existe (p.ej. borrado
	// tras el backup): edita el manifiesto a mano para apuntar a un ID que
	// no tiene fila en users -- viola la FK files.owner_id -> users.id
	// (§14) al intentar reinsertarlo, sin necesidad de ejercitar el borrado
	// real de un usuario (fuera de alcance de este test).
	dest := NewLocalDestination(env.destDir)
	manifest, err := readManifest(context.Background(), dest, job.ID)
	if err != nil {
		t.Fatalf("leyendo manifest.json: %v", err)
	}
	for i := range manifest.Pools {
		for j := range manifest.Pools[i].Files {
			if manifest.Pools[i].Files[j].Name == "huerfano.txt" {
				manifest.Pools[i].Files[j].OwnerID = idgen.New()
			}
		}
	}
	if err := writeManifest(context.Background(), dest, job.ID, manifest); err != nil {
		t.Fatalf("reescribiendo manifest.json: %v", err)
	}

	destino := env.newPool("destino-huerfano", "")
	result, err := env.manager.RestoreToPool(context.Background(), RestoreToPoolOptions{JobID: job.ID, PoolID: destino.ID})
	if err != nil {
		t.Fatalf("RestoreToPool falló: %v", err)
	}
	if result.Restored != 1 {
		t.Errorf("Restored = %d, esperado 1 (bueno.txt; huerfano.txt debía fallar)", result.Restored)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("Errors = %+v, esperado exactamente 1 error (huerfano.txt)", result.Errors)
	}
	if _, err := env.files.GetFileByNaturalKey(context.Background(), destino.ID, owner, "/", "bueno.txt"); err != nil {
		t.Errorf("bueno.txt debía restaurarse igualmente: %v", err)
	}
}

func TestRestoreToPoolDetectsBitrotAndContinuesWithOthers(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("ivan2")
	env.upload(owner, "/", "bueno.txt", "intacto")
	env.upload(owner, "/", "afectado.txt", "contenido original")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}
	backedUpPath := filepath.Join(env.destDir, job.ID, "data", env.defaultPoolID(), owner, "afectado.txt")
	if err := os.WriteFile(backedUpPath, []byte("bytes distintos tras el backup"), 0o600); err != nil {
		t.Fatalf("corrompiendo la copia de backup: %v", err)
	}

	destino := env.newPool("destino-bitrot", "")
	result, err := env.manager.RestoreToPool(context.Background(), RestoreToPoolOptions{JobID: job.ID, PoolID: destino.ID})
	if err != nil {
		t.Fatalf("RestoreToPool falló: %v", err)
	}
	if result.Restored != 1 {
		t.Errorf("Restored = %d, esperado 1 (bueno.txt)", result.Restored)
	}
	if len(result.Errors) != 1 || !errors.Is(result.Errors[0], ErrIntegrityMismatch) {
		t.Fatalf("Errors = %+v, esperado exactamente 1 error envolviendo ErrIntegrityMismatch", result.Errors)
	}
	if _, err := env.files.GetFileByNaturalKey(context.Background(), destino.ID, owner, "/", "afectado.txt"); err == nil {
		t.Error("afectado.txt no debía reinsertarse en el pool destino")
	}
}

// --- Verify -----------------------------------------------------------

func TestVerifyReportsOKForIntactBackup(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("quinn")
	env.upload(owner, "/", "uno.txt", "contenido uno")
	env.upload(owner, "/", "dos.txt", "contenido dos")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	result, err := env.manager.Verify(context.Background(), VerifyOptions{JobID: job.ID})
	if err != nil {
		t.Fatalf("Verify falló: %v", err)
	}
	if !result.OK {
		t.Errorf("result.OK = false, esperado true para un backup intacto: %+v", result.Files)
	}
	if len(result.Files) != 2 {
		t.Fatalf("len(result.Files) = %d, esperado 2", len(result.Files))
	}
	for _, fr := range result.Files {
		if !fr.OK || fr.Error != "" {
			t.Errorf("fichero %s no reportó OK: %+v", fr.Name, fr)
		}
	}

	// Verify es de solo lectura: no debe tocar el backup ni la BD.
	if _, err := os.Stat(filepath.Join(env.destDir, job.ID, "manifest.json")); err != nil {
		t.Errorf("manifest.json no debía tocarse: %v", err)
	}
	if job2, err := env.manager.repo.GetJobByID(context.Background(), job.ID); err != nil || job2.Status != StatusCompleted {
		t.Errorf("el job no debía cambiar de estado tras Verify: %v, %+v", err, job2)
	}
}

func TestVerifyDetectsCorruptedFileWithoutAbortingTheRest(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("rosa")
	env.upload(owner, "/", "bueno.txt", "intacto")
	env.upload(owner, "/", "afectado.txt", "contenido original")

	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}
	backedUpPath := filepath.Join(env.destDir, job.ID, "data", env.defaultPoolID(), owner, "afectado.txt")
	if err := os.WriteFile(backedUpPath, []byte("bytes distintos tras el backup"), 0o600); err != nil {
		t.Fatalf("corrompiendo la copia de backup: %v", err)
	}

	result, err := env.manager.Verify(context.Background(), VerifyOptions{JobID: job.ID})
	if err != nil {
		t.Fatalf("Verify (la llamada en sí) no debía fallar, solo reportar el problema: %v", err)
	}
	if result.OK {
		t.Fatal("result.OK = true, esperado false: hay un fichero corrompido")
	}
	var sawGood, sawBad bool
	for _, fr := range result.Files {
		switch fr.Name {
		case "bueno.txt":
			sawGood = true
			if !fr.OK {
				t.Errorf("bueno.txt debía reportar OK: %+v", fr)
			}
		case "afectado.txt":
			sawBad = true
			if fr.OK || fr.Error == "" {
				t.Errorf("afectado.txt debía reportar el error de integridad: %+v", fr)
			}
		}
	}
	if !sawGood || !sawBad {
		t.Fatalf("esperaba resultados para ambos ficheros: %+v", result.Files)
	}
}

func TestVerifyRejectsNonCompletedJob(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("sam")
	env.upload(owner, "/", "roto.txt", "original")
	onDisk := filepath.Join(env.poolDir, owner, "roto.txt")
	if err := os.WriteFile(onDisk, []byte("corrompido"), 0o600); err != nil {
		t.Fatalf("corrompiendo el fichero de origen: %v", err)
	}
	if _, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir}); err == nil {
		t.Fatal("Run debía fallar")
	}
	jobs, err := env.manager.List(context.Background())
	if err != nil || len(jobs) != 1 {
		t.Fatalf("List = %v, %+v", err, jobs)
	}

	if _, err := env.manager.Verify(context.Background(), VerifyOptions{JobID: jobs[0].ID}); !errors.Is(err, ErrJobNotRestorable) {
		t.Errorf("Verify sobre un job failed: err = %v, esperado ErrJobNotRestorable", err)
	}
}

func TestVerifyRejectsUnknownJob(t *testing.T) {
	env := newBackupTestEnv(t)
	if _, err := env.manager.Verify(context.Background(), VerifyOptions{JobID: "no-existe"}); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("err = %v, esperado ErrJobNotFound", err)
	}
}

// --- Retención (ADR-017) --------------------------------------------------

func TestRunPrunesOldBackupsBeyondRetentionCount(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("judy")

	var jobIDs []string
	for i := 0; i < 3; i++ {
		env.upload(owner, "/", "archivo.txt", fmt.Sprintf("versión %d", i))
		job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, RetentionCount: 2})
		if err != nil {
			t.Fatalf("Run %d falló: %v", i, err)
		}
		jobIDs = append(jobIDs, job.ID)
	}

	jobs, err := env.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("len(jobs) = %d, esperado 2 tras podar con RetentionCount=2: %+v", len(jobs), jobs)
	}
	if _, err := env.manager.repo.GetJobByID(context.Background(), jobIDs[0]); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("el job más antiguo (%s) debía haberse borrado de la BD, err=%v", jobIDs[0], err)
	}
	if _, err := os.Stat(filepath.Join(env.destDir, jobIDs[0])); !os.IsNotExist(err) {
		t.Errorf("la carpeta del job más antiguo (%s) debía haberse borrado del disco, err=%v", jobIDs[0], err)
	}
	for _, id := range jobIDs[1:] {
		if _, err := os.Stat(filepath.Join(env.destDir, id, "manifest.json")); err != nil {
			t.Errorf("el job %s debía conservarse intacto: %v", id, err)
		}
	}
}

func TestRunPrunesOldBackupsBeyondRetentionDays(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("noah")

	oldID := env.createSyntheticCompletedJob(env.destDir, time.Now().Add(-40*24*time.Hour))

	env.upload(owner, "/", "reciente.txt", "contenido")
	newJob, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, RetentionDays: 30})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	if _, err := env.manager.repo.GetJobByID(context.Background(), oldID); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("el job de hace 40 días debía podarse con RetentionDays=30, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(env.destDir, oldID)); !os.IsNotExist(err) {
		t.Errorf("la carpeta del job de hace 40 días debía borrarse del disco, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(env.destDir, newJob.ID, "manifest.json")); err != nil {
		t.Errorf("el job recién completado debía conservarse: %v", err)
	}
}

// TestRunRetentionDaysOverridesAggressiveCount demuestra la mitad "la edad
// salva de una cuenta agresiva" de la composición (ADR-017): con
// RetentionCount=2 puro, los jobs en las posiciones 2+ se podarían; con
// RetentionDays=30 activo A LA VEZ, como los 5 son recientes, ninguno se
// poda -- basta con que UNA política activa vote conservar.
func TestRunRetentionDaysOverridesAggressiveCount(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("olga")

	var jobIDs []string
	for i := 0; i < 5; i++ {
		env.upload(owner, "/", "archivo.txt", fmt.Sprintf("v%d", i))
		job, err := env.manager.Run(context.Background(), RunOptions{
			DestinationPath: env.destDir, RetentionCount: 2, RetentionDays: 30,
		})
		if err != nil {
			t.Fatalf("Run %d falló: %v", i, err)
		}
		jobIDs = append(jobIDs, job.ID)
	}

	jobs, err := env.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(jobs) != 5 {
		t.Errorf("len(jobs) = %d, esperado 5: RetentionDays=30 (todos recientes) debía salvarlos pese a RetentionCount=2", len(jobs))
	}
	for _, id := range jobIDs {
		if _, err := os.Stat(filepath.Join(env.destDir, id, "manifest.json")); err != nil {
			t.Errorf("el job %s debía conservarse intacto: %v", id, err)
		}
	}
}

// TestRunRetentionCountOverridesAggressiveDays demuestra la otra mitad: con
// RetentionDays=30 puro, dos jobs de hace 45 días se podarían por antiguos;
// con RetentionCount=5 activo a la vez (más que suficiente para cubrir los
// 2 que existen), la cuenta los salva a ambos -- ninguno se poda.
func TestRunRetentionCountOverridesAggressiveDays(t *testing.T) {
	env := newBackupTestEnv(t)

	id1 := env.createSyntheticCompletedJob(env.destDir, time.Now().Add(-45*24*time.Hour))
	id2 := env.createSyntheticCompletedJob(env.destDir, time.Now().Add(-44*24*time.Hour))

	// Un tercer Run real, con la misma política, es lo que dispara la poda
	// (la poda ocurre siempre al final de un Run exitoso).
	owner := env.user("piotr")
	env.upload(owner, "/", "trigger.txt", "contenido")
	if _, err := env.manager.Run(context.Background(), RunOptions{
		DestinationPath: env.destDir, RetentionCount: 5, RetentionDays: 30,
	}); err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	for _, id := range []string{id1, id2} {
		if _, err := env.manager.repo.GetJobByID(context.Background(), id); err != nil {
			t.Errorf("el job %s (45/44 días) debía conservarse: RetentionCount=5 solo tiene 3 jobs en total, ninguno queda fuera del top-5: %v", id, err)
		}
		if _, err := os.Stat(filepath.Join(env.destDir, id, "manifest.json")); err != nil {
			t.Errorf("la carpeta del job %s debía conservarse intacta: %v", id, err)
		}
	}
}

func TestRunRetentionZeroKeepsEverything(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("karl")

	for i := 0; i < 3; i++ {
		env.upload(owner, "/", fmt.Sprintf("archivo-%d.txt", i), "contenido")
		if _, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir}); err != nil {
			t.Fatalf("Run %d falló: %v", i, err)
		}
	}
	jobs, err := env.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("len(jobs) = %d, esperado 3: sin RetentionCount (0 por defecto) no debe podar nada", len(jobs))
	}
}

func TestRunRetentionOnlyAffectsSameDestination(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("liam")
	destA, destB := env.destDir, t.TempDir()

	var lastA, lastB string
	for i := 0; i < 3; i++ {
		env.upload(owner, "/", "a.txt", fmt.Sprintf("a%d", i))
		jobA, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: destA, RetentionCount: 1})
		if err != nil {
			t.Fatalf("Run A %d falló: %v", i, err)
		}
		lastA = jobA.ID
		jobB, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: destB})
		if err != nil {
			t.Fatalf("Run B %d falló: %v", i, err)
		}
		lastB = jobB.ID
	}
	_ = lastB

	jobs, err := env.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	var atA, atB int
	for _, j := range jobs {
		switch j.DestinationPath {
		case destA:
			atA++
		case destB:
			atB++
		}
	}
	if atA != 1 {
		t.Errorf("jobs en destA = %d, esperado 1 (podado con RetentionCount=1)", atA)
	}
	if atB != 3 {
		t.Errorf("jobs en destB = %d, esperado 3 (sin retención, no debe verse afectado por la poda de destA)", atB)
	}
	if _, err := os.Stat(filepath.Join(destA, lastA, "manifest.json")); err != nil {
		t.Errorf("el último job de destA debía sobrevivir: %v", err)
	}
}

func TestRunRetentionNeverPrunesFailedJobs(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("mia")

	env.upload(owner, "/", "roto.txt", "original")
	onDisk := filepath.Join(env.poolDir, owner, "roto.txt")
	if err := os.WriteFile(onDisk, []byte("corrompido"), 0o600); err != nil {
		t.Fatalf("corrompiendo el fichero de origen: %v", err)
	}
	if _, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, RetentionCount: 1}); err == nil {
		t.Fatal("Run debía fallar por el hash que no coincide")
	}
	failedJobs, err := env.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(failedJobs) != 1 || failedJobs[0].Status != StatusFailed {
		t.Fatalf("esperaba exactamente 1 job failed tras el Run corrupto: %+v", failedJobs)
	}
	failedJobID := failedJobs[0].ID

	// Arregla el origen y encadena 2 backups sanos más con la misma
	// RetentionCount -- el job failed nunca debe entrar en la cuenta ni
	// borrarse, solo se poda entre los completed.
	if err := os.WriteFile(onDisk, []byte("original"), 0o600); err != nil {
		t.Fatalf("restaurando el fichero de origen: %v", err)
	}
	for i := 0; i < 2; i++ {
		env.upload(owner, "/", fmt.Sprintf("sano-%d.txt", i), "contenido sano")
		if _, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir, RetentionCount: 1}); err != nil {
			t.Fatalf("Run sano %d falló: %v", i, err)
		}
	}

	jobs, err := env.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("len(jobs) = %d, esperado 2 (1 failed intacto + 1 completed tras podar con RetentionCount=1): %+v", len(jobs), jobs)
	}
	if _, err := env.manager.repo.GetJobByID(context.Background(), failedJobID); err != nil {
		t.Errorf("el job failed (%s) nunca debía borrarse: %v", failedJobID, err)
	}
}

// --- Cifrado (ADR-028) -----------------------------------------------

func TestRunEncryptedRoundTripsThroughRestoreRestoreToPoolAndVerify(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("erin")
	const plaintext = "contenido muy secreto que nadie debe leer en claro"
	const passphrase = "correcto-caballo-grapadora-batería"
	original := env.upload(owner, "/", "secreto.txt", plaintext)

	job, err := env.manager.Run(context.Background(), RunOptions{
		DestinationPath: env.destDir, Encrypt: true, Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("Run cifrado falló: %v", err)
	}

	manifest, err := readManifest(context.Background(), NewLocalDestination(env.destDir), job.ID)
	if err != nil {
		t.Fatalf("leyendo manifest.json: %v", err)
	}
	if !manifest.Encrypted || manifest.Salt == "" {
		t.Fatalf("manifest = %+v, esperado Encrypted=true con Salt no vacío", manifest)
	}
	if len(manifest.Pools) != 1 || len(manifest.Pools[0].Files) != 1 || manifest.Pools[0].Files[0].IV == "" {
		t.Fatalf("manifest.Pools = %+v, esperado 1 fichero con IV no vacío", manifest.Pools)
	}

	// Prueba real de que el contenido se cifró: los bytes en disco no deben
	// coincidir con el plaintext.
	backedUpPath := filepath.Join(env.destDir, job.ID, "data", env.defaultPoolID(), owner, "secreto.txt")
	onDisk, err := os.ReadFile(backedUpPath)
	if err != nil {
		t.Fatalf("leyendo la copia respaldada: %v", err)
	}
	if string(onDisk) == plaintext {
		t.Fatal("el contenido en disco coincide con el plaintext -- no se cifró")
	}

	restoreDir := t.TempDir()
	if err := env.manager.Restore(context.Background(), RestoreOptions{
		JobID: job.ID, DestinationPath: restoreDir, Passphrase: passphrase,
	}); err != nil {
		t.Fatalf("Restore con la passphrase correcta falló: %v", err)
	}
	gotRestore, err := os.ReadFile(filepath.Join(restoreDir, env.defaultPoolID(), owner, "secreto.txt"))
	if err != nil {
		t.Fatalf("leyendo el fichero restaurado: %v", err)
	}
	if string(gotRestore) != plaintext {
		t.Errorf("Restore = %q, esperado el plaintext original %q", gotRestore, plaintext)
	}

	destino := env.newPool("destino-cifrado", "")
	result, err := env.manager.RestoreToPool(context.Background(), RestoreToPoolOptions{
		JobID: job.ID, PoolID: destino.ID, Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("RestoreToPool con la passphrase correcta falló: %v", err)
	}
	if result.Restored != 1 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, esperado 1 restaurado sin errores", result)
	}
	meta, err := env.files.GetFileByNaturalKey(context.Background(), destino.ID, owner, "/", "secreto.txt")
	if err != nil {
		t.Fatalf("el archivo restaurado no aparece en el pool destino: %v", err)
	}
	if meta.SHA256 != original.SHA256 {
		t.Errorf("SHA256 tras RestoreToPool = %s, esperado %s (el del plaintext original)", meta.SHA256, original.SHA256)
	}
	_, rc, err := env.svc.Download(context.Background(), owner, meta.ID)
	if err != nil {
		t.Fatalf("Download del archivo restaurado falló: %v", err)
	}
	gotDownload, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("leyendo el contenido descargado: %v", err)
	}
	if string(gotDownload) != plaintext {
		t.Errorf("contenido descargado tras RestoreToPool = %q, esperado el plaintext original %q", gotDownload, plaintext)
	}

	verifyResult, err := env.manager.Verify(context.Background(), VerifyOptions{JobID: job.ID, Passphrase: passphrase})
	if err != nil {
		t.Fatalf("Verify con la passphrase correcta falló: %v", err)
	}
	if !verifyResult.OK {
		t.Errorf("Verify.OK = false, esperado true para un backup cifrado intacto: %+v", verifyResult.Files)
	}
}

func TestRunEncryptRequiresPassphraseAndFailsBeforeWritingAnything(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("frank")
	env.upload(owner, "/", "a.txt", "contenido")

	_, err := env.manager.Run(context.Background(), RunOptions{
		DestinationPath: env.destDir, Encrypt: true, Passphrase: "",
	})
	if !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("err = %v, esperado ErrPassphraseRequired", err)
	}

	entries, err := os.ReadDir(env.destDir)
	if err != nil {
		t.Fatalf("leyendo destDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("destDir = %v, esperado vacío -- Run no debía crear nada antes de fallar", entries)
	}
	jobs, err := env.manager.List(context.Background())
	if err != nil {
		t.Fatalf("List falló: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("List = %+v, esperado ningún job registrado -- falló antes de crear la fila", jobs)
	}
}

func TestRestoreWithWrongPassphraseReportsIntegrityMismatchNotPanic(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("gina")
	env.upload(owner, "/", "confidencial.txt", "contenido confidencial")

	job, err := env.manager.Run(context.Background(), RunOptions{
		DestinationPath: env.destDir, Encrypt: true, Passphrase: "passphrase-correcta",
	})
	if err != nil {
		t.Fatalf("Run cifrado falló: %v", err)
	}

	restoreDir := t.TempDir()
	err = env.manager.Restore(context.Background(), RestoreOptions{
		JobID: job.ID, DestinationPath: restoreDir, Passphrase: "passphrase-incorrecta",
	})
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("err = %v, esperado que envuelva ErrIntegrityMismatch (descifrar con la clave incorrecta produce basura que no verifica, igual que un bitrot)", err)
	}
	if _, err := os.Stat(filepath.Join(restoreDir, env.defaultPoolID(), owner, "confidencial.txt")); !os.IsNotExist(err) {
		t.Errorf("el fichero con hash inválido no debía quedar en el destino, err = %v", err)
	}
}

func TestRunEncryptDisablesIncrementalHardlinkBetweenTwoEncryptedRuns(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("hugo")
	env.upload(owner, "/", "estable.txt", "contenido que nunca cambia")

	opts := RunOptions{DestinationPath: env.destDir, Incremental: true, Encrypt: true, Passphrase: "misma-passphrase"}
	job1, err := env.manager.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("primer Run cifrado+incremental falló: %v", err)
	}
	job2, err := env.manager.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("segundo Run cifrado+incremental falló: %v", err)
	}

	pathIn := func(jobID string) string {
		return filepath.Join(env.destDir, jobID, "data", env.defaultPoolID(), owner, "estable.txt")
	}
	fi1, err := os.Stat(pathIn(job1.ID))
	if err != nil {
		t.Fatalf("Stat del primer backup: %v", err)
	}
	fi2, err := os.Stat(pathIn(job2.ID))
	if err != nil {
		t.Fatalf("Stat del segundo backup: %v", err)
	}
	if os.SameFile(fi1, fi2) {
		t.Error("los dos backups cifrados comparten inodo -- el hardlink de Incremental debía quedar desactivado con Encrypt=true (ADR-028)")
	}
}

func TestUnencryptedManifestStillRestoresAsPlaintextEvenWithPassphraseGiven(t *testing.T) {
	env := newBackupTestEnv(t)
	owner := env.user("ines")
	env.upload(owner, "/", "plano.txt", "contenido en claro")

	// Un manifiesto sin cifrar -- Encrypted/Salt/IV en su cero-valor, igual
	// que uno de antes de ADR-028 gracias a "omitempty" -- debe restaurarse
	// como plaintext sin ningún cambio de comportamiento, incluso si se da
	// una passphrase de más (p.ej. un scheduler que siempre exporta la
	// variable de entorno): RestoreOptions.Passphrase documenta que se
	// ignora sin más cuando el backup no está cifrado.
	job, err := env.manager.Run(context.Background(), RunOptions{DestinationPath: env.destDir})
	if err != nil {
		t.Fatalf("Run falló: %v", err)
	}

	restoreDir := t.TempDir()
	if err := env.manager.Restore(context.Background(), RestoreOptions{
		JobID: job.ID, DestinationPath: restoreDir, Passphrase: "passphrase-que-sobra",
	}); err != nil {
		t.Fatalf("Restore falló: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(restoreDir, env.defaultPoolID(), owner, "plano.txt"))
	if err != nil {
		t.Fatalf("leyendo el fichero restaurado: %v", err)
	}
	if string(got) != "contenido en claro" {
		t.Errorf("contenido restaurado = %q, esperado %q", got, "contenido en claro")
	}
}

// writeFileToPool sube contenido directamente al Provider de un pool
// concreto + su fila de metadatos, sin pasar por el pool activo por defecto
// que usa FileService.Upload -- necesario en los tests para dirigir un
// archivo a un pool secundario específico.
func writeFileToPool(t *testing.T, env *backupTestEnv, poolID, ownerID, parentPath, name, content string) {
	t.Helper()
	ctx := context.Background()
	provider, err := env.resolver.For(ctx, poolID)
	if err != nil {
		t.Fatalf("resolviendo el provider del pool %s: %v", poolID, err)
	}
	size, sha, err := provider.Write(ctx, "/"+ownerID+parentPath+"/"+name, bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("Write falló: %v", err)
	}
	meta := &storage.FileMeta{
		ID: idgen.New(), PoolID: poolID, OwnerID: ownerID, ParentPath: parentPath, Name: name,
		SizeBytes: size, SHA256: sha, MimeType: "text/plain",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := env.files.UpsertFile(ctx, meta); err != nil {
		t.Fatalf("UpsertFile falló: %v", err)
	}
}

// createSyntheticCompletedJob inserta un backup "completed" ya terminado en
// startedAt, con su carpeta y manifest.json reales en disco (aunque sin
// ningún fichero de datos dentro -- no hace falta para probar la política
// de retención). Necesario porque Run siempre usa time.Now(): es la única
// forma de tener en los tests un backup "antiguo" de verdad para las
// pruebas de RetentionDays.
func (e *backupTestEnv) createSyntheticCompletedJob(destinationPath string, startedAt time.Time) string {
	e.t.Helper()
	ctx := context.Background()
	job := &Job{
		ID:              idgen.New(),
		Status:          StatusCompleted,
		DestinationPath: destinationPath,
		PoolIDs:         []string{e.defaultPoolID()},
		StartedAt:       startedAt,
	}
	if err := e.backupRepo.CreateJob(ctx, job); err != nil {
		e.t.Fatalf("CreateJob (sintético) falló: %v", err)
	}
	if err := writeManifest(ctx, NewLocalDestination(destinationPath), job.ID, &Manifest{JobID: job.ID, CreatedAt: startedAt}); err != nil {
		e.t.Fatalf("writeManifest (sintético) falló: %v", err)
	}
	return job.ID
}
