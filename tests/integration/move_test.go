// Tests de integración de PATCH /files/{id} y PATCH /directories/{id}
// (ADR-030, §85: mover/renombrar de verdad, no borrar+volver a subir) --
// arrancan un server.Server real (mismo patrón que el resto de este
// paquete) y los ejercitan por HTTP real.
package integration

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type fileDTO struct {
	ID         string `json:"id"`
	ParentPath string `json:"parent_path"`
	Name       string `json:"name"`
}

type directoryDTO struct {
	ID         string `json:"id"`
	ParentPath string `json:"parent_path"`
	Name       string `json:"name"`
}

type tokenDTO struct {
	Token string `json:"token"`
}

// uploadTestFile sube contenido en crudo (no vía apiClient.do, que
// siempre serializa el body a JSON) -- mismo endpoint y forma de
// autenticación que el resto de este paquete.
func uploadTestFile(t *testing.T, base *apiClient, token, parentPath, name, content string) fileDTO {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		base.base+"/api/v1/files?name="+name+"&path="+parentPath, strings.NewReader(content))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := base.client.Do(req)
	if err != nil {
		t.Fatalf("subiendo %s: %v", name, err)
	}
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("subiendo %s: status = %d, body = %s", name, resp.StatusCode, body)
	}
	return decodeJSON[fileDTO](t, resp)
}

func loginAs(t *testing.T, c *apiClient, username, password string) string {
	t.Helper()
	resp := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": username, "password": password}, "")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login %s: status = %d, body = %s", username, resp.StatusCode, body)
	}
	return decodeJSON[tokenDTO](t, resp).Token
}

func TestMoveFileEndpointRenamesAndPreservesID(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "erin", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: http.DefaultClient}
	token := loginAs(t, c, "erin", "contrasena-larga-123")

	original := uploadTestFile(t, c, token, "/", "viejo.txt", "contenido de prueba")

	resp := c.do(http.MethodPatch, "/api/v1/files/"+original.ID, map[string]string{"name": "nuevo.txt"}, token)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("PATCH /files/{id}: status = %d, body = %s", resp.StatusCode, body)
	}
	moved := decodeJSON[fileDTO](t, resp)
	if moved.ID != original.ID {
		t.Errorf("MoveFile no debía cambiar el ID: %q != %q", moved.ID, original.ID)
	}
	if moved.Name != "nuevo.txt" || moved.ParentPath != "/" {
		t.Errorf("moved = %+v, esperado Name=nuevo.txt ParentPath=/", moved)
	}

	// El archivo sigue siendo descargable por el MISMO ID tras el rename.
	downloadResp := c.do(http.MethodGet, "/api/v1/files/"+original.ID, nil, token)
	if downloadResp.StatusCode != http.StatusOK {
		t.Errorf("GET /files/{id} tras mover: status = %d, esperado 200", downloadResp.StatusCode)
	}
}

func TestMoveFileEndpointRejectsNonOwner(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "victima", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: http.DefaultClient}
	victimaToken := loginAs(t, c, "victima", "contrasena-larga-123")

	original := uploadTestFile(t, c, victimaToken, "/", "secreto.txt", "x")

	// Segundo usuario, creado vía invitación -- mismo patrón que
	// TestFullHappyPathFlow ("crear un segundo usuario vía invitación").
	inviteResp := c.do(http.MethodPost, "/api/v1/invitations", map[string]any{"max_uses": 1}, victimaToken)
	if inviteResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(inviteResp.Body)
		t.Fatalf("crear invitación: status = %d, body = %s", inviteResp.StatusCode, body)
	}
	invite := decodeJSON[tokenDTO](t, inviteResp)
	redeemResp := c.do(http.MethodPost, "/api/v1/invitations/redeem", map[string]string{
		"token": invite.Token, "username": "atacante", "password": "contrasena-del-atacante",
	}, "")
	if redeemResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(redeemResp.Body)
		t.Fatalf("redimiendo invitación: status = %d, body = %s", redeemResp.StatusCode, body)
	}
	atacanteToken := loginAs(t, c, "atacante", "contrasena-del-atacante")

	resp := c.do(http.MethodPatch, "/api/v1/files/"+original.ID, map[string]string{"name": "robado.txt"}, atacanteToken)
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, body = %s, esperado 403 Forbidden", resp.StatusCode, body)
	}
}

func TestMoveDirectoryEndpointMovesTreeAndDescendants(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "hugo", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: http.DefaultClient}
	token := loginAs(t, c, "hugo", "contrasena-larga-123")

	rootResp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Proyecto"}, token)
	if rootResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(rootResp.Body)
		t.Fatalf("creando /Proyecto: status = %d, body = %s", rootResp.StatusCode, body)
	}
	rootDir := decodeJSON[directoryDTO](t, rootResp)
	nested := uploadTestFile(t, c, token, "/Proyecto", "informe.txt", "contenido anidado")

	resp := c.do(http.MethodPatch, "/api/v1/directories/"+rootDir.ID, map[string]string{"name": "ProyectoViejo"}, token)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("PATCH /directories/{id}: status = %d, body = %s", resp.StatusCode, body)
	}
	moved := decodeJSON[directoryDTO](t, resp)
	if moved.Name != "ProyectoViejo" {
		t.Errorf("moved = %+v, esperado Name=ProyectoViejo", moved)
	}

	// El archivo que vivía dentro sigue siendo descargable por el MISMO
	// ID -- confirma que el descendiente se reescribió de verdad.
	downloadResp := c.do(http.MethodGet, "/api/v1/files/"+nested.ID, nil, token)
	if downloadResp.StatusCode != http.StatusOK {
		t.Errorf("GET /files/{id} del archivo anidado tras mover la carpeta: status = %d, esperado 200", downloadResp.StatusCode)
	}
}
