package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/idgen"
)

func runSessions(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newSessionsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// createRealSession crea una sesión real (no vía login HTTP, pero con el
// mismo repositorio que usaría un login real) para un usuario ya creado
// por newCLITestUser, y devuelve su ID.
func createRealSession(t *testing.T, username string) string {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, sessRepo, userRepo, err := openSessionRepo(cfg, false)
	if err != nil {
		t.Fatalf("openSessionRepo falló: %v", err)
	}
	defer sqlDB.Close()

	ownerID, err := resolveOwnerID(context.Background(), userRepo, username)
	if err != nil {
		t.Fatalf("resolveOwnerID falló: %v", err)
	}
	sess := &auth.Session{
		ID: idgen.New(), UserID: ownerID, TokenHash: auth.HashToken("token-de-prueba-" + idgen.New()),
		Device: "navegador de prueba", IP: "203.0.113.9",
		CreatedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if err := sessRepo.CreateSession(context.Background(), sess); err != nil {
		t.Fatalf("CreateSession falló: %v", err)
	}
	return sess.ID
}

func TestSessionsListShowsRealSession(t *testing.T) {
	username := newCLITestUser(t)
	sessionID := createRealSession(t, username)

	out, err := runSessions(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("sessions list falló: %v", err)
	}
	if !strings.Contains(out, sessionID) || !strings.Contains(out, "203.0.113.9") || !strings.Contains(out, "activa") {
		t.Errorf("esperaba la sesión real (activa) en la salida, obtuve:\n%s", out)
	}
}

func TestSessionsRevokeMarksSessionRevoked(t *testing.T) {
	username := newCLITestUser(t)
	sessionID := createRealSession(t, username)

	if out, err := runSessions(t, "revoke", "--username", username, sessionID); err != nil {
		t.Fatalf("sessions revoke falló: %v (salida: %s)", err, out)
	}

	out, err := runSessions(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("sessions list falló: %v", err)
	}
	if !strings.Contains(out, "revocada") {
		t.Errorf("esperaba la sesión como \"revocada\" tras revoke, obtuve:\n%s", out)
	}
}

func TestSessionsRevokeRejectsUnknownSession(t *testing.T) {
	username := newCLITestUser(t)

	_, err := runSessions(t, "revoke", "--username", username, "sesion-que-no-existe")
	if err == nil {
		t.Fatal("esperaba que revocar una sesión inexistente fallase")
	}
}

func TestSessionsListEmptyForUserWithoutSessions(t *testing.T) {
	username := newCLITestUser(t)

	out, err := runSessions(t, "list", "--username", username)
	if err != nil {
		t.Fatalf("sessions list falló: %v", err)
	}
	if !strings.Contains(out, "sin sesiones") {
		t.Errorf("esperaba \"(sin sesiones)\", obtuve:\n%s", out)
	}
}
