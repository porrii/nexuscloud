package webdav

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/users"
)

// davEnv es un servidor HTTP REAL (httptest) con el Handler completo delante
// de un FileService real: se prueba el módulo tal y como lo ve un cliente.
type davEnv struct {
	*fsEnv
	srv     *httptest.Server
	handler *Handler
}

type account struct {
	name, token string
	u           *users.User
}

func newDavEnv(t *testing.T, trashEnabled bool, opts Options) *davEnv {
	t.Helper()
	e := newFSEnv(t, trashEnabled)
	if opts.Prefix == "" {
		opts.Prefix = "/webdav"
	}
	h := NewHandler(e.files, e.tokens, e.rec, opts, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &davEnv{fsEnv: e, srv: srv, handler: h}
}

// account crea un usuario y un token de acceso WebDAV para él.
func (d *davEnv) account(t *testing.T, username string) account {
	t.Helper()
	u := d.user(t, username)
	_, plain, err := d.tokens.Create(context.Background(), u.ID, "prueba")
	if err != nil {
		t.Fatalf("Create token falló: %v", err)
	}
	return account{name: username, token: plain, u: u}
}

func (d *davEnv) do(t *testing.T, a account, method, urlPath, body string, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, d.srv.URL+urlPath, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if a.name != "" {
		req.SetBasicAuth(a.name, a.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := d.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s falló: %v", method, urlPath, err)
	}
	return resp
}

func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("leyendo el cuerpo: %v", err)
	}
	return string(b)
}

func expectStatus(t *testing.T, resp *http.Response, want int, what string) {
	t.Helper()
	if resp.StatusCode != want {
		t.Errorf("%s: status = %d, esperado %d", what, resp.StatusCode, want)
	}
	resp.Body.Close()
}

func TestHandlerAsksForBasicCredentialsAndRejectsAnythingButATokenOfTheOwner(t *testing.T) {
	d := newDavEnv(t, true, Options{})
	maria := d.account(t, "maria")
	d.account(t, "beto")

	resp := d.do(t, account{}, "PROPFIND", "/webdav/", "", nil)
	if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic ") {
		t.Errorf("sin credenciales: status %d, WWW-Authenticate %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	resp.Body.Close()

	// Ninguno de estos debe entrar: token inventado, la contraseña de la
	// cuenta (nunca válida por WebDAV, ADR-034) y el token de otra persona
	// con el username de maria.
	beto := d.account(t, "carla")
	for name, a := range map[string]account{
		"token inventado":            {name: "maria", token: tokenPrefix + "inventado"},
		"contraseña de la cuenta":    {name: "maria", token: "la-contraseña-de-maria"},
		"token ajeno con mi usuario": {name: "maria", token: beto.token},
	} {
		expectStatus(t, d.do(t, a, "PROPFIND", "/webdav/", "", nil), http.StatusUnauthorized, name)
	}
	expectStatus(t, d.do(t, maria, "PROPFIND", "/webdav/", "", map[string]string{"Depth": "0"}), http.StatusMultiStatus, "credenciales correctas")

	// Los 3 intentos fallidos con credenciales quedan auditados (el primero,
	// sin cabecera Authorization, es el sondeo normal de todo cliente y no).
	events, err := d.auditRepo.ListEvents(context.Background(), 50, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	failed := 0
	for _, ev := range events {
		if ev.EventType == audit.EventWebDAVAuthFailed {
			failed++
			if ev.TargetID != "maria" {
				t.Errorf("TargetID = %q, esperado el username intentado", ev.TargetID)
			}
		}
	}
	if failed != 3 {
		t.Errorf("eventos webdav_auth_failed = %d, esperados 3", failed)
	}
}

func TestHandlerFullRoundTripWithRangeAndPropfind(t *testing.T) {
	d := newDavEnv(t, true, Options{})
	a := d.account(t, "maria")
	content := "hola mundo"
	sum := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(sum[:])

	expectStatus(t, d.do(t, a, "MKCOL", "/webdav/docs", "", nil), http.StatusCreated, "MKCOL")
	expectStatus(t, d.do(t, a, "MKCOL", "/webdav/docs", "", nil), http.StatusMethodNotAllowed, "MKCOL repetido")
	expectStatus(t, d.do(t, a, "MKCOL", "/webdav/no/existe", "", nil), http.StatusConflict, "MKCOL con padre inexistente")

	resp := d.do(t, a, "PUT", "/webdav/docs/a.txt", content, nil)
	if resp.StatusCode != http.StatusCreated || resp.Header.Get("ETag") != `"`+sha+`"` {
		t.Errorf("PUT: status %d, ETag %q; esperado 201 y el SHA-256 del contenido", resp.StatusCode, resp.Header.Get("ETag"))
	}
	resp.Body.Close()
	expectStatus(t, d.do(t, a, "PUT", "/webdav/no/existe/a.txt", "x", nil), http.StatusConflict, "PUT con padre inexistente")

	resp = d.do(t, a, "GET", "/webdav/docs/a.txt", "", nil)
	if got := bodyOf(t, resp); got != content || resp.Header.Get("ETag") != `"`+sha+`"` {
		t.Errorf("GET: cuerpo %q, ETag %q", got, resp.Header.Get("ETag"))
	}

	resp = d.do(t, a, "GET", "/webdav/docs/a.txt", "", map[string]string{"Range": "bytes=5-9"})
	if got := bodyOf(t, resp); resp.StatusCode != http.StatusPartialContent || got != "mundo" {
		t.Errorf("GET con Range: status %d, cuerpo %q; esperado 206 y %q", resp.StatusCode, got, "mundo")
	}

	resp = d.do(t, a, "HEAD", "/webdav/docs/a.txt", "", nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Length") != fmt.Sprint(len(content)) {
		t.Errorf("HEAD: status %d, Content-Length %q", resp.StatusCode, resp.Header.Get("Content-Length"))
	}
	resp.Body.Close()

	resp = d.do(t, a, "PROPFIND", "/webdav/docs/", "", map[string]string{"Depth": "1"})
	xml := bodyOf(t, resp)
	if resp.StatusCode != http.StatusMultiStatus || !strings.Contains(xml, "/webdav/docs/a.txt") || !strings.Contains(xml, sha) {
		t.Errorf("PROPFIND: status %d; esperado 207 con a.txt y su ETag (SHA-256)\n%s", resp.StatusCode, xml)
	}

	resp = d.do(t, a, "OPTIONS", "/webdav/", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("DAV"), "1") {
		t.Errorf("OPTIONS: status %d, DAV %q", resp.StatusCode, resp.Header.Get("DAV"))
	}
	resp.Body.Close()
}

func TestHandlerReadOnlyModeOnlyAllowsReadingMethods(t *testing.T) {
	d := newDavEnv(t, true, Options{ReadOnly: true})
	a := d.account(t, "maria")
	putFile(t, d.fs(a.u), context.Background(), "/existe.txt", "contenido")

	lock := `<?xml version="1.0"?><D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockinfo>`
	for method, req := range map[string]struct{ path, body string }{
		"PUT":       {"/webdav/nuevo.txt", "x"},
		"MKCOL":     {"/webdav/carpeta", ""},
		"DELETE":    {"/webdav/existe.txt", ""},
		"MOVE":      {"/webdav/existe.txt", ""},
		"COPY":      {"/webdav/existe.txt", ""},
		"PROPPATCH": {"/webdav/existe.txt", ""},
		"LOCK":      {"/webdav/existe.txt", lock},
	} {
		resp := d.do(t, a, method, req.path, req.body, map[string]string{"Destination": d.srv.URL + "/webdav/otro.txt"})
		expectStatus(t, resp, http.StatusForbidden, method+" en modo solo lectura")
	}
	for method, want := range map[string]int{"GET": 200, "HEAD": 200, "OPTIONS": 200, "PROPFIND": 207} {
		expectStatus(t, d.do(t, a, method, "/webdav/existe.txt", "", map[string]string{"Depth": "0"}), want, method+" en modo solo lectura")
	}
	if _, err := d.fs(a.u).Stat(context.Background(), "/nuevo.txt"); err == nil {
		t.Error("el PUT bloqueado no debería haber creado nada")
	}
}

func TestHandlerMaxUploadSize(t *testing.T) {
	d := newDavEnv(t, true, Options{MaxUploadSizeBytes: 10})
	a := d.account(t, "maria")
	fs := d.fs(a.u)

	expectStatus(t, d.do(t, a, "PUT", "/webdav/grande.txt", "12345678901", nil), http.StatusRequestEntityTooLarge, "PUT de 11 bytes con límite 10")
	expectStatus(t, d.do(t, a, "PUT", "/webdav/justo.txt", "1234567890", nil), http.StatusCreated, "PUT de 10 bytes")

	// Sin Content-Length (chunked) el tamaño no se conoce de antemano: lo
	// corta MaxBytesReader a mitad y el tracker aborta la subida.
	req, _ := http.NewRequest("PUT", d.srv.URL+"/webdav/chunked.txt", io.NopCloser(strings.NewReader("12345678901")))
	req.ContentLength = -1
	req.SetBasicAuth(a.name, a.token)
	resp, err := d.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT chunked falló: %v", err)
	}
	if resp.StatusCode < 400 {
		t.Errorf("PUT chunked por encima del límite: status %d, esperado un error", resp.StatusCode)
	}
	resp.Body.Close()

	for _, name := range []string{"/grande.txt", "/chunked.txt"} {
		if _, err := fs.Stat(context.Background(), name); err == nil {
			t.Errorf("%s no debería existir (superaba el límite)", name)
		}
	}
	if _, err := fs.Stat(context.Background(), "/justo.txt"); err != nil {
		t.Errorf("/justo.txt debería existir: %v", err)
	}
}

// Un PUT cuyo cuerpo falla a mitad no debe dejar un archivo truncado dado por
// bueno. El handler de x/net llama a Close() del archivo también cuando la
// copia falló, así que sin el tracker de errores del cuerpo la subida parcial
// se confirmaría.
//
// Los casos se envían a mano (http.Client jamás enviaría menos bytes de los
// prometidos ni un fragmento chunked corrupto) y la conexión se mantiene
// abierta hasta leer la respuesta: si el cliente cerrase el socket, Go
// cancelaría el contexto de la petición y la subida también fallaría, pero
// por un motivo ajeno al que se quiere probar.
func TestHandlerBrokenPutBodyLeavesNothingBehind(t *testing.T) {
	e := newFSEnv(t, true)
	a := account{name: "maria"}
	a.u = e.user(t, "maria")
	_, a.token, _ = e.tokens.Create(context.Background(), a.u.ID, "prueba")

	h := NewHandler(e.files, e.tokens, e.rec, Options{Prefix: "/webdav"}, nil)
	served := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		served <- struct{}{}
	}))
	defer srv.Close()

	creds := base64.StdEncoding.EncodeToString([]byte(a.name + ":" + a.token))
	// rawPut devuelve el código de estado de la respuesta.
	rawPut := func(path, framing, body string) int {
		t.Helper()
		conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatalf("Dial falló: %v", err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "PUT %s HTTP/1.1\r\nHost: x\r\nAuthorization: Basic %s\r\n%s\r\n\r\n%s", path, creds, framing, body)
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("leyendo la respuesta de %s: %v", path, err)
		}
		resp.Body.Close()
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Fatalf("el handler no terminó para %s", path)
		}
		return resp.StatusCode
	}

	// Control positivo: el mismo camino con el cuerpo completo SÍ crea el
	// archivo. Sin esto el test podría pasar por un motivo ajeno (p.ej. un 401
	// que impidiera llegar siquiera a la subida).
	if status := rawPut("/webdav/completo.txt", "Content-Length: 5", "hola!"); status != http.StatusCreated {
		t.Fatalf("PUT completo por el camino crudo: status %d, esperado 201", status)
	}
	if got := readContent(t, e.fs(a.u), context.Background(), "/completo.txt"); got != "hola!" {
		t.Fatalf("contenido del PUT completo = %q", got)
	}

	// Cuerpo chunked con un fragmento corrupto tras los primeros 5 bytes: la
	// lectura del cuerpo falla con la conexión aún abierta.
	if status := rawPut("/webdav/corrupto.txt", "Transfer-Encoding: chunked", "5\r\nhola!\r\nzz\r\n"); status < 400 {
		t.Errorf("PUT con cuerpo corrupto: status %d, esperado un error", status)
	}
	if _, err := e.fs(a.u).Stat(context.Background(), "/corrupto.txt"); err == nil {
		t.Error("un PUT con el cuerpo corrupto no debe crear el archivo")
	}
}

func TestHandlerLocksAreIsolatedPerUser(t *testing.T) {
	d := newDavEnv(t, true, Options{})
	ana, beto := d.account(t, "ana"), d.account(t, "beto")
	lock := `<?xml version="1.0"?><D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype><D:owner>prueba</D:owner></D:lockinfo>`
	hdr := map[string]string{"Depth": "0", "Timeout": "Second-60", "Content-Type": "application/xml"}

	expectStatus(t, d.do(t, ana, "LOCK", "/webdav/doc.txt", lock, hdr), http.StatusCreated, "LOCK de ana (crea el recurso: lock-null)")
	// Sin aislamiento (una MemLS compartida indexada por path relativo) el
	// mismo "/doc.txt" de beto quedaría bloqueado por el de ana.
	expectStatus(t, d.do(t, beto, "LOCK", "/webdav/doc.txt", lock, hdr), http.StatusCreated, "LOCK de beto sobre SU /doc.txt")
	// Y los locks sí funcionan dentro de un mismo usuario.
	expectStatus(t, d.do(t, ana, "LOCK", "/webdav/doc.txt", lock, hdr), http.StatusLocked, "segundo LOCK exclusivo de ana")
}

func TestHandlerUsersNeverSeeEachOthersFiles(t *testing.T) {
	d := newDavEnv(t, true, Options{})
	ana, beto := d.account(t, "ana"), d.account(t, "beto")

	expectStatus(t, d.do(t, ana, "PUT", "/webdav/secreto.txt", "solo de ana", nil), http.StatusCreated, "PUT de ana")
	expectStatus(t, d.do(t, beto, "GET", "/webdav/secreto.txt", "", nil), http.StatusNotFound, "GET de beto")
	resp := d.do(t, beto, "PROPFIND", "/webdav/", "", map[string]string{"Depth": "1"})
	if xml := bodyOf(t, resp); strings.Contains(xml, "secreto.txt") {
		t.Errorf("beto ve el archivo de ana en su listado:\n%s", xml)
	}
	expectStatus(t, d.do(t, beto, "DELETE", "/webdav/secreto.txt", "", nil), http.StatusNotFound, "DELETE de beto")
	resp = d.do(t, ana, "GET", "/webdav/secreto.txt", "", nil)
	if got := bodyOf(t, resp); got != "solo de ana" {
		t.Errorf("el archivo de ana se alteró: %q", got)
	}
}

func TestHandlerCopyMoveAndRecursiveDelete(t *testing.T) {
	d := newDavEnv(t, true, Options{})
	a := d.account(t, "maria")
	dest := func(p string) map[string]string {
		return map[string]string{"Destination": d.srv.URL + p, "Overwrite": "F"}
	}

	expectStatus(t, d.do(t, a, "PUT", "/webdav/a.txt", "original", nil), http.StatusCreated, "PUT")
	expectStatus(t, d.do(t, a, "COPY", "/webdav/a.txt", "", dest("/webdav/b.txt")), http.StatusCreated, "COPY")
	expectStatus(t, d.do(t, a, "MOVE", "/webdav/b.txt", "", dest("/webdav/c.txt")), http.StatusCreated, "MOVE")
	expectStatus(t, d.do(t, a, "GET", "/webdav/b.txt", "", nil), http.StatusNotFound, "GET del origen movido")
	if got := bodyOf(t, d.do(t, a, "GET", "/webdav/c.txt", "", nil)); got != "original" {
		t.Errorf("contenido tras COPY+MOVE = %q", got)
	}

	expectStatus(t, d.do(t, a, "MKCOL", "/webdav/d", "", nil), http.StatusCreated, "MKCOL")
	expectStatus(t, d.do(t, a, "PUT", "/webdav/d/f.txt", "dentro", nil), http.StatusCreated, "PUT dentro")
	expectStatus(t, d.do(t, a, "DELETE", "/webdav/d", "", nil), http.StatusNoContent, "DELETE de una carpeta con contenido")
	expectStatus(t, d.do(t, a, "GET", "/webdav/d/f.txt", "", nil), http.StatusNotFound, "GET tras borrar la carpeta")
	trash, err := d.files.ListTrash(context.Background(), a.u.ID)
	if err != nil {
		t.Fatalf("ListTrash falló: %v", err)
	}
	if len(trash.Files) != 1 || trash.Files[0].Name != "f.txt" {
		t.Errorf("papelera = %+v, esperado f.txt (DELETE por WebDAV usa la papelera igual que la API)", trash.Files)
	}
}

// MOVE con Overwrite:T sobre un destino que existe: x/net borra primero el
// destino y luego renombra. Con la papelera activa el nombre queda ocupado
// por la papelera (§128, ADR-030) y FileService.MoveFile lo rechaza: límite
// conocido, documentado en ADR-034. Con la papelera desactivada funciona.
// listing devuelve los nombres activos de una carpeta del usuario (las
// carpetas con "/" al final) y trashed los de su papelera: lo que hace falta
// para comprobar qué ha pasado de verdad tras una petición, no solo su status.
func (d *davEnv) listing(t *testing.T, a account, dir string) []string {
	t.Helper()
	res, err := d.files.List(context.Background(), a.u.ID, dir)
	if err != nil {
		t.Fatalf("List(%s) falló: %v", dir, err)
	}
	var out []string
	for _, x := range res.Directories {
		out = append(out, x.Name+"/")
	}
	for _, x := range res.Files {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

func (d *davEnv) trashed(t *testing.T, a account) []string {
	t.Helper()
	res, err := d.files.ListTrash(context.Background(), a.u.ID)
	if err != nil {
		t.Fatalf("ListTrash falló: %v", err)
	}
	var out []string
	for _, x := range res.Directories {
		out = append(out, x.Name+"/")
	}
	for _, x := range res.Files {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

// archivedVersions devuelve el contenido de las versiones archivadas (no la
// vigente) de un fichero, de la más antigua a la más reciente.
func (d *davEnv) archivedVersions(t *testing.T, a account, dir, name string) []string {
	t.Helper()
	ctx := context.Background()
	res, err := d.files.List(ctx, a.u.ID, dir)
	if err != nil {
		t.Fatalf("List(%s) falló: %v", dir, err)
	}
	for _, m := range res.Files {
		if m.Name != name {
			continue
		}
		versions, err := d.files.ListVersions(ctx, a.u.ID, m.ID)
		if err != nil {
			t.Fatalf("ListVersions falló: %v", err)
		}
		sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNum < versions[j].VersionNum })
		var out []string
		for _, v := range versions {
			_, rc, err := d.files.DownloadVersion(ctx, a.u.ID, m.ID, v.VersionNum)
			if err != nil {
				t.Fatalf("DownloadVersion %d falló: %v", v.VersionNum, err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			out = append(out, string(b))
		}
		return out
	}
	t.Fatalf("no existe %s/%s", dir, name)
	return nil
}

func sameList(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func (d *davEnv) overwrite(t *testing.T, a account, method, from, to string) *http.Response {
	t.Helper()
	return d.do(t, a, method, from, "", map[string]string{"Destination": d.srv.URL + to, "Overwrite": "T"})
}

// Con Overwrite:T, x/net hace RemoveAll(destino) y luego crea/renombra encima.
// Con la papelera activa eso mandaría el destino a la papelera y su nombre
// quedaría reservado (§128), así que la operación fallaría DESPUÉS de haberlo
// quitado del árbol. Un MOVE/COPY sobre un archivo existente tiene que
// funcionar y conservar el contenido anterior como versión.
func TestHandlerMoveOverwriteReplacesAFileAndKeepsTheOldContentAsAVersion(t *testing.T) {
	for _, trash := range []bool{true, false} {
		t.Run(fmt.Sprintf("papelera=%v", trash), func(t *testing.T) {
			d := newDavEnv(t, trash, Options{})
			a := d.account(t, "maria")
			expectStatus(t, d.do(t, a, "PUT", "/webdav/x.txt", "nuevo", nil), http.StatusCreated, "PUT x")
			expectStatus(t, d.do(t, a, "PUT", "/webdav/y.txt", "viejo", nil), http.StatusCreated, "PUT y")

			expectStatus(t, d.overwrite(t, a, "MOVE", "/webdav/x.txt", "/webdav/y.txt"), http.StatusNoContent, "MOVE con Overwrite:T")

			resp := d.do(t, a, "GET", "/webdav/y.txt", "", nil)
			if got := bodyOf(t, resp); got != "nuevo" {
				t.Errorf("el destino tiene %q, esperado el contenido movido %q", got, "nuevo")
			}
			expectStatus(t, d.do(t, a, "GET", "/webdav/x.txt", "", nil), http.StatusNotFound, "el origen ya no existe")
			if got := d.listing(t, a, "/"); !sameList(got, []string{"y.txt"}) {
				t.Errorf("árbol activo = %v, esperado solo y.txt", got)
			}
			if trash {
				// El origen (lo que "se movió") va a la papelera; el destino, nunca.
				if got := d.trashed(t, a); !sameList(got, []string{"x.txt"}) {
					t.Errorf("papelera = %v, esperado solo el origen x.txt", got)
				}
				if got := d.archivedVersions(t, a, "/", "y.txt"); !sameList(got, []string{"viejo"}) {
					t.Errorf("versiones archivadas de y.txt = %v, esperado [viejo]", got)
				}
			}
		})
	}
}

func TestHandlerCopyOverwriteReplacesAFileAndKeepsTheSource(t *testing.T) {
	for _, trash := range []bool{true, false} {
		t.Run(fmt.Sprintf("papelera=%v", trash), func(t *testing.T) {
			d := newDavEnv(t, trash, Options{})
			a := d.account(t, "maria")
			expectStatus(t, d.do(t, a, "PUT", "/webdav/x.txt", "origen", nil), http.StatusCreated, "PUT x")
			expectStatus(t, d.do(t, a, "PUT", "/webdav/y.txt", "viejo", nil), http.StatusCreated, "PUT y")

			expectStatus(t, d.overwrite(t, a, "COPY", "/webdav/x.txt", "/webdav/y.txt"), http.StatusNoContent, "COPY con Overwrite:T")

			for _, name := range []string{"x.txt", "y.txt"} {
				if got := bodyOf(t, d.do(t, a, "GET", "/webdav/"+name, "", nil)); got != "origen" {
					t.Errorf("%s = %q, esperado %q", name, got, "origen")
				}
			}
			if trash {
				if got := d.trashed(t, a); len(got) != 0 {
					t.Errorf("papelera = %v: un COPY no debe mandar nada a la papelera", got)
				}
				if got := d.archivedVersions(t, a, "/", "y.txt"); !sameList(got, []string{"viejo"}) {
					t.Errorf("versiones archivadas de y.txt = %v, esperado [viejo]", got)
				}
			}
		})
	}
}

// Regresión del defecto que encontró litmus + curl: un MOVE/COPY con
// Overwrite:T que NO puede completarse debe fallar SIN tocar nada. Antes, el
// destino acababa en la papelera aunque el cliente recibiera un 403.
func TestHandlerAnOverwriteThatCannotCompleteNeverDestroysTheDestination(t *testing.T) {
	for _, tc := range []struct{ name, method, from, to string }{
		{"carpeta sobre carpeta (MOVE)", "MOVE", "/webdav/a", "/webdav/b"},
		{"carpeta sobre carpeta (COPY)", "COPY", "/webdav/a", "/webdav/b"},
		{"carpeta sobre archivo (MOVE)", "MOVE", "/webdav/a", "/webdav/z.txt"},
		{"carpeta sobre archivo (COPY)", "COPY", "/webdav/a", "/webdav/z.txt"},
		{"archivo sobre carpeta (MOVE)", "MOVE", "/webdav/z.txt", "/webdav/b"},
		{"archivo sobre carpeta (COPY)", "COPY", "/webdav/z.txt", "/webdav/b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Entorno propio por caso: si uno falla, no arrastra a los demás.
			d := newDavEnv(t, true, Options{})
			a := d.account(t, "maria")
			expectStatus(t, d.do(t, a, "MKCOL", "/webdav/a", "", nil), http.StatusCreated, "MKCOL a")
			expectStatus(t, d.do(t, a, "PUT", "/webdav/a/f.txt", "a1", nil), http.StatusCreated, "PUT a/f")
			expectStatus(t, d.do(t, a, "MKCOL", "/webdav/b", "", nil), http.StatusCreated, "MKCOL b")
			expectStatus(t, d.do(t, a, "PUT", "/webdav/b/g.txt", "b1", nil), http.StatusCreated, "PUT b/g")
			expectStatus(t, d.do(t, a, "PUT", "/webdav/z.txt", "zeta", nil), http.StatusCreated, "PUT z")

			expectStatus(t, d.overwrite(t, a, tc.method, tc.from, tc.to), http.StatusForbidden, tc.name)

			if got := d.listing(t, a, "/"); !sameList(got, []string{"a/", "b/", "z.txt"}) {
				t.Errorf("árbol activo = %v tras el intento fallido, esperado a/ b/ z.txt", got)
			}
			if got := d.trashed(t, a); len(got) != 0 {
				t.Errorf("papelera = %v: un overwrite fallido no debe haber borrado nada", got)
			}
			for path, want := range map[string]string{"/webdav/a/f.txt": "a1", "/webdav/b/g.txt": "b1", "/webdav/z.txt": "zeta"} {
				if got := bodyOf(t, d.do(t, a, "GET", path, "", nil)); got != want {
					t.Errorf("%s = %q, esperado %q", path, got, want)
				}
			}
		})
	}
}

// Con la papelera desactivada el borrado es físico y los nombres quedan
// libres, así que la semántica completa de RFC 4918 (incluidas las carpetas)
// sigue disponible.
func TestHandlerOverwriteOfCollectionsWorksWhenTheTrashIsOff(t *testing.T) {
	d := newDavEnv(t, false, Options{})
	a := d.account(t, "maria")
	expectStatus(t, d.do(t, a, "MKCOL", "/webdav/a", "", nil), http.StatusCreated, "MKCOL a")
	expectStatus(t, d.do(t, a, "PUT", "/webdav/a/f.txt", "a1", nil), http.StatusCreated, "PUT a/f")
	expectStatus(t, d.do(t, a, "MKCOL", "/webdav/b", "", nil), http.StatusCreated, "MKCOL b")
	expectStatus(t, d.do(t, a, "PUT", "/webdav/b/g.txt", "b1", nil), http.StatusCreated, "PUT b/g")

	expectStatus(t, d.overwrite(t, a, "MOVE", "/webdav/a", "/webdav/b"), http.StatusNoContent, "MOVE de carpeta sobre carpeta")
	if got := d.listing(t, a, "/b"); !sameList(got, []string{"f.txt"}) {
		t.Errorf("/b = %v, esperado solo f.txt (la carpeta antigua se reemplazó entera)", got)
	}
}
