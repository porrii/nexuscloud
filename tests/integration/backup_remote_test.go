// Tests de integración de los endpoints de backup entrante (ADR-029):
// arrancan un server.Server real (como el resto de este paquete) y los
// ejercitan por HTTP real, en vez de solo probar internal/backup en
// aislamiento (eso ya lo cubre internal/backup/destination_test.go contra
// un servidor HTTP simulado). Aquí se comprueba el cableado completo:
// router condicional, middleware de token, handlers, y el registro real
// en backup_jobs de esta instancia.
package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/porrii/nexuscloud/internal/backup"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/server"
)

// newTestServerWithBackupReceiveToken arranca un server.Server real igual
// que newTestServer, pero con NEXUSCLOUD_BACKUP_RECEIVE_TOKEN fijado ANTES
// de server.Build -- server.Build lee esa variable una única vez al
// construir apiv1.Handlers (ver internal/server/server.go), así que
// t.Setenv tiene que ocurrir antes de esa llamada, no después. Duplica el
// cuerpo de newTestServer (igual que ya hace TestLoginIsRateLimited en
// e2e_test.go para sus propios límites) en vez de forzar un parámetro
// nuevo en el helper compartido que no necesita ningún otro test.
func newTestServerWithBackupReceiveToken(t *testing.T, token string) (*httptest.Server, *server.Server) {
	t.Helper()
	if token != "" {
		t.Setenv("NEXUSCLOUD_BACKUP_RECEIVE_TOKEN", token)
	}
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := server.Build(cfg, logger)
	if err != nil {
		t.Fatalf("server.Build falló: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, srv
}

func TestBackupInboundRoutesAbsentWithoutReceiveTokenConfigured(t *testing.T) {
	ts, _ := newTestServerWithBackupReceiveToken(t, "") // sin token -- capacidad desactivada
	resp, err := http.Post(ts.URL+"/api/v1/backups/inbound/job1/complete", "application/octet-stream", nil)
	if err != nil {
		t.Fatalf("POST falló: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404: sin token configurado, la ruta ni debía existir", resp.StatusCode)
	}
}

func TestBackupInboundRejectsMissingOrWrongToken(t *testing.T) {
	ts, _ := newTestServerWithBackupReceiveToken(t, "secreto-correcto")

	put := func(token string) int {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/backups/inbound/job1/files/manifest.json", bytes.NewReader([]byte("{}")))
		if token != "" {
			req.Header.Set("X-NexusCloud-Backup-Token", token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT falló: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := put(""); got != http.StatusUnauthorized {
		t.Errorf("sin token: status = %d, esperado 401", got)
	}
	if got := put("secreto-incorrecto"); got != http.StatusUnauthorized {
		t.Errorf("token incorrecto: status = %d, esperado 401", got)
	}
	if got := put("secreto-correcto"); got != http.StatusNoContent {
		t.Errorf("token correcto: status = %d, esperado 204", got)
	}
}

func TestBackupInboundFullRoundTripRegistersJobLocally(t *testing.T) {
	ts, srv := newTestServerWithBackupReceiveToken(t, "secreto")
	const token = "secreto"
	baseURL := ts.URL + "/api/v1/backups/inbound/job-remoto-1"

	doWithToken := func(method, url string, body []byte) *http.Response {
		t.Helper()
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, url, reader)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("X-NexusCloud-Backup-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s falló: %v", method, url, err)
		}
		return resp
	}

	manifestJSON := []byte(`{"job_id":"job-remoto-1","pools":[{"pool_id":"p1","pool_name":"default","files":[
		{"owner_id":"o1","parent_path":"/","name":"a.txt","size_bytes":5,"sha256":"abc"}
	]}]}`)

	// Sube el fichero de datos y el manifiesto -- exactamente lo que hace
	// remoteDestination desde el origen.
	if resp := doWithToken(http.MethodPut, baseURL+"/files/data/p1/o1/a.txt", []byte("hola!")); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT del fichero de datos: status = %d", resp.StatusCode)
	}
	if resp := doWithToken(http.MethodPut, baseURL+"/files/manifest.json", manifestJSON); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT del manifiesto: status = %d", resp.StatusCode)
	}

	// Antes de "complete", el job no debe existir todavía en backup_jobs
	// (ver ADR-029: un corte de red a medias no debe parecer completado).
	conn := db.Wrap("sqlite", srv.DB)
	repo := backup.NewSQLRepository(conn)
	if _, err := repo.GetJobByID(context.Background(), "job-remoto-1"); !errors.Is(err, backup.ErrJobNotFound) {
		t.Fatalf("antes de /complete, GetJobByID = %v, esperado ErrJobNotFound", err)
	}

	if resp := doWithToken(http.MethodPost, baseURL+"/complete", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /complete: status = %d", resp.StatusCode)
	}

	job, err := repo.GetJobByID(context.Background(), "job-remoto-1")
	if err != nil {
		t.Fatalf("tras /complete, GetJobByID falló: %v", err)
	}
	if job.Status != backup.StatusCompleted || job.FileCount != 1 || job.TotalBytes != 5 {
		t.Errorf("job = %+v, esperado status=completed, file_count=1, total_bytes=5", job)
	}

	// El fichero de datos se puede releer de vuelta -- lo que restore/
	// verify/restore-to-pool del origen necesitan poder hacer.
	resp := doWithToken(http.MethodGet, baseURL+"/files/data/p1/o1/a.txt", nil)
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "hola!" {
		t.Errorf("contenido descargado = %q, esperado %q", got, "hola!")
	}

	// DELETE del job entero (retención desde el origen): ficheros y fila
	// desaparecen.
	if resp := doWithToken(http.MethodDelete, baseURL, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE del job: status = %d", resp.StatusCode)
	}
	if _, err := repo.GetJobByID(context.Background(), "job-remoto-1"); !errors.Is(err, backup.ErrJobNotFound) {
		t.Errorf("tras DELETE, GetJobByID = %v, esperado ErrJobNotFound", err)
	}
	if resp := doWithToken(http.MethodGet, baseURL+"/files/data/p1/o1/a.txt", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("tras DELETE, el fichero seguía descargándose: status = %d", resp.StatusCode)
	}
}
