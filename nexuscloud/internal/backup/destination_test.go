package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeInboundServer simula, en memoria y sin tocar disco, el protocolo
// HTTP que internal/api/v1/backup_remote_handlers.go expone de verdad
// (ADR-029) -- suficiente para probar remoteDestination de forma
// aislada. El receptor real (respaldado por localDestination) se prueba
// en su propio paquete, contra su propio router.
type fakeInboundServer struct {
	mu    sync.Mutex
	files map[string][]byte
	token string
}

func newFakeInboundServer(token string) *fakeInboundServer {
	return &fakeInboundServer{files: map[string][]byte{}, token: token}
}

func (s *fakeInboundServer) key(jobID, relPath string) string { return jobID + "/" + relPath }

func (s *fakeInboundServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-NexusCloud-Backup-Token") != s.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/backups/inbound/")
		jobID, tail, _ := strings.Cut(rest, "/")

		s.mu.Lock()
		defer s.mu.Unlock()

		switch {
		case tail == "complete" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		case tail == "" && r.Method == http.MethodDelete:
			for k := range s.files {
				if strings.HasPrefix(k, jobID+"/") {
					delete(s.files, k)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(tail, "files/"):
			relPath := strings.TrimPrefix(tail, "files/")
			switch r.Method {
			case http.MethodPut:
				body, _ := io.ReadAll(r.Body)
				s.files[s.key(jobID, relPath)] = body
				w.WriteHeader(http.StatusNoContent)
			case http.MethodGet:
				body, ok := s.files[s.key(jobID, relPath)]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Write(body)
			case http.MethodDelete:
				delete(s.files, s.key(jobID, relPath))
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// TestLocalDestinationConfinesPathTraversalWithinRoot confirma que
// localDestination realmente USA storage.SafeJoin (en vez de un
// filepath.Join a pelo) en cada método. SafeJoin en sí NO rechaza un
// intento de escape con un error -- lo NEUTRALIZA anclándolo dentro de
// root, como un chroot (ver TestSafeJoinBlocksPathTraversal en
// internal/storage/path_test.go, ya exhaustivo sobre esa propiedad) --
// así que WriteFile con un jobID/relPath "atacante" puede tener éxito
// perfectamente; lo único que importa de verdad es que el resultado
// nunca termine fuera de root. Se comprueba escribiendo con un
// jobID/relPath así y confirmando que el fichero resultante aparece
// DENTRO de root (recorriéndolo), nunca en otro sitio.
func TestLocalDestinationConfinesPathTraversalWithinRoot(t *testing.T) {
	root := t.TempDir()
	dest := NewLocalDestination(root)
	ctx := context.Background()

	if _, err := dest.WriteFile(ctx, "../../../etc", "passwd", strings.NewReader("contenido")); err != nil {
		t.Fatalf("WriteFile no debía fallar (SafeJoin contiene el intento, no lo rechaza): %v", err)
	}

	var found bool
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("recorriendo root: %v", err)
	}
	if !found {
		t.Fatal("el fichero escrito con un jobID/relPath \"atacante\" no aparece dentro de root -- localDestination pudo haber escapado")
	}
}

func TestResolveDestinationPicksRemoteForHTTPURLAndLocalOtherwise(t *testing.T) {
	if _, ok := resolveDestination("https://backup2.example.com:8080", "tok").(*remoteDestination); !ok {
		t.Error("una URL https:// debía resolver a *remoteDestination")
	}
	if _, ok := resolveDestination("http://backup2.example.com:8080", "tok").(*remoteDestination); !ok {
		t.Error("una URL http:// debía resolver a *remoteDestination")
	}
	if _, ok := resolveDestination(t.TempDir(), "").(*localDestination); !ok {
		t.Error("una carpeta local debía resolver a *localDestination")
	}
}

func TestRemoteDestinationWriteOpenRemoveFileRoundTrip(t *testing.T) {
	srv := newFakeInboundServer("secreto")
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	dest := resolveDestination(ts.URL, "secreto")
	ctx := context.Background()

	n, err := dest.WriteFile(ctx, "job1", "data/pool/owner/a.txt", bytes.NewReader([]byte("contenido")))
	if err != nil {
		t.Fatalf("WriteFile falló: %v", err)
	}
	if n != int64(len("contenido")) {
		t.Errorf("n = %d, esperado %d", n, len("contenido"))
	}

	r, err := dest.OpenFile(ctx, "job1", "data/pool/owner/a.txt")
	if err != nil {
		t.Fatalf("OpenFile falló: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if string(got) != "contenido" {
		t.Errorf("contenido leído = %q, esperado %q", got, "contenido")
	}

	if err := dest.RemoveFile(ctx, "job1", "data/pool/owner/a.txt"); err != nil {
		t.Fatalf("RemoveFile falló: %v", err)
	}
	if _, err := dest.OpenFile(ctx, "job1", "data/pool/owner/a.txt"); err == nil {
		t.Error("el fichero debía haber desaparecido tras RemoveFile")
	}
}

func TestRemoteDestinationRemoveJobDeletesEveryFileUnderIt(t *testing.T) {
	srv := newFakeInboundServer("secreto")
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	dest := resolveDestination(ts.URL, "secreto")
	ctx := context.Background()

	if _, err := dest.WriteFile(ctx, "job1", "manifest.json", bytes.NewReader([]byte("{}"))); err != nil {
		t.Fatalf("WriteFile(manifest.json) falló: %v", err)
	}
	if _, err := dest.WriteFile(ctx, "job1", "data/a.txt", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("WriteFile(data/a.txt) falló: %v", err)
	}

	if err := dest.RemoveJob(ctx, "job1"); err != nil {
		t.Fatalf("RemoveJob falló: %v", err)
	}
	if _, err := dest.OpenFile(ctx, "job1", "manifest.json"); err == nil {
		t.Error("manifest.json debía haber desaparecido tras RemoveJob")
	}
	if _, err := dest.OpenFile(ctx, "job1", "data/a.txt"); err == nil {
		t.Error("data/a.txt debía haber desaparecido tras RemoveJob")
	}
}

func TestRemoteDestinationRejectsWrongToken(t *testing.T) {
	srv := newFakeInboundServer("secreto")
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	dest := resolveDestination(ts.URL, "token-incorrecto")

	if _, err := dest.WriteFile(context.Background(), "job1", "manifest.json", bytes.NewReader([]byte("{}"))); err == nil {
		t.Fatal("WriteFile debía fallar con un token incorrecto")
	}
}

// TestRemoteDestinationLinkAlwaysUnsupported confirma ADR-029: un destino
// remoto nunca puede compartir inodo entre jobs -- Run ya sabe caer a
// copia completa cuando Link falla (ADR-026), así que esto basta sin
// necesitar ningún servidor de verdad.
func TestRemoteDestinationLinkAlwaysUnsupported(t *testing.T) {
	dest := &remoteDestination{baseURL: "http://example.invalid", token: "x", client: http.DefaultClient}
	if err := dest.Link(context.Background(), "job1", "job2", "data/x.txt"); !errors.Is(err, ErrLinkUnsupported) {
		t.Errorf("err = %v, esperado ErrLinkUnsupported", err)
	}
}

// TestCopyToDestinationCleansUpRemoteFileOnHashMismatch confirma que, con
// un destino remoto, un hash que no verifica limpia lo ya subido en vez
// de dejarlo dado por bueno -- mismo criterio que ya se probaba para
// destinos locales, ahora también contra un servidor real (aunque
// simulado) por HTTP.
func TestCopyToDestinationCleansUpRemoteFileOnHashMismatch(t *testing.T) {
	srv := newFakeInboundServer("secreto")
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	dest := resolveDestination(ts.URL, "secreto")

	_, _, _, err := copyToDestination(context.Background(), dest, "job1", "data/a.txt",
		bytes.NewReader([]byte("contenido real")), "sha256-que-nunca-va-a-coincidir", nil)
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("err = %v, esperado ErrIntegrityMismatch", err)
	}
	if _, err := dest.OpenFile(context.Background(), "job1", "data/a.txt"); err == nil {
		t.Error("el fichero con hash incorrecto no debía quedar en el destino remoto")
	}
}
