// Guardas de PATCH /users/{id} sobre la propia cuenta del administrador
// (revisión de seguridad del cliente de escritorio, 2026-10): igual que
// DELETE ya impedía borrarse a uno mismo, desactivarse dejaría la instancia
// sin nadie que pudiera administrarla si es el único administrador.
package integration

import (
	"io"
	"net/http"
	"testing"
)

func TestAdminCannotDisableTheirOwnAccount(t *testing.T) {
	e := newQuotaEnv(t, 0)

	resp := e.c.do(http.MethodPatch, "/api/v1/users/"+e.adminID, map[string]any{"status": "disabled"}, e.admin)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("PATCH status=disabled sobre la propia cuenta: status = %d, cuerpo = %s, esperado 400", resp.StatusCode, b)
	}

	// La cuenta sigue operativa: la misma sesión todavía funciona.
	me := e.c.do(http.MethodGet, "/api/v1/users/me", nil, e.admin)
	defer me.Body.Close()
	if me.StatusCode != http.StatusOK {
		t.Fatalf("GET /users/me tras el intento: status = %d, esperado 200", me.StatusCode)
	}
}

func TestAdminCanStillEditTheirOwnProfileAndDisableOthers(t *testing.T) {
	e := newQuotaEnv(t, 0)

	self := e.c.do(http.MethodPatch, "/api/v1/users/"+e.adminID, map[string]any{"display_name": "La dueña"}, e.admin)
	defer self.Body.Close()
	if self.StatusCode != http.StatusOK {
		t.Fatalf("PATCH display_name propio: status = %d, esperado 200", self.StatusCode)
	}

	other := e.c.do(http.MethodPatch, "/api/v1/users/"+e.memberID, map[string]any{"status": "disabled"}, e.admin)
	defer other.Body.Close()
	if other.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status=disabled sobre otro usuario: status = %d, esperado 200", other.StatusCode)
	}
}
