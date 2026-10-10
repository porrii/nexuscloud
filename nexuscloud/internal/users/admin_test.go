package users

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Reglas de administración de cuentas (ADR-042, B1).

type fakePublications struct{ links, anon, uploads int }

func (f fakePublications) CountPublicationsForOwner(context.Context, string) (int, int, int, error) {
	return f.links, f.anon, f.uploads, nil
}

func mustCreate(t *testing.T, svc *Service, username, role string) *User {
	t.Helper()
	u, err := svc.CreateUser(context.Background(), CreateUserInput{Username: username, PasswordHash: "x", Role: role})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return u
}

func TestSetRoleReplacesAllRolesWithOne(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	svc := NewService(repo, WithPublicationCounter(fakePublications{}))
	root := mustCreate(t, svc, "raiz", RoleSuperAdmin)
	ana := mustCreate(t, svc, "ana", RoleUser)
	// Una cuenta antigua podía acumular roles con AssignRole.
	if err := repo.AssignRole(ctx, ana.ID, RoleAdministrator); err != nil {
		t.Fatal(err)
	}

	previous, err := svc.SetRole(ctx, root.ID, ana.ID, RoleReadOnly)
	if err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	if previous != RoleAdministrator {
		t.Errorf("rol anterior = %q, esperado administrator (gana sobre user)", previous)
	}
	roles, err := repo.RolesForUser(ctx, ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].ID != RoleReadOnly {
		t.Errorf("roles tras SetRole = %+v, esperado solo read_only", roles)
	}
}

func TestSetRoleRejectsOwnRoleAndUnknownRole(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newTestRepo(t), WithPublicationCounter(fakePublications{}))
	root := mustCreate(t, svc, "raiz", RoleSuperAdmin)
	ana := mustCreate(t, svc, "ana", RoleUser)

	if _, err := svc.SetRole(ctx, root.ID, root.ID, RoleUser); !errors.Is(err, ErrSelfRoleChange) {
		t.Errorf("cambiar el propio rol: err = %v, esperado ErrSelfRoleChange", err)
	}
	for _, role := range []string{"", "dios"} {
		if _, err := svc.SetRole(ctx, root.ID, ana.ID, role); !errors.Is(err, ErrInvalidRole) {
			t.Errorf("rol %q: err = %v, esperado ErrInvalidRole", role, err)
		}
	}
}

func TestOnlySuperAdminTouchesSuperAdmin(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newTestRepo(t), WithPublicationCounter(fakePublications{}))
	root := mustCreate(t, svc, "raiz", RoleSuperAdmin)
	other := mustCreate(t, svc, "otra-raiz", RoleSuperAdmin)
	admin := mustCreate(t, svc, "admin", RoleAdministrator)
	ana := mustCreate(t, svc, "ana", RoleUser)

	if _, err := svc.SetRole(ctx, admin.ID, ana.ID, RoleSuperAdmin); !errors.Is(err, ErrSuperAdminProtected) {
		t.Errorf("administrator concede super_admin: err = %v", err)
	}
	if _, err := svc.SetRole(ctx, admin.ID, other.ID, RoleUser); !errors.Is(err, ErrSuperAdminProtected) {
		t.Errorf("administrator degrada a un super_admin: err = %v", err)
	}
	if err := svc.EnsureCanManage(ctx, admin.ID, other.ID); !errors.Is(err, ErrSuperAdminProtected) {
		t.Errorf("administrator gestiona a un super_admin: err = %v", err)
	}
	if err := svc.EnsureCanManage(ctx, admin.ID, ana.ID); err != nil {
		t.Errorf("administrator gestiona a un user: err = %v", err)
	}
	// Un super_admin sí puede, y un administrator puede tocar a otro.
	if _, err := svc.SetRole(ctx, root.ID, ana.ID, RoleSuperAdmin); err != nil {
		t.Errorf("super_admin concede super_admin: %v", err)
	}
	if _, err := svc.SetRole(ctx, root.ID, other.ID, RoleAdministrator); err != nil {
		t.Errorf("super_admin degrada a otro super_admin: %v", err)
	}
}

func TestNeverZeroActiveSuperAdmins(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	svc := NewService(repo, WithPublicationCounter(fakePublications{}))
	root := mustCreate(t, svc, "raiz", RoleSuperAdmin)
	other := mustCreate(t, svc, "otra-raiz", RoleSuperAdmin)

	// Con dos, se puede degradar a uno (desde la CLI: actor vacío).
	if _, err := svc.SetRole(ctx, "", other.ID, RoleUser); err != nil {
		t.Fatalf("degradar con otro super_admin activo: %v", err)
	}
	// El que queda es el último: ni degradarlo, ni desactivarlo, ni borrarlo.
	if _, err := svc.SetRole(ctx, "", root.ID, RoleAdministrator); !errors.Is(err, ErrLastSuperAdmin) {
		t.Errorf("degradar al último super_admin: err = %v", err)
	}
	if err := svc.EnsureNotLastSuperAdmin(ctx, root.ID); !errors.Is(err, ErrLastSuperAdmin) {
		t.Errorf("EnsureNotLastSuperAdmin del último: err = %v", err)
	}
	if err := svc.EnsureNotLastSuperAdmin(ctx, other.ID); err != nil {
		t.Errorf("EnsureNotLastSuperAdmin de un user: err = %v", err)
	}

	// Un super_admin DESACTIVADO no cuenta como respaldo.
	third := mustCreate(t, svc, "tercera", RoleSuperAdmin)
	third.Status = StatusDisabled
	third.UpdatedAt = time.Now().UTC()
	if err := repo.UpdateUser(ctx, third); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureNotLastSuperAdmin(ctx, root.ID); !errors.Is(err, ErrLastSuperAdmin) {
		t.Errorf("con el otro super_admin desactivado: err = %v, esperado ErrLastSuperAdmin", err)
	}
}

func TestSetRoleToReadOnlyRefusedWithPublications(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	root := mustCreate(t, NewService(repo), "raiz", RoleSuperAdmin)
	ana := mustCreate(t, NewService(repo), "ana", RoleUser)

	svc := NewService(repo, WithPublicationCounter(fakePublications{links: 2, uploads: 1}))
	_, err := svc.SetRole(ctx, root.ID, ana.ID, RoleReadOnly)
	var pubErr *PublicationsError
	if !errors.As(err, &pubErr) || !errors.Is(err, ErrHasPublications) {
		t.Fatalf("err = %v, esperado *PublicationsError", err)
	}
	if pubErr.Counts != (PublicationCounts{PublicLinks: 2, UploadShares: 1}) {
		t.Errorf("recuento = %+v", pubErr.Counts)
	}
	if role, _ := svc.PrimaryRole(ctx, ana.ID); role != RoleUser {
		t.Errorf("el rol cambió pese al rechazo: %q", role)
	}

	// Sin contador configurado, falla cerrado.
	if _, err := NewService(repo).SetRole(ctx, root.ID, ana.ID, RoleReadOnly); err == nil {
		t.Error("sin PublicationCounter el paso a read_only debería rechazarse")
	}
}

func TestGroupMembersRenameAndDelete(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	svc := NewService(repo)
	ana := mustCreate(t, svc, "ana", RoleUser)
	bea := mustCreate(t, svc, "bea", RoleUser)
	g := &Group{ID: "g1", Name: "equipo", CreatedAt: time.Now().UTC()}
	if err := repo.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateGroup(ctx, &Group{ID: "g2", Name: "otro", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*User{ana, bea} {
		if err := repo.AddUserToGroup(ctx, u.ID, g.ID); err != nil {
			t.Fatal(err)
		}
	}

	members, err := svc.ListGroupMembers(ctx, g.ID)
	if err != nil || len(members) != 2 || members[0].Username != "ana" {
		t.Fatalf("miembros = %v, %v", members, err)
	}
	if err := svc.RemoveGroupMember(ctx, g.ID, ana.ID); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if err := svc.RemoveGroupMember(ctx, g.ID, ana.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("quitar a quien ya no es miembro: err = %v", err)
	}

	if err := svc.RenameGroup(ctx, g.ID, "otro"); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("renombrar a un nombre ocupado: err = %v", err)
	}
	if err := svc.RenameGroup(ctx, g.ID, "   "); !errors.Is(err, ErrInvalidGroupName) {
		t.Errorf("nombre vacío: err = %v", err)
	}
	if err := svc.RenameGroup(ctx, g.ID, "  diseño "); err != nil {
		t.Fatalf("RenameGroup: %v", err)
	}
	if got, _ := repo.GetGroupByID(ctx, g.ID); got.Name != "diseño" {
		t.Errorf("nombre tras renombrar = %q", got.Name)
	}

	if err := svc.DeleteGroup(ctx, g.ID); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if _, err := svc.ListGroupMembers(ctx, g.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("miembros de un grupo borrado: err = %v", err)
	}
	if groups, _ := repo.GroupsForUser(ctx, bea.ID); len(groups) != 0 {
		t.Errorf("la membresía no se borró en cascada: %+v", groups)
	}
}

func TestRoleChangeNeedsReauth(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{RoleUser, RoleReadOnly, false},
		{RoleReadOnly, RoleUser, false},
		{RoleUser, RoleAdministrator, true},
		{RoleAdministrator, RoleUser, true},
		{RoleSuperAdmin, RoleAdministrator, true},
	}
	for _, c := range cases {
		if got := RoleChangeNeedsReauth(c.from, c.to); got != c.want {
			t.Errorf("RoleChangeNeedsReauth(%s, %s) = %v, esperado %v", c.from, c.to, got, c.want)
		}
	}
}
