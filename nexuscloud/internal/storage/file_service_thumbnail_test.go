package storage

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/users"
)

// thumbnailTestEnv monta su PROPIO FileService con WithThumbnails
// activado -- newTestEnv (file_service_test.go) no lo soporta y lo usan
// ~20 tests preexistentes, así que en vez de tocarlo se duplica aquí la
// parte de montaje que hace falta (mismo criterio de "duplicación en vez
// de abstracción prematura" ya establecido en el resto del proyecto).
type thumbnailTestEnv struct {
	svc     *FileService
	userSvc *users.Service
	cache   *ThumbnailCache
	jobs    ThumbnailJobRepository
}

func newThumbnailTestEnv(t *testing.T, limits ThumbnailLimits, enabled bool) *thumbnailTestEnv {
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
	resolver := NewPoolProviderResolver(pools)
	files := NewSQLFileRepository(conn)
	directories := NewSQLDirectoryRepository(conn)
	versions := NewSQLVersionRepository(conn)
	shares := NewSQLShareRepository(conn)
	userRepo := users.NewSQLRepository(conn)
	jobs := NewSQLThumbnailJobRepository(conn)

	cache, err := NewThumbnailCache(t.TempDir(), 100*1024*1024)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}

	svc := NewFileService(files, directories, versions, shares, pools, resolver, &testPasswordHasher{},
		false, true, 10, 0, 0, true, true,
		WithThumbnails(jobs, cache, t.TempDir(), limits, enabled),
	)
	return &thumbnailTestEnv{
		svc:     svc,
		userSvc: users.NewService(userRepo),
		cache:   cache,
		jobs:    jobs,
	}
}

func (e *thumbnailTestEnv) user(t *testing.T, username string) string {
	t.Helper()
	u, err := e.userSvc.CreateUser(context.Background(), users.CreateUserInput{Username: username, PasswordHash: "hash-de-prueba"})
	if err != nil {
		t.Fatalf("creando usuario de prueba %q: %v", username, err)
	}
	return u.ID
}

func defaultThumbnailLimits() ThumbnailLimits {
	return ThumbnailLimits{
		Image:                    ImageThumbnailLimits{MaxInputBytes: 25 * 1024 * 1024, MaxPixels: 40_000_000, Timeout: 5 * time.Second},
		Video:                    ExecThumbnailLimits{MaxInputBytes: 200 * 1024 * 1024, Timeout: 30 * time.Second},
		PDF:                      ExecThumbnailLimits{MaxInputBytes: 50 * 1024 * 1024, Timeout: 20 * time.Second},
		MaxConcurrentGenerations: 4,
	}
}

func uploadBytes(t *testing.T, env *thumbnailTestEnv, ownerID, name string, content []byte) *FileMeta {
	t.Helper()
	meta, err := env.svc.Upload(context.Background(), UploadInput{
		OwnerID: ownerID, ParentPath: "/", Name: name, Content: bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("Upload %s: %v", name, err)
	}
	return meta
}

func TestEnqueueThumbnailJob_SoloParaTiposConMiniatura(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	owner := env.user(t, "duena")

	imgMeta := uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(20, 20))
	if job, err := env.jobs.NextPending(context.Background()); err != nil || job == nil || job.FileID != imgMeta.ID {
		t.Errorf("subir una imagen debería encolar un job; NextPending = (%+v, %v)", job, err)
	} else if err := env.jobs.MarkDone(context.Background(), job.ID); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	uploadBytes(t, env, owner, "notas.txt", []byte("texto plano, sin miniatura posible"))
	if job, err := env.jobs.NextPending(context.Background()); err != nil || job != nil {
		t.Errorf("subir un .txt NO debería encolar ningún job; NextPending = (%+v, %v)", job, err)
	}
}

func TestEnqueueThumbnailJob_DesactivadoNoEncolaNada(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), false)
	owner := env.user(t, "duena")
	uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(20, 20))

	if job, err := env.jobs.NextPending(context.Background()); err != nil || job != nil {
		t.Errorf("con thumbnails desactivados, Upload NO debería encolar ningún job; NextPending = (%+v, %v)", job, err)
	}
}

func TestGenerateOrGetThumbnail_Desactivado(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), false)
	owner := env.user(t, "duena")
	f := uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(20, 20))

	_, _, _, err := env.svc.GenerateOrGetThumbnail(context.Background(), owner, f.ID)
	if !errors.Is(err, ErrThumbnailsDisabled) {
		t.Errorf("err = %v, esperado ErrThumbnailsDisabled", err)
	}
}

func TestGenerateOrGetThumbnail_ArchivoAjenoSinAcceso(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	owner := env.user(t, "duena")
	otro := env.user(t, "fisgon")
	f := uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(20, 20))

	_, _, _, err := env.svc.GenerateOrGetThumbnail(context.Background(), otro, f.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, esperado ErrForbidden (§198 IDOR)", err)
	}
}

func TestGenerateOrGetThumbnail_TipoNoSoportado(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	owner := env.user(t, "duena")
	f := uploadBytes(t, env, owner, "notas.txt", []byte("sin miniatura posible"))

	_, _, _, err := env.svc.GenerateOrGetThumbnail(context.Background(), owner, f.ID)
	if !errors.Is(err, ErrThumbnailUnsupportedFormat) {
		t.Errorf("err = %v, esperado ErrThumbnailUnsupportedFormat", err)
	}
}

func TestGenerateOrGetThumbnail_GeneraYCachea(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	owner := env.user(t, "duena")
	f := uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(800, 600))

	meta, data, becameFailed, err := env.svc.GenerateOrGetThumbnail(context.Background(), owner, f.ID)
	if err != nil {
		t.Fatalf("GenerateOrGetThumbnail: %v", err)
	}
	if becameFailed {
		t.Error("becameFailed = true en una generación exitosa")
	}
	if meta.ID != f.ID {
		t.Errorf("meta.ID = %s, esperado %s", meta.ID, f.ID)
	}
	if len(data) == 0 {
		t.Error("la miniatura generada está vacía")
	}

	cached, found, err := env.cache.Get(owner, f.SHA256)
	if err != nil || !found {
		t.Fatalf("la miniatura debería haber quedado en caché tras generarse: (found=%v, err=%v)", found, err)
	}
	if !bytes.Equal(cached, data) {
		t.Error("el contenido cacheado no coincide con el devuelto")
	}

	// El job se marca done (borrado) tras generarse con éxito.
	if job, err := env.jobs.NextPending(context.Background()); err != nil || job != nil {
		t.Errorf("tras generar con éxito, no debería quedar ningún job pendiente; quedó %+v", job)
	}
}

func TestGenerateOrGetThumbnail_SirveDesdeCacheSinRegenerar(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	owner := env.user(t, "duena")
	f := uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(20, 20))

	// Pre-rellena la caché con un marcador que NUNCA saldría de una
	// generación real -- si GenerateOrGetThumbnail regenerase en vez de
	// leer de caché, este valor centinela no coincidiría.
	sentinel := []byte("MARCADOR-DE-CACHE-NUNCA-GENERADO-DE-VERDAD")
	if err := env.cache.Put(owner, f.SHA256, sentinel); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, data, _, err := env.svc.GenerateOrGetThumbnail(context.Background(), owner, f.ID)
	if err != nil {
		t.Fatalf("GenerateOrGetThumbnail: %v", err)
	}
	if !bytes.Equal(data, sentinel) {
		t.Errorf("data = %q, esperado el marcador de caché %q (no debería haber regenerado)", data, sentinel)
	}
}

func TestGenerateOrGetThumbnail_CachePelaLlena(t *testing.T) {
	limits := defaultThumbnailLimits()
	env := newThumbnailTestEnv(t, limits, true)
	// Sustituye la caché por una con un tope minúsculo, ya superado.
	tinyCache, err := NewThumbnailCache(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}
	if err := tinyCache.Put("relleno", "sha-relleno", []byte("0123456789")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	env.svc.thumbnailCache = tinyCache
	owner := env.user(t, "duena")
	f := uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(20, 20))

	_, _, _, err = env.svc.GenerateOrGetThumbnail(context.Background(), owner, f.ID)
	if !errors.Is(err, ErrThumbnailCacheFull) {
		t.Errorf("err = %v, esperado ErrThumbnailCacheFull", err)
	}
}

func TestGenerateOrGetThumbnail_FalloHastaBecameFailed(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	owner := env.user(t, "duena")
	// Un archivo con extensión .jpg pero contenido que NO es una imagen
	// real -- detectMimeType lo clasifica igualmente como image/jpeg por
	// extensión, así que ThumbnailKindForMimeType lo intenta, y
	// GenerateImageThumbnail falla al decodificar (ErrThumbnailUnsupportedFormat).
	f := uploadBytes(t, env, owner, "corrupto.jpg", []byte("esto no es un JPEG de verdad"))

	var lastBecameFailed bool
	for i := 0; i < thumbnailMaxAttempts; i++ {
		_, _, becameFailed, err := env.svc.GenerateOrGetThumbnail(context.Background(), owner, f.ID)
		if err == nil {
			t.Fatalf("intento %d: se esperaba un error generando la miniatura de un archivo corrupto", i+1)
		}
		lastBecameFailed = becameFailed
	}
	if !lastBecameFailed {
		t.Errorf("tras %d intentos fallidos, becameFailed debería ser true", thumbnailMaxAttempts)
	}
}

func TestGenerateOrGetThumbnail_SemaforoLleno(t *testing.T) {
	limits := defaultThumbnailLimits()
	limits.MaxConcurrentGenerations = 1
	env := newThumbnailTestEnv(t, limits, true)
	owner := env.user(t, "duena")
	f := uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(20, 20))

	// Ocupa manualmente el único hueco del semáforo -- determinista, sin
	// depender de ganar una carrera de temporización contra una
	// generación real en curso (mismo espíritu que el test de timeout del
	// pipeline de imagen: nunca confiar en la velocidad real de un
	// proceso concurrente para que un test sea fiable).
	env.svc.thumbnailSem <- struct{}{}
	defer func() { <-env.svc.thumbnailSem }()

	_, _, _, err := env.svc.GenerateOrGetThumbnail(context.Background(), owner, f.ID)
	if !errors.Is(err, ErrThumbnailBusy) {
		t.Errorf("err = %v, esperado ErrThumbnailBusy (§34 Decisión 6)", err)
	}
}

func TestProcessNextThumbnailJob_SinJobsPendientes(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	result, err := env.svc.ProcessNextThumbnailJob(context.Background())
	if err != nil {
		t.Fatalf("ProcessNextThumbnailJob: %v", err)
	}
	if result.Processed {
		t.Errorf("result = %+v, esperado Processed=false sin jobs pendientes", result)
	}
}

func TestProcessNextThumbnailJob_ProcesaConExito(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	owner := env.user(t, "duena")
	f := uploadBytes(t, env, owner, "foto.jpg", mustEncodeJPEG(100, 100))

	result, err := env.svc.ProcessNextThumbnailJob(context.Background())
	if err != nil {
		t.Fatalf("ProcessNextThumbnailJob: %v", err)
	}
	if !result.Processed || result.BecameFailed || result.FileID != f.ID {
		t.Errorf("result = %+v, esperado Processed=true/BecameFailed=false/FileID=%s", result, f.ID)
	}
	if _, found, err := env.cache.Get(owner, f.SHA256); err != nil || !found {
		t.Errorf("tras procesar el job, la miniatura debería estar en caché: (found=%v, err=%v)", found, err)
	}
}

func TestProcessNextThumbnailJob_FalloHastaFailed(t *testing.T) {
	env := newThumbnailTestEnv(t, defaultThumbnailLimits(), true)
	owner := env.user(t, "duena")
	uploadBytes(t, env, owner, "corrupto.jpg", []byte("no es un JPEG de verdad"))

	var last ThumbnailJobResult
	for i := 0; i < thumbnailMaxAttempts; i++ {
		result, err := env.svc.ProcessNextThumbnailJob(context.Background())
		if err != nil {
			t.Fatalf("intento %d: ProcessNextThumbnailJob: %v", i+1, err)
		}
		if !result.Processed {
			t.Fatalf("intento %d: Processed=false, esperado true (job corrupto real)", i+1)
		}
		last = result
	}
	if !last.BecameFailed {
		t.Errorf("tras %d intentos, BecameFailed debería ser true; último resultado = %+v", thumbnailMaxAttempts, last)
	}

	failed, err := env.jobs.ListByStatus(context.Background(), ThumbnailJobFailed, 10, 0)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(failed) != 1 {
		t.Errorf("ListByStatus(failed) = %d jobs, esperado 1", len(failed))
	}
}
