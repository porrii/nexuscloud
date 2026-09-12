package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
		&testPasswordHasher{}, true, true, 10, true, true)
	backupRepo := NewSQLRepository(conn)

	return &backupTestEnv{
		t: t, svc: svc, files: files, pools: pools, resolver: resolver,
		backupRepo: backupRepo,
		manager:    NewManager(pools, files, resolver, backupRepo),
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

	manifest, err := readManifest(filepath.Join(env.destDir, job.ID))
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
	manifest, err := readManifest(filepath.Join(env.destDir, job.ID))
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
	manifest, _ := readManifest(filepath.Join(env.destDir, job.ID))
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
	manifest, _ := readManifest(filepath.Join(env.destDir, job.ID))
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
	if _, statErr := os.Stat(manifestPath(filepath.Join(env.destDir, jobs[0].ID))); !os.IsNotExist(statErr) {
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
	jobDir := filepath.Join(destinationPath, job.ID)
	if err := os.MkdirAll(jobDir, 0o750); err != nil {
		e.t.Fatalf("creando la carpeta del job sintético: %v", err)
	}
	if err := writeManifest(jobDir, &Manifest{JobID: job.ID, CreatedAt: startedAt}); err != nil {
		e.t.Fatalf("writeManifest (sintético) falló: %v", err)
	}
	return job.ID
}
