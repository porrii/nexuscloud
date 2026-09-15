package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

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
