package webdav

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	xwebdav "golang.org/x/net/webdav"
)

const lockBody = `<?xml version="1.0" encoding="utf-8"?><D:lockinfo xmlns:D="DAV:">` +
	`<D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype><D:owner>maria</D:owner></D:lockinfo>`

func zeroDepthLock(t *testing.T, ls xwebdav.LockSystem, now time.Time, name string) string {
	t.Helper()
	tok, err := ls.Create(now, xwebdav.LockDetails{Root: name, Duration: time.Minute, ZeroDepth: true})
	if err != nil {
		t.Fatalf("Create(%s) falló: %v", name, err)
	}
	return tok
}

// Canario: documenta el comportamiento de x/net que newLockSystem corrige.
// Si algún día x/net lo arregla, este test falla y avisa de que el envoltorio
// ya no hace falta.
func TestUpstreamMemLSStillDemandsATokenForTheDestinationToo(t *testing.T) {
	ls := xwebdav.NewMemLS()
	now := time.Now()
	tok := zeroDepthLock(t, ls, now, "/a.txt")

	if _, err := ls.Confirm(now, "/a.txt", "/b.txt", xwebdav.Condition{Token: tok}); !errors.Is(err, xwebdav.ErrConfirmationFailed) {
		t.Fatalf("x/net MemLS.Confirm con destino sin lock: err = %v; esperado ErrConfirmationFailed (el defecto que newLockSystem corrige)", err)
	}
}

// RFC 4918 §7.5: hace falta token para los recursos que SÍ están bloqueados. Un
// MOVE de un archivo bloqueado a un destino libre debe bastar con el token del
// origen (es lo que hacen davfs2 y otros clientes que bloquean antes de
// renombrar).
func TestLockSystemAcceptsTheSourceTokenWhenTheDestinationIsFree(t *testing.T) {
	ls := newLockSystem()
	now := time.Now()
	tok := zeroDepthLock(t, ls, now, "/a.txt")

	release, err := ls.Confirm(now, "/a.txt", "/b.txt", xwebdav.Condition{Token: tok})
	if err != nil {
		t.Fatalf("Confirm con el token del origen y destino libre: %v", err)
	}
	// Mientras dura la operación el destino queda retenido: nadie más lo bloquea.
	if _, err := ls.Create(now, xwebdav.LockDetails{Root: "/b.txt", Duration: time.Minute, ZeroDepth: true}); !errors.Is(err, xwebdav.ErrLocked) {
		t.Errorf("Create sobre el destino durante la operación: err = %v, esperado ErrLocked", err)
	}
	release()
	// Al terminar, queda libre otra vez.
	zeroDepthLock(t, ls, now, "/b.txt")
}

func TestLockSystemAlsoAcceptsATokenThatOnlyCoversTheDestination(t *testing.T) {
	ls := newLockSystem()
	now := time.Now()
	tok := zeroDepthLock(t, ls, now, "/b.txt")

	release, err := ls.Confirm(now, "/a.txt", "/b.txt", xwebdav.Condition{Token: tok})
	if err != nil {
		t.Fatalf("Confirm con el token del destino y origen libre: %v", err)
	}
	release()
}

func TestLockSystemStillRejectsWhatMustBeRejected(t *testing.T) {
	now := time.Now()

	t.Run("token inventado", func(t *testing.T) {
		ls := newLockSystem()
		if _, err := ls.Confirm(now, "/a.txt", "/b.txt", xwebdav.Condition{Token: "inventado"}); !errors.Is(err, xwebdav.ErrConfirmationFailed) {
			t.Errorf("err = %v, esperado ErrConfirmationFailed", err)
		}
	})

	t.Run("token de un recurso que no es ni origen ni destino", func(t *testing.T) {
		ls := newLockSystem()
		tok := zeroDepthLock(t, ls, now, "/otro.txt")
		if _, err := ls.Confirm(now, "/a.txt", "/b.txt", xwebdav.Condition{Token: tok}); !errors.Is(err, xwebdav.ErrConfirmationFailed) {
			t.Errorf("err = %v, esperado ErrConfirmationFailed", err)
		}
	})

	t.Run("destino bloqueado por otro lock cuyo token no se presenta", func(t *testing.T) {
		ls := newLockSystem()
		src := zeroDepthLock(t, ls, now, "/a.txt")
		zeroDepthLock(t, ls, now, "/b.txt") // bloqueo ajeno sobre el destino
		if _, err := ls.Confirm(now, "/a.txt", "/b.txt", xwebdav.Condition{Token: src}); !errors.Is(err, xwebdav.ErrConfirmationFailed) {
			t.Fatalf("err = %v, esperado ErrConfirmationFailed", err)
		}
		// El intento fallido no deja retenido el origen.
		if release, err := ls.Confirm(now, "/a.txt", "", xwebdav.Condition{Token: src}); err != nil {
			t.Errorf("tras el fallo, el origen debía seguir disponible: %v", err)
		} else {
			release()
		}
	})
}

// Lo que hace davfs2: LOCK del archivo y MOVE con la cabecera If (con la URL
// del origen como etiqueta y solo su token). Antes: 412 Precondition Failed.
func TestHandlerMoveOfALockedFileWithTheIfHeaderSucceeds(t *testing.T) {
	d := newDavEnv(t, true, Options{})
	a := d.account(t, "maria")
	expectStatus(t, d.do(t, a, "PUT", "/webdav/a.txt", "hola", nil), http.StatusCreated, "PUT")

	resp := d.do(t, a, "LOCK", "/webdav/a.txt", lockBody, map[string]string{"Depth": "0", "Timeout": "Second-1800", "Content-Type": "application/xml"})
	token := strings.Trim(resp.Header.Get("Lock-Token"), "<>")
	expectStatus(t, resp, http.StatusOK, "LOCK")
	if token == "" {
		t.Fatal("LOCK no devolvió Lock-Token")
	}

	resp = d.do(t, a, "MOVE", "/webdav/a.txt", "", map[string]string{
		"Destination": d.srv.URL + "/webdav/b.txt",
		"Overwrite":   "T",
		"If":          "<" + d.srv.URL + "/webdav/a.txt> (<" + token + ">)",
	})
	expectStatus(t, resp, http.StatusCreated, "MOVE del archivo bloqueado con If etiquetado")
	if got := bodyOf(t, d.do(t, a, "GET", "/webdav/b.txt", "", nil)); got != "hola" {
		t.Errorf("b.txt = %q, esperado %q", got, "hola")
	}
	expectStatus(t, d.do(t, a, "GET", "/webdav/a.txt", "", nil), http.StatusNotFound, "el origen ya no existe")
}

func TestHandlerMoveWithIfStillFailsWhenTheTokenIsWrongOrTheDestinationIsLockedByAnother(t *testing.T) {
	d := newDavEnv(t, true, Options{})
	a := d.account(t, "maria")
	expectStatus(t, d.do(t, a, "PUT", "/webdav/a.txt", "hola", nil), http.StatusCreated, "PUT a")
	expectStatus(t, d.do(t, a, "PUT", "/webdav/b.txt", "otro", nil), http.StatusCreated, "PUT b")
	lock := func(path string) string {
		resp := d.do(t, a, "LOCK", path, lockBody, map[string]string{"Depth": "0", "Timeout": "Second-1800", "Content-Type": "application/xml"})
		defer resp.Body.Close()
		return strings.Trim(resp.Header.Get("Lock-Token"), "<>")
	}
	move := func(ifHeader string) *http.Response {
		return d.do(t, a, "MOVE", "/webdav/a.txt", "", map[string]string{
			"Destination": d.srv.URL + "/webdav/c.txt", "If": ifHeader,
		})
	}

	// Token inventado.
	expectStatus(t, move("<"+d.srv.URL+"/webdav/a.txt> (<inventado>)"), http.StatusPreconditionFailed, "MOVE con token inventado")

	// El destino tiene un bloqueo cuyo token no se presenta.
	srcToken := lock("/webdav/a.txt")
	lock("/webdav/c.txt")
	expectStatus(t, move("<"+d.srv.URL+"/webdav/a.txt> (<"+srcToken+">)"), http.StatusPreconditionFailed, "MOVE sobre un destino bloqueado por otro")

	// Nada se movió.
	if got := bodyOf(t, d.do(t, a, "GET", "/webdav/a.txt", "", nil)); got != "hola" {
		t.Errorf("a.txt = %q tras los MOVE rechazados, esperado %q", got, "hola")
	}
}
