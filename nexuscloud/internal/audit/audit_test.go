package audit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/users"
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

// newTestRepoWithUser es como newTestRepo, pero además crea un usuario real
// y devuelve su ID junto con la conexión (para poder crear más usuarios en
// la MISMA base de datos con createTestUser) -- actor_user_id tiene FK
// contra users(id), así que ListEventsForActor (que sí filtra por actor) no
// puede probarse con un string arbitrario como newTestRepo hace para otros
// tests que no miran el actor.
func newTestRepoWithUser(t *testing.T, username string) (repo Repository, conn *db.Conn, userID string) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "audit-actor-test.db")

	sqlDB, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open falló: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(cfg, sqlDB); err != nil {
		t.Fatalf("db.Migrate falló: %v", err)
	}
	conn = db.Wrap(cfg.Database.Driver, sqlDB)
	return NewSQLRepository(conn), conn, createTestUser(t, conn, username)
}

func createTestUser(t *testing.T, conn *db.Conn, username string) string {
	t.Helper()
	u, err := users.NewService(users.NewSQLRepository(conn)).CreateUser(context.Background(), users.CreateUserInput{
		Username: username, PasswordHash: "hash-de-prueba",
	})
	if err != nil {
		t.Fatalf("creando usuario de prueba %q: %v", username, err)
	}
	return u.ID
}

func TestListEventsForActorFiltersByActorAndType(t *testing.T) {
	ctx := context.Background()
	repo, conn, ana := newTestRepoWithUser(t, "ana")
	bea := createTestUser(t, conn, "bea") // MISMA base de datos que ana
	rec := NewRecorder(repo, nil)

	rec.Record(ctx, EventUpload, ana, "file", "f1", "", map[string]any{"name": "a.txt"})
	rec.Record(ctx, EventLogin, ana, "", "", "", nil)        // mismo actor, tipo NO incluido en el filtro
	rec.Record(ctx, EventDelete, ana, "file", "f2", "", nil) // mismo actor, tipo SÍ incluido
	rec.Record(ctx, EventUpload, bea, "file", "f3", "", nil) // mismo tipo, OTRO actor -- no debe salir

	events, err := repo.ListEventsForActor(ctx, ana, []string{EventUpload, EventDelete}, 10, 0)
	if err != nil {
		t.Fatalf("ListEventsForActor: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("esperaba 2 eventos (upload+delete de ana, sin el login ni el de bea), hay %d: %+v", len(events), events)
	}
	// Más reciente primero: delete se registró después que upload.
	if events[0].EventType != EventDelete || events[1].EventType != EventUpload {
		t.Errorf("orden inesperado: %+v", events)
	}
	for _, e := range events {
		if e.ActorUserID != ana {
			t.Errorf("evento de otro actor coló en el filtro: %+v", e)
		}
	}
}

func TestListEventsForActorWithNoEventTypesReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	repo, _, ana := newTestRepoWithUser(t, "ana")

	events, err := repo.ListEventsForActor(ctx, ana, nil, 10, 0)
	if err != nil {
		t.Fatalf("ListEventsForActor con eventTypes vacío: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("esperaba una lista vacía sin tipos que filtrar, hay %d", len(events))
	}
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
