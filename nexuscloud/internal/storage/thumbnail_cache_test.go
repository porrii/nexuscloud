package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestThumbnailCacheGetPutRoundTrip(t *testing.T) {
	cache, err := NewThumbnailCache(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}

	if _, found, err := cache.Get("owner-1", "sha-abc"); err != nil || found {
		t.Fatalf("Get antes de Put = (found=%v, err=%v), esperado (false, nil)", found, err)
	}

	data := []byte("contenido de la miniatura")
	if err := cache.Put("owner-1", "sha-abc", data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, found, err := cache.Get("owner-1", "sha-abc")
	if err != nil || !found {
		t.Fatalf("Get tras Put = (found=%v, err=%v), esperado (true, nil)", found, err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Get devolvió %q, esperado %q", got, data)
	}
}

func TestThumbnailCachePartitionadaPorPropietario(t *testing.T) {
	cache, err := NewThumbnailCache(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}

	// Mismo sha256, propietarios distintos: NUNCA deben compartir archivo
	// (cierra el oráculo de temporización cross-tenant, §34 Decisión 2).
	if err := cache.Put("duena", "sha-compartido", []byte("de la dueña")); err != nil {
		t.Fatalf("Put duena: %v", err)
	}
	if _, found, err := cache.Get("otro-usuario", "sha-compartido"); err != nil || found {
		t.Errorf("Get(otro-usuario, sha-compartido) = (found=%v, err=%v), esperado (false, nil) -- la caché no debe compartirse entre propietarios", found, err)
	}

	if err := cache.Put("otro-usuario", "sha-compartido", []byte("del otro usuario")); err != nil {
		t.Fatalf("Put otro-usuario: %v", err)
	}
	// Contenido deliberadamente distinto por propietario en este test:
	// confirma que cada Get lee SU PROPIO archivo, nunca el del otro.
	gotDuena, _, _ := cache.Get("duena", "sha-compartido")
	gotOtro, _, _ := cache.Get("otro-usuario", "sha-compartido")
	if !bytes.Equal(gotDuena, []byte("de la dueña")) {
		t.Errorf("Get(duena, ...) = %q, esperado el contenido de la dueña, sin cruzarse con el otro propietario", gotDuena)
	}
	if !bytes.Equal(gotOtro, []byte("del otro usuario")) {
		t.Errorf("Get(otro-usuario, ...) = %q, esperado el contenido del otro usuario, sin cruzarse con la dueña", gotOtro)
	}
}

func TestThumbnailCacheDedupDentroDelMismoPropietario(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewThumbnailCache(dir, 1024*1024)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}
	if err := cache.Put("duena", "sha-x", []byte("miniatura")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Confirma la ruta física exacta esperada (sharding de 2 hex).
	expected := filepath.Join(dir, "duena", "sh", "sha-x.jpg")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("no se encontró el archivo en la ruta esperada %s: %v", expected, err)
	}
}

func TestThumbnailCacheHasRoom(t *testing.T) {
	cache, err := NewThumbnailCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}
	if !cache.HasRoom() {
		t.Fatal("una caché recién creada y vacía debería tener espacio")
	}
	if err := cache.Put("owner", "sha", []byte("0123456789ABCDEF")); err != nil { // 16 bytes > maxBytes=10
		t.Fatalf("Put: %v", err)
	}
	if cache.HasRoom() {
		t.Error("tras superar maxBytes, HasRoom debería devolver false")
	}
}

func TestNewThumbnailCacheContabilizaArchivosExistentes(t *testing.T) {
	dir := t.TempDir()
	// Simula una caché con contenido de una ejecución anterior del
	// servidor: NewThumbnailCache debe recorrerla y contar lo que ya hay,
	// no arrancar siempre desde cero.
	preexisting := filepath.Join(dir, "owner-1", "ab")
	if err := os.MkdirAll(preexisting, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(preexisting, "abcdef.jpg"), []byte("0123456789"), 0o640); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cache, err := NewThumbnailCache(dir, 15)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}
	if cache.HasRoom() != true {
		t.Error("con 10 de 15 bytes ya usados, todavía debería quedar espacio")
	}
	if err := cache.Put("owner-2", "otrasha", []byte("123456")); err != nil { // 10+6=16 > 15
		t.Fatalf("Put: %v", err)
	}
	if cache.HasRoom() {
		t.Error("tras sumar los 10 preexistentes + 6 nuevos (16 > 15), HasRoom debería ser false")
	}
}

// TestThumbnailCachePutConcurrenteEsAtomico lanza muchas escrituras
// concurrentes de la MISMA miniatura y confirma que el resultado final
// nunca queda corrupto/entrelazado (hallazgo MEDIO del pase de
// seguridad: escritura de caché no atómica, §34 Decisión 2) -- el
// patrón staging+rename hace que cada escritura sea atómica de cara a un
// lector, sea cual sea el orden real de finalización.
func TestThumbnailCachePutConcurrenteEsAtomico(t *testing.T) {
	cache, err := NewThumbnailCache(t.TempDir(), 10*1024*1024)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}
	data := bytes.Repeat([]byte("miniatura-de-prueba-"), 100) // suficientemente grande para que un entrelazado sea detectable

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cache.Put("duena", "sha-concurrente", data); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Put concurrente devolvió error: %v", err)
	}

	got, found, err := cache.Get("duena", "sha-concurrente")
	if err != nil || !found {
		t.Fatalf("Get tras escrituras concurrentes = (found=%v, err=%v)", found, err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("el contenido final está corrupto/entrelazado: %d bytes leídos, %d esperados", len(got), len(data))
	}

	// No deben quedar ficheros .tmp.* huérfanos tras el rename.
	entries, err := filepath.Glob(filepath.Join(cache.path("duena", "sha-concurrente") + ".tmp.*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("quedaron %d ficheros temporales sin limpiar: %v", len(entries), entries)
	}
}
