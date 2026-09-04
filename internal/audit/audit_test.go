package audit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
)

func newTestRepo(t *testing.T) Repository {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "audit-test.db")

	sqlDB, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open falló: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(cfg, sqlDB); err != nil {
		t.Fatalf("db.Migrate falló: %v", err)
	}
	return NewSQLRepository(db.Wrap(cfg.Database.Driver, sqlDB))
}

func TestRecorderPersistsEventWithMetadata(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	rec := NewRecorder(repo, nil)

	rec.Record(ctx, EventLoginFailed, "", "user", "u-123", "203.0.113.9", map[string]any{"reason": "bad_password"})

	events, err := repo.ListEvents(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("esperaba 1 evento, hay %d", len(events))
	}
	e := events[0]
	if e.EventType != EventLoginFailed || e.TargetID != "u-123" || e.IP != "203.0.113.9" {
		t.Errorf("evento inesperado: %+v", e)
	}
	if e.Metadata["reason"] != "bad_password" {
		t.Errorf("metadata = %+v, esperado reason=bad_password", e.Metadata)
	}
}

func TestListEventsOrdersNewestFirst(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	rec := NewRecorder(repo, nil)

	// actor_user_id vacío (sin actor autenticado, p.ej. login_failed) evita
	// la FK contra users(id): esta prueba solo comprueba el orden, no la
	// identidad del actor.
	rec.Record(ctx, EventLogin, "", "", "", "", nil)
	rec.Record(ctx, EventLogout, "", "", "", "", nil)

	events, err := repo.ListEvents(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	if len(events) != 2 || events[0].EventType != EventLogout {
		t.Errorf("esperaba el evento más reciente (logout) primero: %+v", events)
	}
}
