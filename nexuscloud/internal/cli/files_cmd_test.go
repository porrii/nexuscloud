package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/users"
)

// newCLITestUser monta una base de datos SQLite real + un directorio de
// almacenamiento real, ambos en carpetas temporales, expuestos vía las
// mismas variables de entorno NEXUSCLOUD_* que usaría una instalación real.
// Cada subcomando de internal/cli llama a loadConfig() de forma
// independiente, así que fijar esto por entorno -- en vez de inyectar un
// *config.Config a mano -- es lo único que garantiza que todas las
// invocaciones dentro de un mismo test vean exactamente la misma base de
// datos y el mismo storage pool. Reutilizado también por
// users_cmd_test.go, de ahí el nombre genérico en vez de "Files".
func newCLITestUser(t *testing.T) string {
	t.Helper()
	t.Setenv("NEXUSCLOUD_DB_DRIVER", "sqlite")
	t.Setenv("NEXUSCLOUD_DB_DSN", filepath.Join(t.TempDir(), "cli-files-test.db"))
	t.Setenv("NEXUSCLOUD_DATA_DIR", t.TempDir())

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, true)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()

	u, err := users.NewService(userRepo).CreateUser(context.Background(), users.CreateUserInput{
		Username: "files-test-user", PasswordHash: "hash-de-prueba",
	})
	if err != nil {
		t.Fatalf("creando usuario de prueba: %v", err)
	}
	return u.Username
}

// runFiles ejecuta newFilesCmd() de forma aislada (sin pasar por newRootCmd)
// -- ningún subcomando de "files" depende del flag persistente --config de
// la raíz, así que basta con esto para probar el árbol RunE real.
func runFiles(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newFilesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestFilesRequiresUsername(t *testing.T) {
	newCLITestUser(t)
	_, err := runFiles(t, "list")
	if err == nil || !strings.Contains(err.Error(), "--username") {
		t.Fatalf("esperaba error sobre --username, obtuve: %v", err)
	}
}

func TestFilesUploadListDownloadRoundTrip(t *testing.T) {
	username := newCLITestUser(t)

	localSrc := filepath.Join(t.TempDir(), "informe.txt")
	if err := os.WriteFile(localSrc, []byte("contenido real de prueba"), 0o600); err != nil {
		t.Fatalf("escribiendo fichero local de origen: %v", err)
	}

	if out, err := runFiles(t, "upload", "--username", username, localSrc, "/informe.txt"); err != nil {
		t.Fatalf("upload falló: %v (salida: %s)", err, out)
	}

	out, err := runFiles(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("list falló: %v", err)
	}
	if !strings.Contains(out, "informe.txt") {
		t.Fatalf("esperaba \"informe.txt\" en el listado, obtuve:\n%s", out)
	}

	localDst := filepath.Join(t.TempDir(), "descargado.txt")
	if out, err := runFiles(t, "download", "--username", username, "/informe.txt", localDst); err != nil {
		t.Fatalf("download falló: %v (salida: %s)", err, out)
	}
	got, err := os.ReadFile(localDst)
	if err != nil {
		t.Fatalf("leyendo fichero descargado: %v", err)
	}
	if string(got) != "contenido real de prueba" {
		t.Fatalf("contenido descargado no coincide: %q", string(got))
	}
}

func TestFilesMkdirAndList(t *testing.T) {
	username := newCLITestUser(t)

	if out, err := runFiles(t, "mkdir", "--username", username, "/Documentos"); err != nil {
		t.Fatalf("mkdir falló: %v (salida: %s)", err, out)
	}
	out, err := runFiles(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("list falló: %v", err)
	}
	if !strings.Contains(out, "Documentos") {
		t.Fatalf("esperaba \"Documentos\" en el listado, obtuve:\n%s", out)
	}
}

func TestFilesRmMovesToTrashByDefault(t *testing.T) {
	username := newCLITestUser(t)
	localSrc := filepath.Join(t.TempDir(), "borrame.txt")
	if err := os.WriteFile(localSrc, []byte("x"), 0o600); err != nil {
		t.Fatalf("escribiendo fichero local: %v", err)
	}
	if _, err := runFiles(t, "upload", "--username", username, localSrc, "/borrame.txt"); err != nil {
		t.Fatalf("upload falló: %v", err)
	}

	if out, err := runFiles(t, "rm", "--username", username, "/borrame.txt"); err != nil {
		t.Fatalf("rm falló: %v (salida: %s)", err, out)
	}

	out, err := runFiles(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("list falló: %v", err)
	}
	if strings.Contains(out, "borrame.txt") {
		t.Fatalf("\"borrame.txt\" seguía en el listado activo tras rm:\n%s", out)
	}
}

func TestFilesRmPermanentDeletesForGood(t *testing.T) {
	username := newCLITestUser(t)
	localSrc := filepath.Join(t.TempDir(), "borrame.txt")
	if err := os.WriteFile(localSrc, []byte("x"), 0o600); err != nil {
		t.Fatalf("escribiendo fichero local: %v", err)
	}
	if _, err := runFiles(t, "upload", "--username", username, localSrc, "/borrame.txt"); err != nil {
		t.Fatalf("upload falló: %v", err)
	}

	if out, err := runFiles(t, "rm", "--username", username, "--permanent", "/borrame.txt"); err != nil {
		t.Fatalf("rm --permanent falló: %v (salida: %s)", err, out)
	}

	// Tras un borrado permanente, ni siquiera queda en la papelera: subir el
	// mismo nombre otra vez debe funcionar sin conflicto de nombre ocupado.
	if _, err := runFiles(t, "upload", "--username", username, localSrc, "/borrame.txt"); err != nil {
		t.Fatalf("re-subir tras borrado permanente falló: %v", err)
	}
}

func TestFilesMvRenamesFile(t *testing.T) {
	username := newCLITestUser(t)
	localSrc := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(localSrc, []byte("x"), 0o600); err != nil {
		t.Fatalf("escribiendo fichero local: %v", err)
	}
	if _, err := runFiles(t, "upload", "--username", username, localSrc, "/a.txt"); err != nil {
		t.Fatalf("upload falló: %v", err)
	}

	if out, err := runFiles(t, "mv", "--username", username, "/a.txt", "/b.txt"); err != nil {
		t.Fatalf("mv falló: %v (salida: %s)", err, out)
	}

	out, err := runFiles(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("list falló: %v", err)
	}
	if strings.Contains(out, "a.txt") {
		t.Fatalf("\"a.txt\" seguía en el listado tras mv:\n%s", out)
	}
	if !strings.Contains(out, "b.txt") {
		t.Fatalf("esperaba \"b.txt\" en el listado tras mv:\n%s", out)
	}
}

func TestFilesDownloadRejectsDirectoryPath(t *testing.T) {
	username := newCLITestUser(t)
	if _, err := runFiles(t, "mkdir", "--username", username, "/Carpeta"); err != nil {
		t.Fatalf("mkdir falló: %v", err)
	}

	localDst := filepath.Join(t.TempDir(), "no-deberia-existir.txt")
	_, err := runFiles(t, "download", "--username", username, "/Carpeta", localDst)
	if err == nil || !strings.Contains(err.Error(), "carpeta") {
		t.Fatalf("esperaba error indicando que es una carpeta, obtuve: %v", err)
	}
}

func TestFilesRmRejectsNonexistentPath(t *testing.T) {
	username := newCLITestUser(t)
	_, err := runFiles(t, "rm", "--username", username, "/no-existe")
	if err == nil || !strings.Contains(err.Error(), "no existe") {
		t.Fatalf("esperaba error \"no existe\", obtuve: %v", err)
	}
}
