package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/server"
	"github.com/porrii/nexuscloud/internal/users"
)

// Cubre el hueco encontrado en la auditoría "todo por comandos"
// (2026-09-15): POST /groups y POST /groups/{id}/members no existían en
// ningún sitio (ni web ni CLI) aunque CreateGroup/AddUserToGroup ya vivían
// en el repositorio -- ver internal/api/v1/groups_handlers.go.

// loginToken hace login por HTTP real y devuelve el token de sesión --
// mismo camino que TestFullHappyPathFlow, factorizado aquí porque estos
// tests lo repiten varias veces.
func loginToken(t *testing.T, c *apiClient, username, password string) string {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": username, "password": password,
	}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login de %q = %d", username, resp.StatusCode)
	}
	return decodeJSON[struct {
		Token string `json:"token"`
	}](t, resp).Token
}

// createMemberUser crea un segundo usuario vía invitación (mismo camino que
// TestFullHappyPathFlow) y devuelve su ID real (consultado directamente en
// la base de datos, igual que createAdmin) junto con su token de sesión.
func createMemberUser(t *testing.T, srv *server.Server, c *apiClient, adminToken, username, password string) (userID, token string) {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/v1/invitations", map[string]any{"max_uses": 1}, adminToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear invitación = %d", resp.StatusCode)
	}
	inv := decodeJSON[struct {
		Token string `json:"token"`
	}](t, resp)

	resp = c.do(http.MethodPost, "/api/v1/invitations/redeem", map[string]string{
		"token": inv.Token, "username": username, "password": password,
	}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("redeem = %d", resp.StatusCode)
	}

	conn := db.Wrap("sqlite", srv.DB)
	u, err := users.NewSQLRepository(conn).GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("buscando usuario recién creado: %v", err)
	}
	return u.ID, loginToken(t, c, username, password)
}

func TestGroupsCreateAndAddMember(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	adminToken := loginToken(t, c, "admin", "contraseña-admin-segura")

	memberID, _ := createMemberUser(t, srv, c, adminToken, "miembro", "contraseña-del-miembro")

	resp := c.do(http.MethodPost, "/api/v1/groups", map[string]string{"name": "Contabilidad"}, adminToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear grupo = %d", resp.StatusCode)
	}
	group := decodeJSON[struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}](t, resp)
	if group.Name != "Contabilidad" {
		t.Errorf("name = %q, esperado Contabilidad", group.Name)
	}

	resp = c.do(http.MethodGet, "/api/v1/groups", nil, adminToken)
	groups := decodeJSON[[]struct {
		ID string `json:"id"`
	}](t, resp)
	found := false
	for _, g := range groups {
		if g.ID == group.ID {
			found = true
		}
	}
	if !found {
		t.Error("el grupo recién creado debería aparecer en GET /groups")
	}

	resp = c.do(http.MethodPost, "/api/v1/groups/"+group.ID+"/members", map[string]string{"user_id": memberID}, adminToken)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("añadir miembro = %d", resp.StatusCode)
	}

	conn := db.Wrap("sqlite", srv.DB)
	memberGroups, err := users.NewSQLRepository(conn).GroupsForUser(context.Background(), memberID)
	if err != nil {
		t.Fatalf("GroupsForUser falló: %v", err)
	}
	belongs := false
	for _, g := range memberGroups {
		if g.ID == group.ID {
			belongs = true
		}
	}
	if !belongs {
		t.Error("tras añadir el miembro, GroupsForUser debería incluir el grupo nuevo")
	}
}

func TestGroupsCreateRejectsDuplicateName(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	adminToken := loginToken(t, c, "admin", "contraseña-admin-segura")

	resp := c.do(http.MethodPost, "/api/v1/groups", map[string]string{"name": "Duplicado"}, adminToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("primera creación = %d", resp.StatusCode)
	}
	resp = c.do(http.MethodPost, "/api/v1/groups", map[string]string{"name": "Duplicado"}, adminToken)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("segunda creación con el mismo nombre = %d, esperado 409", resp.StatusCode)
	}
}

func TestGroupsCreateIsAdminOnly(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	adminToken := loginToken(t, c, "admin", "contraseña-admin-segura")

	_, memberToken := createMemberUser(t, srv, c, adminToken, "normal", "contraseña-normal-segura")

	resp := c.do(http.MethodPost, "/api/v1/groups", map[string]string{"name": "NoDeberíaCrearse"}, memberToken)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("un usuario normal creando un grupo = %d, esperado 403", resp.StatusCode)
	}
}

func TestGroupsAddMemberIsAdminOnly(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	adminToken := loginToken(t, c, "admin", "contraseña-admin-segura")

	resp := c.do(http.MethodPost, "/api/v1/groups", map[string]string{"name": "GrupoProtegido"}, adminToken)
	group := decodeJSON[struct {
		ID string `json:"id"`
	}](t, resp)
	memberID, memberToken := createMemberUser(t, srv, c, adminToken, "normal2", "contraseña-normal-2-segura")

	resp = c.do(http.MethodPost, "/api/v1/groups/"+group.ID+"/members", map[string]string{"user_id": memberID}, memberToken)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("un usuario normal añadiendo un miembro = %d, esperado 403", resp.StatusCode)
	}
}

func TestGroupsAddMemberRejectsUnknownGroup(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	adminToken := loginToken(t, c, "admin", "contraseña-admin-segura")
	memberID, _ := createMemberUser(t, srv, c, adminToken, "miembro2", "contraseña-del-miembro-2")

	resp := c.do(http.MethodPost, "/api/v1/groups/no-existe-este-id/members", map[string]string{"user_id": memberID}, adminToken)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("grupo inexistente = %d, esperado 404", resp.StatusCode)
	}
}

func TestGroupsAddMemberRejectsUnknownUser(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "admin", "contraseña-admin-segura")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	adminToken := loginToken(t, c, "admin", "contraseña-admin-segura")

	resp := c.do(http.MethodPost, "/api/v1/groups", map[string]string{"name": "GrupoReal"}, adminToken)
	group := decodeJSON[struct {
		ID string `json:"id"`
	}](t, resp)

	resp = c.do(http.MethodPost, "/api/v1/groups/"+group.ID+"/members", map[string]string{"user_id": "usuario-que-no-existe"}, adminToken)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("usuario inexistente = %d, esperado 404", resp.StatusCode)
	}
}
