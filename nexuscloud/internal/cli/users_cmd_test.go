package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/users"
)

// runUsers ejecuta newUsersCmd() de forma aislada, mismo patrón que
// runFiles en files_cmd_test.go.
func runUsers(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newUsersCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestUsersGroupCreateAndAddMember(t *testing.T) {
	username := newCLITestUser(t)

	if out, err := runUsers(t, "group", "create", "Contabilidad"); err != nil {
		t.Fatalf("group create falló: %v (salida: %s)", err, out)
	}

	if out, err := runUsers(t, "group", "add-member", username, "Contabilidad"); err != nil {
		t.Fatalf("group add-member falló: %v (salida: %s)", err, out)
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()

	u, err := userRepo.GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("GetUserByUsername falló: %v", err)
	}
	groups, err := userRepo.GroupsForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GroupsForUser falló: %v", err)
	}
	found := false
	for _, g := range groups {
		if g.Name == "Contabilidad" {
			found = true
		}
	}
	if !found {
		t.Errorf("esperaba que %q perteneciera a \"Contabilidad\" tras add-member, grupos: %+v", username, groups)
	}
}

func TestUsersGroupCreateRejectsDuplicateName(t *testing.T) {
	newCLITestUser(t)

	if out, err := runUsers(t, "group", "create", "Duplicado"); err != nil {
		t.Fatalf("primera creación falló: %v (salida: %s)", err, out)
	}
	_, err := runUsers(t, "group", "create", "Duplicado")
	if err == nil || !errors.Is(err, users.ErrAlreadyExists) {
		t.Fatalf("esperaba users.ErrAlreadyExists, obtuve: %v", err)
	}
}

func TestUsersGroupAddMemberRejectsUnknownGroup(t *testing.T) {
	username := newCLITestUser(t)

	_, err := runUsers(t, "group", "add-member", username, "NoExiste")
	if err == nil || !strings.Contains(err.Error(), "no encontrado") {
		t.Fatalf("esperaba error \"no encontrado\", obtuve: %v", err)
	}
}

func TestUsersGroupAddMemberRejectsUnknownUsername(t *testing.T) {
	newCLITestUser(t)
	if _, err := runUsers(t, "group", "create", "GrupoReal"); err != nil {
		t.Fatalf("group create falló: %v", err)
	}

	_, err := runUsers(t, "group", "add-member", "usuario-que-no-existe", "GrupoReal")
	if err == nil || !strings.Contains(err.Error(), "no encontrado") {
		t.Fatalf("esperaba error \"no encontrado\", obtuve: %v", err)
	}
}

// parseTOTPSecret extrae el secreto impreso por "totp enroll" (línea
// "Secreto: <valor>") para poder generar un código real con la misma
// librería pquerna/otp que ya usa el proyecto, sin tener que hardcodear ni
// mockear nada.
func parseTOTPSecret(t *testing.T, enrollOutput string) string {
	t.Helper()
	for _, line := range strings.Split(enrollOutput, "\n") {
		if strings.HasPrefix(line, "Secreto: ") {
			return strings.TrimPrefix(line, "Secreto: ")
		}
	}
	t.Fatalf("no encontré \"Secreto: \" en la salida de enroll:\n%s", enrollOutput)
	return ""
}

func TestUsersTotpEnrollVerifyDisableRoundTrip(t *testing.T) {
	username := newCLITestUser(t)

	enrollOut, err := runUsers(t, "totp", "enroll", "--username", username)
	if err != nil {
		t.Fatalf("enroll falló: %v (salida: %s)", err, enrollOut)
	}
	secret := parseTOTPSecret(t, enrollOut)

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generando código TOTP real: %v", err)
	}

	if out, err := runUsers(t, "totp", "verify", "--username", username, "--secret", secret, code); err != nil {
		t.Fatalf("verify falló: %v (salida: %s)", err, out)
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	u, err := userRepo.GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("GetUserByUsername falló: %v", err)
	}
	if !u.HasTOTP() || u.TOTPSecret != secret {
		t.Errorf("tras verify, esperaba TOTPSecret = %q, obtuve %q", secret, u.TOTPSecret)
	}
	sqlDB.Close()

	if out, err := runUsers(t, "totp", "disable", "--username", username); err != nil {
		t.Fatalf("disable falló: %v (salida: %s)", err, out)
	}

	sqlDB, userRepo, err = openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()
	u, err = userRepo.GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("GetUserByUsername falló: %v", err)
	}
	if u.HasTOTP() {
		t.Errorf("tras disable, esperaba HasTOTP() = false, TOTPSecret sigue siendo %q", u.TOTPSecret)
	}
}

func TestUsersTotpVerifyRejectsWrongCode(t *testing.T) {
	username := newCLITestUser(t)

	enrollOut, err := runUsers(t, "totp", "enroll", "--username", username)
	if err != nil {
		t.Fatalf("enroll falló: %v", err)
	}
	secret := parseTOTPSecret(t, enrollOut)

	_, err = runUsers(t, "totp", "verify", "--username", username, "--secret", secret, "000000")
	if err == nil {
		t.Fatal("esperaba que un código incorrecto fallase")
	}
}

func TestUsersTotpVerifyRequiresSecret(t *testing.T) {
	username := newCLITestUser(t)

	_, err := runUsers(t, "totp", "verify", "--username", username, "123456")
	if err == nil || !strings.Contains(err.Error(), "--secret") {
		t.Fatalf("esperaba error sobre --secret, obtuve: %v", err)
	}
}

func TestUsersEnableReactivatesDisabledUser(t *testing.T) {
	username := newCLITestUser(t)
	if _, err := runUsers(t, "disable", username); err != nil {
		t.Fatalf("disable falló: %v", err)
	}

	if out, err := runUsers(t, "enable", username); err != nil {
		t.Fatalf("enable falló: %v (salida: %s)", err, out)
	}

	listOut, err := runUsers(t, "list")
	if err != nil {
		t.Fatalf("list falló: %v", err)
	}
	if !strings.Contains(listOut, "active") {
		t.Fatalf("esperaba el usuario como \"active\" tras enable, obtuve:\n%s", listOut)
	}
	if strings.Contains(listOut, "disabled") {
		t.Fatalf("no esperaba ningún usuario \"disabled\" tras enable, obtuve:\n%s", listOut)
	}
}

func TestUsersEditUpdatesDisplayNameAndEmail(t *testing.T) {
	username := newCLITestUser(t)
	if out, err := runUsers(t, "edit", username, "--display-name", "María Real", "--email", "maria@ejemplo.test"); err != nil {
		t.Fatalf("edit falló: %v (salida: %s)", err, out)
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()
	u, err := userRepo.GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("GetUserByUsername falló: %v", err)
	}
	if u.DisplayName != "María Real" || u.Email != "maria@ejemplo.test" {
		t.Errorf("esperaba DisplayName/Email actualizados, obtuve DisplayName=%q Email=%q", u.DisplayName, u.Email)
	}
}

func TestUsersEditRequiresAtLeastOneField(t *testing.T) {
	username := newCLITestUser(t)
	_, err := runUsers(t, "edit", username)
	if err == nil || !strings.Contains(err.Error(), "display-name") {
		t.Fatalf("esperaba error pidiendo --display-name o --email, obtuve: %v", err)
	}
}

func TestUsersDeleteRequiresConfirm(t *testing.T) {
	username := newCLITestUser(t)
	_, err := runUsers(t, "delete", username)
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("esperaba error pidiendo --confirm, obtuve: %v", err)
	}

	// Sin --confirm, el usuario debe seguir existiendo.
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()
	if _, err := userRepo.GetUserByUsername(context.Background(), username); err != nil {
		t.Fatalf("el usuario no debería haberse borrado sin --confirm: %v", err)
	}
}

func TestUsersDeleteRemovesUserForReal(t *testing.T) {
	username := newCLITestUser(t)
	if out, err := runUsers(t, "delete", username, "--confirm"); err != nil {
		t.Fatalf("delete --confirm falló: %v (salida: %s)", err, out)
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()
	if _, err := userRepo.GetUserByUsername(context.Background(), username); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("esperaba users.ErrNotFound tras el borrado, obtuve: %v", err)
	}
}

func TestUsersInvitationCreateListRevoke(t *testing.T) {
	admin := newCLITestUser(t)

	createOut, err := runUsers(t, "invitation", "create", "--created-by", admin, "--max-uses", "3", "--ttl-hours", "24")
	if err != nil {
		t.Fatalf("invitation create falló: %v (salida: %s)", err, createOut)
	}
	if !strings.Contains(createOut, "Token") {
		t.Fatalf("esperaba un token en la salida de create, obtuve:\n%s", createOut)
	}
	id := ""
	for _, line := range strings.Split(createOut, "\n") {
		if strings.HasPrefix(line, "Invitación creada (id=") {
			id = strings.TrimSuffix(strings.TrimPrefix(line, "Invitación creada (id="), ")")
		}
	}
	if id == "" {
		t.Fatalf("no pude extraer el id de la invitación de:\n%s", createOut)
	}

	listOut, err := runUsers(t, "invitation", "list")
	if err != nil {
		t.Fatalf("invitation list falló: %v", err)
	}
	if !strings.Contains(listOut, id) || !strings.Contains(listOut, "usable") || !strings.Contains(listOut, "0/3") {
		t.Fatalf("esperaba la invitación real, usable, 0/3 usos, obtuve:\n%s", listOut)
	}

	if out, err := runUsers(t, "invitation", "revoke", id); err != nil {
		t.Fatalf("invitation revoke falló: %v (salida: %s)", err, out)
	}
	listOut, err = runUsers(t, "invitation", "list")
	if err != nil {
		t.Fatalf("invitation list (tras revoke) falló: %v", err)
	}
	if !strings.Contains(listOut, "revocada") {
		t.Fatalf("esperaba la invitación como \"revocada\" tras revoke, obtuve:\n%s", listOut)
	}
}

func TestUsersInvitationCreateRequiresRealCreatedBy(t *testing.T) {
	newCLITestUser(t)
	_, err := runUsers(t, "invitation", "create", "--created-by", "usuario-que-no-existe")
	if err == nil || !strings.Contains(err.Error(), "no encontrado") {
		t.Fatalf("esperaba error \"no encontrado\" para --created-by, obtuve: %v", err)
	}
}

func TestUsersInvitationCreatedTokenCanBeRedeemed(t *testing.T) {
	admin := newCLITestUser(t)

	createOut, err := runUsers(t, "invitation", "create", "--created-by", admin, "--role", users.RoleAdministrator)
	if err != nil {
		t.Fatalf("invitation create falló: %v", err)
	}
	var token string
	for _, line := range strings.Split(createOut, "\n") {
		if strings.HasPrefix(line, "Token") {
			parts := strings.SplitN(line, ": ", 2)
			if len(parts) == 2 {
				token = strings.TrimSpace(parts[1])
			}
		}
	}
	if token == "" {
		t.Fatalf("no pude extraer el token de:\n%s", createOut)
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, svc, _, _, err := openInvitationService(cfg, false)
	if err != nil {
		t.Fatalf("openInvitationService falló: %v", err)
	}
	defer sqlDB.Close()

	u, err := svc.Redeem(context.Background(), auth.RedeemInvitationInput{
		Token: token, Username: "recien-invitado", Password: "contraseña-real-segura-1",
	})
	if err != nil {
		t.Fatalf("Redeem sobre el token creado por CLI falló: %v", err)
	}
	if u.Username != "recien-invitado" {
		t.Errorf("username tras canjear = %q, esperado \"recien-invitado\"", u.Username)
	}
}

func TestUsersInvitationListEmptyByDefault(t *testing.T) {
	newCLITestUser(t)
	out, err := runUsers(t, "invitation", "list")
	if err != nil {
		t.Fatalf("invitation list falló: %v", err)
	}
	if !strings.Contains(out, "sin invitaciones") {
		t.Fatalf("esperaba \"(sin invitaciones)\", obtuve:\n%s", out)
	}
}
