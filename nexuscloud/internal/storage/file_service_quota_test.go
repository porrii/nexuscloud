package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Cuotas de almacenamiento (§24, ADR-036): FileService.Upload rechaza con
// ErrQuotaExceeded lo que no cabe en el límite efectivo del propietario. El
// uso es la huella real en disco: archivos activos + papelera + versiones.

// withQuotas activa las cuotas del servicio de prueba con el resolver real
// (users.Service: cuota propia -> grupo -> global) y la medición SQL real.
func (e *testEnv) withQuotas() {
	WithQuotas(e.userSvc, NewSQLUsageRepository(e.conn))(e.svc)
}

func (e *testEnv) setQuota(t *testing.T, ownerID string, limit int64) {
	t.Helper()
	if err := e.userSvc.SetUserQuota(context.Background(), ownerID, &limit); err != nil {
		t.Fatalf("fijando la cuota de %s: %v", ownerID, err)
	}
}

// blob genera n bytes que dependen de seed: contenidos distintos = hashes distintos.
func blob(seed byte, n int) []byte { return bytes.Repeat([]byte{seed}, n) }

func quotaUpload(env *testEnv, owner, name string, content []byte) (*FileMeta, error) {
	return env.svc.Upload(context.Background(), UploadInput{
		OwnerID: owner, ParentPath: "/", Name: name, Content: bytes.NewReader(content),
	})
}

func mustUsage(t *testing.T, env *testEnv, owner string) Usage {
	t.Helper()
	u, err := env.svc.Usage(context.Background(), owner)
	if err != nil {
		t.Fatalf("Usage falló: %v", err)
	}
	return u
}

// assertNoStagingLeftovers comprueba que una subida rechazada no dejó ningún
// temporal a medias en el staging del propietario.
func assertNoStagingLeftovers(t *testing.T, env *testEnv, owner string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(env.poolDir, owner, ".nexuscloud-staging"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("leyendo el staging: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("quedan %d temporales en el staging tras una subida rechazada", len(entries))
	}
}

// countingReader cuenta cuántos bytes se le han pedido de verdad.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func TestQuotaDoesNothingWithoutALimit(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "sin-limite")

	if _, err := quotaUpload(env, owner, "grande.bin", blob(1, 5000)); err != nil {
		t.Fatalf("sin cuota configurada la subida debe funcionar como siempre: %v", err)
	}
}

func TestQuotaRejectsAnUploadThatDoesNotFit(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "ana")
	env.setQuota(t, owner, 100)

	if _, err := quotaUpload(env, owner, "a.bin", blob(1, 60)); err != nil {
		t.Fatalf("60 bytes caben en 100: %v", err)
	}
	_, err := quotaUpload(env, owner, "b.bin", blob(2, 50))
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("60+50 sobre 100 = %v, esperado ErrQuotaExceeded", err)
	}
	if names := listNames(t, env, owner, "/"); len(names) != 1 || names[0] != "a.bin" {
		t.Errorf("archivos = %v, esperado solo a.bin: el rechazado no debe existir", names)
	}
	if got := mustUsage(t, env, owner).Total(); got != 60 {
		t.Errorf("uso = %d, esperado 60", got)
	}
	assertNoStagingLeftovers(t, env, owner)
}

func TestQuotaLimitIsInclusive(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "justo")
	env.setQuota(t, owner, 100)

	if _, err := quotaUpload(env, owner, "exacto.bin", blob(1, 100)); err != nil {
		t.Fatalf("exactamente el límite debe caber: %v", err)
	}
	if _, err := quotaUpload(env, owner, "uno.bin", blob(2, 1)); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("un byte de más = %v, esperado ErrQuotaExceeded", err)
	}
	if _, err := quotaUpload(env, owner, "vacio.txt", nil); err != nil {
		t.Errorf("un archivo vacío no añade nada y debe caber: %v", err)
	}
}

// La papelera y las versiones ocupan disco de verdad: si no contaran, borrar
// y volver a subir (o sobrescribir) evadiría la cuota.
func TestQuotaCountsTrashAndVersions(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true) // con papelera y con versionado
	env.withQuotas()
	owner := env.user(t, "papelera")
	env.setQuota(t, owner, 100)

	a, err := quotaUpload(env, owner, "a.bin", blob(1, 40))
	if err != nil {
		t.Fatalf("subiendo a: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if u := mustUsage(t, env, owner); u.FilesBytes != 0 || u.TrashBytes != 40 || u.VersionsBytes != 0 {
		t.Fatalf("uso tras borrar = %+v, esperado 40 en la papelera", u)
	}

	if _, err := quotaUpload(env, owner, "b.bin", blob(2, 50)); err != nil {
		t.Fatalf("subiendo b: %v", err)
	}
	if _, err := quotaUpload(env, owner, "c.bin", blob(3, 20)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("40 en la papelera + 50 + 20 sobre 100 = %v, esperado ErrQuotaExceeded", err)
	}

	// Vaciar la papelera libera el espacio.
	if err := env.svc.PermanentlyDeleteFile(ctx, owner, a.ID); err != nil {
		t.Fatalf("PermanentlyDeleteFile: %v", err)
	}
	if _, err := quotaUpload(env, owner, "c.bin", blob(3, 20)); err != nil {
		t.Fatalf("tras vaciar la papelera c debe caber: %v", err)
	}

	// Sobrescribir b (50 -> 25) deja la versión anterior: 20 + 25 + 50 = 95.
	if _, err := quotaUpload(env, owner, "b.bin", blob(9, 25)); err != nil {
		t.Fatalf("sobrescribiendo b: %v", err)
	}
	if u := mustUsage(t, env, owner); u.FilesBytes != 45 || u.VersionsBytes != 50 || u.TrashBytes != 0 {
		t.Fatalf("uso = %+v, esperado 45 de archivos y 50 de versiones", u)
	}
	if _, err := quotaUpload(env, owner, "d.bin", blob(4, 10)); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("las versiones cuentan: 95 + 10 sobre 100 = %v, esperado ErrQuotaExceeded", err)
	}
}

// Sin versionado, sobrescribir libera el contenido anterior: solo cuenta la
// diferencia.
func TestQuotaOverwriteWithoutVersioningCreditsTheOldContent(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, false, 10, 0, 0, true, true)
	env.withQuotas()
	owner := env.user(t, "sin-versiones")
	env.setQuota(t, owner, 100)

	f, err := quotaUpload(env, owner, "f.bin", blob(1, 90))
	if err != nil {
		t.Fatalf("subiendo f: %v", err)
	}
	if _, err := quotaUpload(env, owner, "f.bin", blob(2, 100)); err != nil {
		t.Fatalf("sobrescribir 90 por 100 sobre 100 (el anterior se libera): %v", err)
	}
	if u := mustUsage(t, env, owner); u.FilesBytes != 100 || u.VersionsBytes != 0 {
		t.Errorf("uso = %+v, esperado 100 de archivos y ninguna versión", u)
	}
	if _, err := quotaUpload(env, owner, "f.bin", blob(3, 101)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("sobrescribir por 101 = %v, esperado ErrQuotaExceeded", err)
	}
	_, rc, err := env.svc.Download(ctx, owner, f.ID)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got := readAll(t, rc); !bytes.Equal(got, blob(2, 100)) {
		t.Errorf("el rechazo no debe tocar el contenido vigente (%d bytes)", len(got))
	}
}

// Con versionado, el contenido anterior se conserva como versión y sí ocupa;
// pero volver a subir EXACTAMENTE lo mismo no crea versión ni gasta cuota.
func TestQuotaOverwriteWithVersioningKeepsTheOldContent(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "con-versiones")
	env.setQuota(t, owner, 100)

	f, err := quotaUpload(env, owner, "f.bin", blob(1, 60))
	if err != nil {
		t.Fatalf("subiendo f: %v", err)
	}
	if _, err := quotaUpload(env, owner, "g.bin", blob(3, 40)); err != nil {
		t.Fatalf("subiendo g: %v", err)
	}

	if _, err := quotaUpload(env, owner, "f.bin", blob(2, 60)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("sobrescribir con otro contenido (el anterior queda como versión) = %v, esperado ErrQuotaExceeded", err)
	}
	_, rc, _ := env.svc.Download(ctx, owner, f.ID)
	if got := readAll(t, rc); !bytes.Equal(got, blob(1, 60)) {
		t.Error("el rechazo no debe tocar el contenido vigente")
	}

	if _, err := quotaUpload(env, owner, "f.bin", blob(1, 60)); err != nil {
		t.Fatalf("volver a subir el mismo contenido al 100%% debe funcionar: %v", err)
	}
	if u := mustUsage(t, env, owner); u.FilesBytes != 100 || u.VersionsBytes != 0 {
		t.Errorf("uso = %+v, esperado 100 de archivos y ninguna versión", u)
	}
	assertNoStagingLeftovers(t, env, owner)
}

// Si el Content-Length ya dice que no cabe, se rechaza sin leer el cuerpo.
func TestQuotaRejectsEarlyWithTheSizeHint(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "pista")
	env.setQuota(t, owner, 100)

	src := &countingReader{r: bytes.NewReader(blob(1, 500))}
	_, err := env.svc.Upload(context.Background(), UploadInput{
		OwnerID: owner, ParentPath: "/", Name: "x.bin", Content: src, SizeHint: 500,
	})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("SizeHint 500 sobre 100 = %v, esperado ErrQuotaExceeded", err)
	}
	if src.n != 0 {
		t.Errorf("se leyeron %d bytes del cuerpo: debía rechazarse antes de leerlo", src.n)
	}
	assertNoStagingLeftovers(t, env, owner)
}

// Sin pista de tamaño (chunked), la lectura se corta al pasar el espacio libre.
func TestQuotaStopsReadingWhenTheSpaceRunsOut(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "corte")
	env.setQuota(t, owner, 100)

	src := &countingReader{r: bytes.NewReader(blob(1, 100000))}
	_, err := env.svc.Upload(context.Background(), UploadInput{
		OwnerID: owner, ParentPath: "/", Name: "x.bin", Content: src,
	})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("100 000 bytes sobre 100 = %v, esperado ErrQuotaExceeded", err)
	}
	if src.n > 101 {
		t.Errorf("se leyeron %d bytes: debía cortar en cuanto se pasa el límite (101 como mucho)", src.n)
	}
	if got := mustUsage(t, env, owner).Total(); got != 0 {
		t.Errorf("uso = %d, esperado 0", got)
	}
	assertNoStagingLeftovers(t, env, owner)
}

func TestQuotaSkipQuota(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "restauracion")
	env.setQuota(t, owner, 10)

	if _, err := env.svc.Upload(context.Background(), UploadInput{
		OwnerID: owner, ParentPath: "/", Name: "restaurado.bin", Content: bytes.NewReader(blob(1, 1000)), SkipQuota: true,
	}); err != nil {
		t.Fatalf("SkipQuota debe saltarse la cuota (restaurar un backup no puede fallar por ella): %v", err)
	}
}

// Estar por encima de la cuota (p. ej. porque se bajó) solo bloquea subir más:
// leer, mover, borrar y restaurar siguen funcionando.
func TestQuotaOverTheLimitOnlyBlocksNewUploads(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "pasado")

	f, err := quotaUpload(env, owner, "f.bin", blob(1, 80))
	if err != nil {
		t.Fatalf("subiendo f: %v", err)
	}
	env.setQuota(t, owner, 10) // ahora está muy por encima

	if _, err := quotaUpload(env, owner, "g.bin", blob(2, 1)); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("subir estando por encima = %v, esperado ErrQuotaExceeded", err)
	}
	if _, rc, err := env.svc.Download(ctx, owner, f.ID); err != nil {
		t.Errorf("Download estando por encima de la cuota: %v", err)
	} else {
		rc.Close()
	}
	if _, err := env.svc.Mkdir(ctx, owner, "/", "Otra", ""); err != nil {
		t.Errorf("Mkdir estando por encima: %v", err)
	}
	dest := "/Otra"
	if _, err := env.svc.MoveFile(ctx, owner, f.ID, &dest, nil); err != nil {
		t.Errorf("MoveFile estando por encima: %v", err)
	}
	if err := env.svc.Delete(ctx, owner, f.ID); err != nil {
		t.Errorf("Delete estando por encima: %v", err)
	}
	if err := env.svc.RestoreFile(ctx, owner, f.ID); err != nil {
		t.Errorf("RestoreFile estando por encima: %v", err)
	}
}

// Ocho subidas simultáneas de 1 KiB con sitio para tres: el cerrojo del
// propietario hace que la comprobación al confirmar sea autoritativa, así que
// entran exactamente tres, nunca más.
func TestQuotaConcurrentUploadsNeverExceedTheLimit(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "concurrente")
	env.setQuota(t, owner, 3*1024)

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = quotaUpload(env, owner, fmt.Sprintf("f%d.bin", i), blob(byte(i+1), 1024))
		}(i)
	}
	close(start)
	wg.Wait()

	ok, rejected := 0, 0
	for i, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrQuotaExceeded):
			rejected++
		default:
			t.Errorf("subida %d: error inesperado %v", i, err)
		}
	}
	if ok != 3 || rejected != 5 {
		t.Errorf("entraron %d y se rechazaron %d, esperado 3 y 5", ok, rejected)
	}
	if got := mustUsage(t, env, owner).Total(); got != 3*1024 {
		t.Errorf("uso = %d, esperado %d", got, 3*1024)
	}
	assertNoStagingLeftovers(t, env, owner)
}

func TestUsageBreakdownAndIsolation(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	env.withQuotas()
	one := env.user(t, "uno")
	two := env.user(t, "dos")
	nobody := env.user(t, "nadie")

	if _, err := quotaUpload(env, one, "a.bin", blob(1, 10)); err != nil {
		t.Fatal(err)
	}
	b, err := quotaUpload(env, one, "b.bin", blob(2, 20))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.svc.Delete(ctx, one, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := quotaUpload(env, one, "a.bin", blob(5, 15)); err != nil { // deja la versión de 10
		t.Fatal(err)
	}
	if _, err := quotaUpload(env, two, "c.bin", blob(7, 7)); err != nil {
		t.Fatal(err)
	}

	if u := mustUsage(t, env, one); u != (Usage{FilesBytes: 15, TrashBytes: 20, VersionsBytes: 10}) {
		t.Errorf("uso de uno = %+v, esperado 15 activos, 20 en papelera, 10 en versiones", u)
	}
	if u := mustUsage(t, env, two); u != (Usage{FilesBytes: 7}) {
		t.Errorf("uso de dos = %+v, esperado solo 7 activos (no debe mezclarse con el de uno)", u)
	}
	if u := mustUsage(t, env, nobody); u != (Usage{}) {
		t.Errorf("uso de quien no tiene nada = %+v, esperado cero", u)
	}

	all, err := NewSQLUsageRepository(env.conn).AllOwnersUsage(ctx)
	if err != nil {
		t.Fatalf("AllOwnersUsage: %v", err)
	}
	if all[one] != (Usage{FilesBytes: 15, TrashBytes: 20, VersionsBytes: 10}) || all[two] != (Usage{FilesBytes: 7}) {
		t.Errorf("AllOwnersUsage = %+v", all)
	}
	if _, present := all[nobody]; present {
		t.Error("AllOwnersUsage no debe listar a quien no tiene datos")
	}
}

func TestUsageIsUnavailableWithoutQuotaConfiguration(t *testing.T) {
	env := newTestEnv(t, true) // sin withQuotas
	owner := env.user(t, "sin-config")
	if _, err := env.svc.Usage(context.Background(), owner); !errors.Is(err, ErrUsageUnavailable) {
		t.Errorf("Usage sin WithQuotas = %v, esperado ErrUsageUnavailable", err)
	}
}

type failingResolver struct{}

func (failingResolver) LimitFor(context.Context, string) (int64, error) {
	return 0, errors.New("resolver caído")
}

// Si no se puede saber el límite, se falla (no se sube «sin límite» en silencio).
func TestQuotaFailsClosedWhenTheLimitCannotBeResolved(t *testing.T) {
	env := newTestEnv(t, true)
	WithQuotas(failingResolver{}, NewSQLUsageRepository(env.conn))(env.svc)
	owner := env.user(t, "resolver-roto")

	_, err := quotaUpload(env, owner, "a.bin", blob(1, 10))
	if err == nil || errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("con el resolver roto = %v, esperado un error distinto de ErrQuotaExceeded", err)
	}
}

// Los archivos de una carpeta compartida pertenecen al propietario de la
// carpeta (ADR-035): cuentan contra SU cuota, no contra la de quien sube.
func TestQuotaOfASharedFolderIsTheOwnersNotTheUploaders(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "propietario")
	guest := env.user(t, "invitado")
	dir := mkFolder(t, env, owner, "/", "Buzon")
	grant(t, env, owner, dir, guest, "", true, nil)
	env.setQuota(t, owner, 100)
	env.setQuota(t, guest, 10) // la del invitado no debe importar

	if _, _, err := sharedUpload(env, guest, dir, "a.txt", strings.Repeat("x", 60)); err != nil {
		t.Fatalf("60 bytes caben en la cuota del propietario (100): %v", err)
	}
	if u := mustUsage(t, env, owner); u.FilesBytes != 60 {
		t.Errorf("uso del propietario = %+v, esperado 60", u)
	}
	if u := mustUsage(t, env, guest); u.Total() != 0 {
		t.Errorf("uso del invitado = %+v, esperado 0: lo subido no es suyo", u)
	}
	if _, _, err := sharedUpload(env, guest, dir, "b.txt", strings.Repeat("y", 50)); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("60+50 sobre la cuota del propietario (100) = %v, esperado ErrQuotaExceeded", err)
	}
}

func TestQuotaAppliesToPublicLinkUploads(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "dueno")
	dir := mkFolder(t, env, owner, "/", "Publica")
	env.setQuota(t, owner, 100)
	_, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceIsDirectory: true, ResourceID: dir.ID, Type: ShareTypeLink, CanUpload: true,
	})
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	if _, err := env.svc.UploadViaPublicShare(ctx, PublicUploadInput{Token: token, Name: "a.txt", Content: strings.NewReader(strings.Repeat("x", 60))}); err != nil {
		t.Fatalf("60 bytes caben: %v", err)
	}
	if _, err := env.svc.UploadViaPublicShare(ctx, PublicUploadInput{Token: token, Name: "b.txt", Content: strings.NewReader(strings.Repeat("y", 50))}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("60+50 sobre 100 = %v, esperado ErrQuotaExceeded", err)
	}
	// Con SizeHint, sin leer el cuerpo.
	src := &countingReader{r: strings.NewReader(strings.Repeat("z", 500))}
	if _, err := env.svc.UploadViaPublicShare(ctx, PublicUploadInput{Token: token, Name: "c.txt", Content: src, SizeHint: 500}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("SizeHint 500 = %v, esperado ErrQuotaExceeded", err)
	}
	if src.n != 0 {
		t.Errorf("se leyeron %d bytes: debía rechazarse antes de leerlo", src.n)
	}
}

func TestQuotaSizeHintOnSharedFolderUploads(t *testing.T) {
	env := newTestEnv(t, true)
	env.withQuotas()
	owner := env.user(t, "propietario")
	guest := env.user(t, "invitado")
	dir := mkFolder(t, env, owner, "/", "Buzon")
	grant(t, env, owner, dir, guest, "", true, nil)
	env.setQuota(t, owner, 100)

	src := &countingReader{r: strings.NewReader(strings.Repeat("x", 500))}
	_, _, err := env.svc.UploadToSharedDirectory(context.Background(), SharedUploadInput{
		RequesterID: guest, DirectoryID: dir.ID, Name: "a.txt", Content: src, SizeHint: 500,
	})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("SizeHint 500 sobre 100 = %v, esperado ErrQuotaExceeded", err)
	}
	if src.n != 0 {
		t.Errorf("se leyeron %d bytes: debía rechazarse antes de leerlo", src.n)
	}
}
