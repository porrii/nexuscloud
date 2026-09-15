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

func TestFilesTrashListAndRestoreFile(t *testing.T) {
	username := newCLITestUser(t)
	localSrc := filepath.Join(t.TempDir(), "papelera.txt")
	if err := os.WriteFile(localSrc, []byte("x"), 0o600); err != nil {
		t.Fatalf("escribiendo fichero local: %v", err)
	}
	if _, err := runFiles(t, "upload", "--username", username, localSrc, "/papelera.txt"); err != nil {
		t.Fatalf("upload falló: %v", err)
	}
	if _, err := runFiles(t, "rm", "--username", username, "/papelera.txt"); err != nil {
		t.Fatalf("rm falló: %v", err)
	}

	listOut, err := runFiles(t, "trash", "list", "--username", username)
	if err != nil {
		t.Fatalf("trash list falló: %v", err)
	}
	if !strings.Contains(listOut, "papelera.txt") {
		t.Fatalf("esperaba \"papelera.txt\" en la papelera, obtuve:\n%s", listOut)
	}
	id := firstColumn(t, listOut)

	if out, err := runFiles(t, "trash", "restore", "--username", username, id); err != nil {
		t.Fatalf("trash restore falló: %v (salida: %s)", err, out)
	}

	out, err := runFiles(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("list falló: %v", err)
	}
	if !strings.Contains(out, "papelera.txt") {
		t.Fatalf("esperaba \"papelera.txt\" de vuelta en el listado activo, obtuve:\n%s", out)
	}
}

func TestFilesTrashListAndRestoreDirectory(t *testing.T) {
	username := newCLITestUser(t)
	if _, err := runFiles(t, "mkdir", "--username", username, "/CarpetaBorrada"); err != nil {
		t.Fatalf("mkdir falló: %v", err)
	}
	if _, err := runFiles(t, "rm", "--username", username, "/CarpetaBorrada"); err != nil {
		t.Fatalf("rm falló: %v", err)
	}

	listOut, err := runFiles(t, "trash", "list", "--username", username)
	if err != nil {
		t.Fatalf("trash list falló: %v", err)
	}
	if !strings.Contains(listOut, "CarpetaBorrada") {
		t.Fatalf("esperaba \"CarpetaBorrada\" en la papelera, obtuve:\n%s", listOut)
	}
	id := firstColumn(t, listOut)

	if out, err := runFiles(t, "trash", "restore", "--username", username, id); err != nil {
		t.Fatalf("trash restore falló: %v (salida: %s)", err, out)
	}
	out, err := runFiles(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("list falló: %v", err)
	}
	if !strings.Contains(out, "CarpetaBorrada") {
		t.Fatalf("esperaba \"CarpetaBorrada\" de vuelta en el listado activo, obtuve:\n%s", out)
	}
}

func TestFilesTrashRestoreRejectsUnknownID(t *testing.T) {
	username := newCLITestUser(t)
	_, err := runFiles(t, "trash", "restore", "--username", username, "id-que-no-existe")
	if err == nil || !strings.Contains(err.Error(), "no encuentro") {
		t.Fatalf("esperaba error \"no encuentro\", obtuve: %v", err)
	}
}

func TestFilesTrashListEmptyByDefault(t *testing.T) {
	username := newCLITestUser(t)
	out, err := runFiles(t, "trash", "list", "--username", username)
	if err != nil {
		t.Fatalf("trash list falló: %v", err)
	}
	if !strings.Contains(out, "vacía") {
		t.Fatalf("esperaba \"(papelera vacía)\", obtuve:\n%s", out)
	}
}

func TestFilesVersionsListDownloadAndRestore(t *testing.T) {
	username := newCLITestUser(t)
	dir := t.TempDir()
	v1 := filepath.Join(dir, "v1.txt")
	v2 := filepath.Join(dir, "v2.txt")
	if err := os.WriteFile(v1, []byte("contenido versión 1"), 0o600); err != nil {
		t.Fatalf("escribiendo v1: %v", err)
	}
	if err := os.WriteFile(v2, []byte("contenido versión 2, más largo"), 0o600); err != nil {
		t.Fatalf("escribiendo v2: %v", err)
	}
	if _, err := runFiles(t, "upload", "--username", username, v1, "/versionado.txt"); err != nil {
		t.Fatalf("primer upload falló: %v", err)
	}
	if _, err := runFiles(t, "upload", "--username", username, v2, "/versionado.txt"); err != nil {
		t.Fatalf("segundo upload falló: %v", err)
	}

	listOut, err := runFiles(t, "versions", "list", "--username", username, "/versionado.txt")
	if err != nil {
		t.Fatalf("versions list falló: %v", err)
	}
	if !strings.Contains(listOut, "1") {
		t.Fatalf("esperaba la versión 1 en el listado, obtuve:\n%s", listOut)
	}

	downloaded := filepath.Join(dir, "descargada-v1.txt")
	if out, err := runFiles(t, "versions", "download", "--username", username, "/versionado.txt", "1", downloaded); err != nil {
		t.Fatalf("versions download falló: %v (salida: %s)", err, out)
	}
	got, err := os.ReadFile(downloaded)
	if err != nil {
		t.Fatalf("leyendo la versión descargada: %v", err)
	}
	if string(got) != "contenido versión 1" {
		t.Fatalf("contenido de la versión 1 descargada no coincide: %q", string(got))
	}

	if out, err := runFiles(t, "versions", "restore", "--username", username, "/versionado.txt", "1"); err != nil {
		t.Fatalf("versions restore falló: %v (salida: %s)", err, out)
	}
	restoredLocal := filepath.Join(dir, "tras-restaurar.txt")
	if _, err := runFiles(t, "download", "--username", username, "/versionado.txt", restoredLocal); err != nil {
		t.Fatalf("download tras restore falló: %v", err)
	}
	got, err = os.ReadFile(restoredLocal)
	if err != nil {
		t.Fatalf("leyendo el fichero tras restaurar: %v", err)
	}
	if string(got) != "contenido versión 1" {
		t.Fatalf("tras \"versions restore 1\", el contenido activo debería volver a ser el de la versión 1, obtuve: %q", string(got))
	}
}

func TestFilesVersionsListRejectsDirectory(t *testing.T) {
	username := newCLITestUser(t)
	if _, err := runFiles(t, "mkdir", "--username", username, "/UnaCarpeta"); err != nil {
		t.Fatalf("mkdir falló: %v", err)
	}
	_, err := runFiles(t, "versions", "list", "--username", username, "/UnaCarpeta")
	if err == nil || !strings.Contains(err.Error(), "carpeta") {
		t.Fatalf("esperaba error indicando que es una carpeta, obtuve: %v", err)
	}
}

// firstColumn extrae el primer campo (separado por espacios) de la
// PRIMERA línea de datos real de una tabla impresa por el CLI -- se salta
// la cabecera (empieza por "ID") y cualquier línea vacía.
func firstColumn(t *testing.T, tableOutput string) string {
	t.Helper()
	for _, line := range strings.Split(tableOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ID ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		return fields[0]
	}
	t.Fatalf("no encontré ninguna fila de datos en la tabla:\n%s", tableOutput)
	return ""
}
