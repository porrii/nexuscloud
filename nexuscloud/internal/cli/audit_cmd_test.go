package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/idgen"
)

func runAudit(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAuditCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestAuditListShowsRealEvents(t *testing.T) {
	username := newCLITestUser(t)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	actorID, err := resolveOwnerID(context.Background(), userRepo, username)
	if err != nil {
		t.Fatalf("resolveOwnerID falló: %v", err)
	}
	sqlDB.Close()

	sqlDB, repo, err := openAuditRepo(cfg, false)
	if err != nil {
		t.Fatalf("openAuditRepo falló: %v", err)
	}
	// actor_user_id tiene FK contra users(id) -- hay que usar el ID de un
	// usuario real (creado por newCLITestUser), no un string arbitrario.
	if err := repo.RecordEvent(context.Background(), &audit.Event{
		ID: idgen.New(), OccurredAt: time.Now().UTC(), EventType: audit.EventLogin,
		ActorUserID: actorID, TargetType: "user", TargetID: actorID, IP: "203.0.113.5",
		Metadata: map[string]any{"via": "test"},
	}); err != nil {
		t.Fatalf("RecordEvent falló: %v", err)
	}
	sqlDB.Close()

	out, err := runAudit(t, "list")
	if err != nil {
		t.Fatalf("audit list falló: %v", err)
	}
	if !strings.Contains(out, audit.EventLogin) || !strings.Contains(out, "203.0.113.5") {
		t.Errorf("esperaba el evento real en la salida, obtuve:\n%s", out)
	}
}

func TestAuditListEmptyWithoutEvents(t *testing.T) {
	newCLITestUser(t)

	out, err := runAudit(t, "list")
	if err != nil {
		t.Fatalf("audit list falló: %v", err)
	}
	if !strings.Contains(out, "sin eventos") {
		t.Errorf("esperaba \"(sin eventos)\", obtuve:\n%s", out)
	}
}

func TestAuditListAcceptsLimitAndOffset(t *testing.T) {
	newCLITestUser(t)
	if _, err := runAudit(t, "list", "--limit", "1", "--offset", "0"); err != nil {
		t.Fatalf("audit list --limit --offset falló: %v", err)
	}
}
