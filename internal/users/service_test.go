package users

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
	cfg.Database.DSN = filepath.Join(t.TempDir(), "users-test.db")

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

func TestCreateUserAssignsDefaultRole(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newTestRepo(t))

	u, err := svc.CreateUser(ctx, CreateUserInput{Username: "ivan", PasswordHash: "hash"})
	if err != nil {
		t.Fatalf("CreateUser falló: %v", err)
	}
	if u.DisplayName != "ivan" {
		t.Errorf("DisplayName por defecto = %q, esperado 'ivan'", u.DisplayName)
	}

	isAdmin, err := svc.IsAdmin(ctx, u.ID)
	if err != nil {
		t.Fatalf("IsAdmin falló: %v", err)
	}
	if isAdmin {
		t.Error("un usuario creado sin rol explícito no debería ser admin")
	}
}

func TestCreateUserRejectsInvalidUsername(t *testing.T) {
	svc := NewService(newTestRepo(t))
	_, err := svc.CreateUser(context.Background(), CreateUserInput{Username: "IN VALIDO!"})
	if err != ErrInvalidUsername {
		t.Errorf("err = %v, esperado ErrInvalidUsername", err)
	}
}

func TestCreateUserRejectsDuplicateUsername(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newTestRepo(t))

	if _, err := svc.CreateUser(ctx, CreateUserInput{Username: "dup", PasswordHash: "h"}); err != nil {
		t.Fatalf("primera creación falló: %v", err)
	}
	_, err := svc.CreateUser(ctx, CreateUserInput{Username: "dup", PasswordHash: "h"})
	if err != ErrAlreadyExists {
		t.Errorf("err = %v, esperado ErrAlreadyExists", err)
	}
}

func TestIsFirstUser(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	svc := NewService(repo)

	first, err := svc.IsFirstUser(ctx)
	if err != nil {
		t.Fatalf("IsFirstUser falló: %v", err)
	}
	if !first {
		t.Error("una base de datos vacía debería reportar IsFirstUser=true")
	}

	if _, err := svc.CreateUser(ctx, CreateUserInput{Username: "primero", PasswordHash: "h", Role: RoleSuperAdmin}); err != nil {
		t.Fatalf("CreateUser falló: %v", err)
	}

	first, err = svc.IsFirstUser(ctx)
	if err != nil {
		t.Fatalf("IsFirstUser falló: %v", err)
	}
	if first {
		t.Error("tras crear un usuario, IsFirstUser debería ser false")
	}
}

func TestDisableUser(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	svc := NewService(repo)

	u, err := svc.CreateUser(ctx, CreateUserInput{Username: "temporal", PasswordHash: "h"})
	if err != nil {
		t.Fatalf("CreateUser falló: %v", err)
	}
	if err := svc.Disable(ctx, u.ID); err != nil {
		t.Fatalf("Disable falló: %v", err)
	}

	got, err := repo.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID falló: %v", err)
	}
	if got.IsActive() {
		t.Error("el usuario debería estar deshabilitado")
	}
}

func TestGetUserByUsernameNotFound(t *testing.T) {
	repo := newTestRepo(t)
	_, err := repo.GetUserByUsername(context.Background(), "no-existe")
	if err != ErrNotFound {
		t.Errorf("err = %v, esperado ErrNotFound", err)
	}
}
