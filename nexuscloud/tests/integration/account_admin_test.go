package integration

// Administración de cuentas por HTTP (ADR-042, B1): modo «sudo», roles,
// restablecer contraseña, reglas entre administradores, grupos,
// capacidades y el contrato JSON de la auditoría.

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
)

const adminPassword = "contrasena-larga-123"

func reauth(t *testing.T, c *apiClient, token, password string) *http.Response {
	t.Helper()
	return c.do(http.MethodPost, "/api/v1/auth/reauthenticate", map[string]string{"password": password}, token)
}

func mustReauth(t *testing.T, c *apiClient, token, password string) {
	t.Helper()
	resp := reauth(t, c, token, password)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("reautenticar: status = %d, cuerpo = %s", resp.StatusCode, b)
	}
}

func TestDeleteUserRequiresRecentReauthentication(t *testing.T) {
	e := newQuotaEnv(t, 0)

	wantErrorCode(t, e.c.do(http.MethodDelete, "/api/v1/users/"+e.memberID, nil, e.admin), http.StatusForbidden, "reauth_required")
	// Contraseña incorrecta: 403 (no 401, que haría al cliente cerrar sesión).
	wantErrorCode(t, reauth(t, e.c, e.admin, "no-es-la-contrasena"), http.StatusForbidden, "unauthorized")

	mustReauth(t, e.c, e.admin, adminPassword)
	resp := e.c.do(http.MethodDelete, "/api/v1/users/"+e.memberID, nil, e.admin)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("borrar tras reautenticar: status = %d, esperado 204", resp.StatusCode)
	}
}

func TestAPITokenCannotPassReauthentication(t *testing.T) {
	e := newQuotaEnv(t, 0)
	resp := e.c.do(http.MethodPost, "/api/v1/auth/api-tokens", map[string]any{"label": "script"}, e.admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear token de API = %d", resp.StatusCode)
	}
	apiToken := decodeJSON[struct {
		Token string `json:"token"`
	}](t, resp).Token

	wantErrorCode(t, reauth(t, e.c, apiToken, adminPassword), http.StatusForbidden, "reauth_unavailable")
	wantErrorCode(t, e.c.do(http.MethodDelete, "/api/v1/users/"+e.memberID, nil, apiToken), http.StatusForbidden, "reauth_unavailable")
}

func TestResetPasswordClosesTheOtherAccountsSessions(t *testing.T) {
	e := newQuotaEnv(t, 0)
	body := map[string]string{"password": "contrasena-nueva-del-miembro"}

	wantErrorCode(t, e.c.do(http.MethodPost, "/api/v1/users/"+e.memberID+"/password", body, e.admin), http.StatusForbidden, "reauth_required")
	mustReauth(t, e.c, e.admin, adminPassword)
	resp := e.c.do(http.MethodPost, "/api/v1/users/"+e.memberID+"/password", body, e.admin)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restablecer contraseña: status = %d", resp.StatusCode)
	}

	me := e.c.do(http.MethodGet, "/api/v1/users/me", nil, e.member)
	me.Body.Close()
	if me.StatusCode != http.StatusUnauthorized {
		t.Errorf("la sesión del miembro sigue valiendo: status = %d", me.StatusCode)
	}
	loginToken(t, e.c, "miembro", "contrasena-nueva-del-miembro")
}

type roleUserDTO struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func TestSetRoleAndRoleInResponses(t *testing.T) {
	e := newQuotaEnv(t, 0)
	path := "/api/v1/users/" + e.memberID + "/role"

	// user → read_only no toca privilegios: sin reautenticación.
	resp := e.c.do(http.MethodPut, path, map[string]string{"role": "read_only"}, e.admin)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("pasar a read_only: status = %d, cuerpo = %s", resp.StatusCode, b)
	}
	if got := decodeJSON[roleUserDTO](t, resp).Role; got != "read_only" {
		t.Errorf("rol en la respuesta = %q", got)
	}

	// Hacia administrador, sí.
	wantErrorCode(t, e.c.do(http.MethodPut, path, map[string]string{"role": "administrator"}, e.admin), http.StatusForbidden, "reauth_required")
	mustReauth(t, e.c, e.admin, adminPassword)
	resp = e.c.do(http.MethodPut, path, map[string]string{"role": "administrator"}, e.admin)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pasar a administrator tras reautenticar: status = %d", resp.StatusCode)
	}

	wantErrorCode(t, e.c.do(http.MethodPut, "/api/v1/users/"+e.adminID+"/role", map[string]string{"role": "user"}, e.admin), http.StatusBadRequest, "invalid_request")
	wantErrorCode(t, e.c.do(http.MethodPut, path, map[string]string{"role": "dios"}, e.admin), http.StatusBadRequest, "invalid_role")

	list := decodeJSON[[]roleUserDTO](t, e.c.do(http.MethodGet, "/api/v1/users", nil, e.admin))
	roles := map[string]string{}
	for _, u := range list {
		roles[u.Username] = u.Role
	}
	if roles["duena"] != "super_admin" || roles["miembro"] != "administrator" {
		t.Errorf("roles en GET /users = %v", roles)
	}
}

func TestAdministratorCannotTouchSuperAdmin(t *testing.T) {
	e := newQuotaEnv(t, 0)
	resp := e.c.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "jefa", "password": "contrasena-de-jefa", "role": "administrator",
	}, e.admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear administradora = %d", resp.StatusCode)
	}
	resp.Body.Close()
	jefa := loginToken(t, e.c, "jefa", "contrasena-de-jefa")

	wantErrorCode(t, e.c.do(http.MethodPatch, "/api/v1/users/"+e.adminID, map[string]any{"display_name": "x"}, jefa), http.StatusForbidden, "super_admin_protected")
	wantErrorCode(t, e.c.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "otra-raiz", "password": "contrasena-raiz-2", "role": "super_admin",
	}, jefa), http.StatusForbidden, "super_admin_protected")
	wantErrorCode(t, e.c.do(http.MethodPost, "/api/v1/invitations", map[string]any{"role": "super_admin"}, jefa), http.StatusForbidden, "super_admin_protected")
	mustReauth(t, e.c, jefa, "contrasena-de-jefa")
	wantErrorCode(t, e.c.do(http.MethodDelete, "/api/v1/users/"+e.adminID, nil, jefa), http.StatusForbidden, "super_admin_protected")

	// Pero sí gestiona a un usuario normal.
	resp = e.c.do(http.MethodGet, "/api/v1/users/"+e.memberID+"/sessions", nil, jefa)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("listar sesiones de un user: status = %d", resp.StatusCode)
	}
}

func TestReadOnlyRefusedWhileTheAccountHasPublications(t *testing.T) {
	e := newSharedUploadEnv(t)
	ownerID := decodeJSON[roleUserDTO](t, e.c.do(http.MethodGet, "/api/v1/users/me", nil, e.owner)).ID
	// La dueña comparte con subida; un segundo super_admin intenta pasarla a
	// read_only (nadie cambia su propio rol).
	e.shareWithMember(e.dir.ID, map[string]any{"can_upload": true})
	createAdmin(t, e.srv, "segunda", "contrasena-segunda")
	second := loginToken(t, e.c, "segunda", "contrasena-segunda")

	resp := e.c.do(http.MethodPut, "/api/v1/users/"+ownerID+"/role", map[string]string{"role": "read_only"}, second)
	if resp.StatusCode != http.StatusForbidden {
		// super_admin → read_only exige reautenticación primero.
		t.Fatalf("sin reautenticar: status = %d, esperado 403", resp.StatusCode)
	}
	resp.Body.Close()
	mustReauth(t, e.c, second, "contrasena-segunda")
	resp = e.c.do(http.MethodPut, "/api/v1/users/"+ownerID+"/role", map[string]string{"role": "read_only"}, second)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("con comparticiones de subida: status = %d, esperado 409", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code   string         `json:"code"`
			Counts map[string]int `json:"counts"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "has_publications" || body.Error.Counts["upload_shares"] != 1 {
		t.Errorf("respuesta = %+v", body.Error)
	}
}

func TestDeleteGroupNeedsConfirmationOfLostShares(t *testing.T) {
	e := newSharedUploadEnv(t)
	resp := e.c.do(http.MethodPost, "/api/v1/groups", map[string]any{"name": "equipo"}, e.owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear grupo = %d", resp.StatusCode)
	}
	group := decodeJSON[struct {
		ID string `json:"id"`
	}](t, resp)
	resp = e.c.do(http.MethodPost, "/api/v1/groups/"+group.ID+"/members", map[string]any{"user_id": e.memberID}, e.owner)
	resp.Body.Close()
	e.share(map[string]any{"resource_type": "directory", "resource_id": e.dir.ID, "share_type": "group", "target_group_id": group.ID})

	members := decodeJSON[[]roleUserDTO](t, e.c.do(http.MethodGet, "/api/v1/groups/"+group.ID+"/members", nil, e.owner))
	if len(members) != 1 || members[0].Username != "miembro" {
		t.Errorf("miembros = %+v", members)
	}

	resp = e.c.do(http.MethodPatch, "/api/v1/groups/"+group.ID, map[string]any{"name": "diseño"}, e.owner)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("renombrar: status = %d", resp.StatusCode)
	}

	mustReauth(t, e.c, e.owner, adminPassword)
	resp = e.c.do(http.MethodDelete, "/api/v1/groups/"+group.ID, nil, e.owner)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("borrar sin confirmar: status = %d, esperado 409", resp.StatusCode)
	}
	confirm := decodeJSON[struct {
		Error struct {
			Code   string `json:"code"`
			Shares int    `json:"shares"`
		} `json:"error"`
	}](t, resp)
	if confirm.Error.Code != "confirm_required" || confirm.Error.Shares != 1 {
		t.Fatalf("confirmación = %+v", confirm.Error)
	}
	resp = e.c.do(http.MethodDelete, "/api/v1/groups/"+group.ID+"?expected_shares="+strconv.Itoa(confirm.Error.Shares), nil, e.owner)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("borrar confirmado: status = %d", resp.StatusCode)
	}
	shared := decodeJSON[[]shareDTO](t, e.c.do(http.MethodGet, "/api/v1/shares?direction=with-me", nil, e.member))
	if len(shared) != 0 {
		t.Errorf("el miembro sigue viendo la compartición del grupo borrado: %+v", shared)
	}
}

func TestMeAdvertisesCapabilitiesAndAuditUsesSnakeCase(t *testing.T) {
	e := newQuotaEnv(t, 0)
	me := decodeJSON[struct {
		Role          string   `json:"role"`
		ServerVersion string   `json:"server_version"`
		Capabilities  []string `json:"capabilities"`
	}](t, e.c.do(http.MethodGet, "/api/v1/users/me", nil, e.admin))
	if me.Role != "super_admin" || me.ServerVersion == "" {
		t.Errorf("/users/me = %+v", me)
	}
	want := map[string]bool{"read_only_role": false, "disabled_owner_links": false, "account_admin": false, "reauthentication": false}
	for _, c := range me.Capabilities {
		if _, ok := want[c]; ok {
			want[c] = true
		}
	}
	for c, seen := range want {
		if !seen {
			t.Errorf("falta la capacidad %q en %v", c, me.Capabilities)
		}
	}

	mustReauth(t, e.c, e.admin, adminPassword)
	events := decodeJSON[[]map[string]any](t, e.c.do(http.MethodGet, "/api/v1/audit?limit=10", nil, e.admin))
	if len(events) == 0 {
		t.Fatal("la auditoría está vacía")
	}
	first := events[0]
	for _, key := range []string{"id", "occurred_at", "event_type"} {
		if _, ok := first[key]; !ok {
			t.Errorf("al evento le falta %q: %v", key, first)
		}
	}
	if first["event_type"] != "reauthenticated" {
		t.Errorf("último evento = %v, esperado reauthenticated", first["event_type"])
	}
	if _, ok := first["EventType"]; ok {
		t.Error("la auditoría sigue saliendo con los nombres de campo de Go")
	}
}
