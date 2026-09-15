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

func runShares(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newSharesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// createExtraUser añade un SEGUNDO usuario a la misma BD que
// newCLITestUser ya dejó configurada por variables de entorno -- para
// probar compartición hace falta un propietario y un destinatario reales
// y distintos.
func createExtraUser(t *testing.T, username string) string {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()
	u, err := users.NewService(userRepo).CreateUser(context.Background(), users.CreateUserInput{
		Username: username, PasswordHash: "hash-de-prueba",
	})
	if err != nil {
		t.Fatalf("creando usuario adicional %q: %v", username, err)
	}
	return u.Username
}

func uploadRealFile(t *testing.T, username, remotePath, content string) {
	t.Helper()
	local := filepath.Join(t.TempDir(), "origen.txt")
	if err := os.WriteFile(local, []byte(content), 0o600); err != nil {
		t.Fatalf("escribiendo fichero local: %v", err)
	}
	if _, err := runFiles(t, "upload", "--username", username, local, remotePath); err != nil {
		t.Fatalf("upload falló: %v", err)
	}
}

func TestSharesCreateListRevokeUserShare(t *testing.T) {
	owner := newCLITestUser(t)
	target := createExtraUser(t, "juan")
	uploadRealFile(t, owner, "/compartido.txt", "contenido compartido")

	createOut, err := runShares(t, "create", "--username", owner, "/compartido.txt",
		"--share-type", "user", "--target-username", target)
	if err != nil {
		t.Fatalf("shares create falló: %v (salida: %s)", err, createOut)
	}
	id := ""
	for _, line := range strings.Split(createOut, "\n") {
		if strings.HasPrefix(line, "Share creado (id=") {
			id = strings.TrimSuffix(strings.TrimPrefix(line, "Share creado (id="), ")")
		}
	}
	if id == "" {
		t.Fatalf("no pude extraer el id del share de:\n%s", createOut)
	}

	byMeOut, err := runShares(t, "list", "--username", owner)
	if err != nil {
		t.Fatalf("shares list falló: %v", err)
	}
	if !strings.Contains(byMeOut, id) || !strings.Contains(byMeOut, "compartido.txt") || !strings.Contains(byMeOut, "activo") {
		t.Fatalf("esperaba el share real y activo en \"compartido por mí\", obtuve:\n%s", byMeOut)
	}

	withMeOut, err := runShares(t, "list", "--username", target, "--with-me")
	if err != nil {
		t.Fatalf("shares list --with-me falló: %v", err)
	}
	if !strings.Contains(withMeOut, id) {
		t.Fatalf("esperaba el share en \"compartido conmigo\" del destinatario, obtuve:\n%s", withMeOut)
	}

	if out, err := runShares(t, "revoke", "--username", owner, id); err != nil {
		t.Fatalf("shares revoke falló: %v (salida: %s)", err, out)
	}
	// ListSharesByOwner filtra "WHERE revoked_at IS NULL" en el propio SQL
	// (storage/share_sql_repository.go) -- "compartido por mí" no muestra
	// lo ya revocado en absoluto (vive en audit_events, no aquí), así que
	// tras revocar el ÚNICO share, el listado vuelve a quedar vacío, no
	// aparece con un estado "revocado".
	byMeOut, err = runShares(t, "list", "--username", owner)
	if err != nil {
		t.Fatalf("shares list (tras revoke) falló: %v", err)
	}
	if strings.Contains(byMeOut, id) {
		t.Fatalf("un share revocado no debería seguir apareciendo en \"compartido por mí\", obtuve:\n%s", byMeOut)
	}
	if !strings.Contains(byMeOut, "sin comparticiones") {
		t.Fatalf("esperaba \"(sin comparticiones)\" tras revocar el único share, obtuve:\n%s", byMeOut)
	}
}

func TestSharesCreateGroupShare(t *testing.T) {
	owner := newCLITestUser(t)
	member := createExtraUser(t, "miembro-grupo")
	if _, err := runUsers(t, "group", "create", "Amigos"); err != nil {
		t.Fatalf("group create falló: %v", err)
	}
	if _, err := runUsers(t, "group", "add-member", member, "Amigos"); err != nil {
		t.Fatalf("group add-member falló: %v", err)
	}
	if _, err := runFiles(t, "mkdir", "--username", owner, "/CarpetaCompartida"); err != nil {
		t.Fatalf("mkdir falló: %v", err)
	}

	createOut, err := runShares(t, "create", "--username", owner, "/CarpetaCompartida",
		"--share-type", "group", "--target-group", "Amigos")
	if err != nil {
		t.Fatalf("shares create (group) falló: %v (salida: %s)", err, createOut)
	}

	withMeOut, err := runShares(t, "list", "--username", member, "--with-me")
	if err != nil {
		t.Fatalf("shares list --with-me falló: %v", err)
	}
	if !strings.Contains(withMeOut, "CarpetaCompartida") {
		t.Fatalf("esperaba que el miembro del grupo viera la carpeta compartida vía su pertenencia, obtuve:\n%s", withMeOut)
	}
}

// TestSharesCreateLinkRejectedWhenPublicLinksDisabled confirma el
// comportamiento seguro por defecto real: sharing.publicLinksEnabled es
// "false" de fábrica (config.Defaults) y no tiene variable de entorno
// propia (a diferencia de otros campos de Storage/DB), así que este test
// no puede activarlo -- lo cual es precisamente lo que hay que probar: un
// enlace público de verdad se deja para el E2E (Tarea #16, con un
// config.yaml real generado por "config init" y editado a mano). La
// creación de shares de tipo user/group no pasa por esta comprobación
// (ver TestSharesCreateListRevokeUserShare/TestSharesCreateGroupShare).
func TestSharesCreateLinkRejectedWhenPublicLinksDisabled(t *testing.T) {
	owner := newCLITestUser(t)
	uploadRealFile(t, owner, "/enlace.txt", "contenido de enlace")

	_, err := runShares(t, "create", "--username", owner, "/enlace.txt", "--share-type", "link")
	if err == nil || !strings.Contains(err.Error(), "enlaces públicos están desactivados") {
		t.Fatalf("esperaba el error real de enlaces públicos desactivados, obtuve: %v", err)
	}
}

func TestSharesCreateRequiresShareType(t *testing.T) {
	owner := newCLITestUser(t)
	uploadRealFile(t, owner, "/x.txt", "x")
	_, err := runShares(t, "create", "--username", owner, "/x.txt")
	if err == nil || !strings.Contains(err.Error(), "share-type") {
		t.Fatalf("esperaba error sobre --share-type, obtuve: %v", err)
	}
}

func TestSharesCreateUserTypeRequiresTargetUsername(t *testing.T) {
	owner := newCLITestUser(t)
	uploadRealFile(t, owner, "/x.txt", "x")
	_, err := runShares(t, "create", "--username", owner, "/x.txt", "--share-type", "user")
	if err == nil || !strings.Contains(err.Error(), "target-username") {
		t.Fatalf("esperaba error sobre --target-username, obtuve: %v", err)
	}
}

func TestSharesListEmptyByDefault(t *testing.T) {
	owner := newCLITestUser(t)
	out, err := runShares(t, "list", "--username", owner)
	if err != nil {
		t.Fatalf("shares list falló: %v", err)
	}
	if !strings.Contains(out, "sin comparticiones") {
		t.Fatalf("esperaba \"(sin comparticiones)\", obtuve:\n%s", out)
	}
}
