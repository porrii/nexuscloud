package webdav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"

	xwebdav "golang.org/x/net/webdav"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

func fileMeta(t *testing.T, e *fsEnv, u *users.User, parent, name string) *storage.FileMeta {
	t.Helper()
	res, err := e.files.List(context.Background(), u.ID, parent)
	if err != nil {
		t.Fatalf("List(%s) falló: %v", parent, err)
	}
	for _, m := range res.Files {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no hay ningún archivo %q en %s", name, parent)
	return nil
}

func TestFileSystemUploadStatDownloadAndSeek(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	fs := e.fs(e.user(t, "maria"))
	content := "contenido de prueba"

	putFile(t, fs, ctx, "/hola.txt", content)

	fi, err := fs.Stat(ctx, "/hola.txt")
	if err != nil {
		t.Fatalf("Stat falló: %v", err)
	}
	if fi.IsDir() || fi.Size() != int64(len(content)) || fi.Name() != "hola.txt" {
		t.Errorf("Stat = name %q size %d dir %v", fi.Name(), fi.Size(), fi.IsDir())
	}
	// El ETag es el SHA-256 real del contenido (mismo que FileMeta.SHA256), no
	// la heurística mtime+tamaño de x/net.
	sum := sha256.Sum256([]byte(content))
	etag, err := fi.(xwebdav.ETager).ETag(ctx)
	if err != nil || etag != `"`+hex.EncodeToString(sum[:])+`"` {
		t.Errorf("ETag = %q, %v; esperado el SHA-256 del contenido", etag, err)
	}

	// Seek + lectura parcial: lo que hace http.ServeContent para un Range.
	f, err := fs.OpenFile(ctx, "/hola.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile falló: %v", err)
	}
	defer f.Close()
	if _, err := f.Seek(-5, io.SeekEnd); err != nil {
		t.Fatalf("Seek falló: %v", err)
	}
	tail, _ := io.ReadAll(f)
	if string(tail) != "rueba" {
		t.Errorf("últimos 5 bytes = %q, esperado %q", tail, "rueba")
	}
}

func TestFileSystemMkdirFollowsWebDAVSemantics(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	fs := e.fs(e.user(t, "maria"))

	if err := fs.Mkdir(ctx, "/docs", 0o777); err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	// MKCOL sobre algo que ya existe -> 405 (FileService.Mkdir por sí solo
	// sería idempotente).
	if err := fs.Mkdir(ctx, "/docs", 0o777); !os.IsExist(err) {
		t.Errorf("segundo Mkdir: err = %v, esperado os.IsExist", err)
	}
	// Intermedio que no existe -> 409 (os.IsNotExist).
	if err := fs.Mkdir(ctx, "/no/existe/dentro", 0o777); !os.IsNotExist(err) {
		t.Errorf("Mkdir con padre inexistente: err = %v, esperado os.IsNotExist", err)
	}
	// Intermedio que es un archivo.
	putFile(t, fs, ctx, "/f.txt", "x")
	if err := fs.Mkdir(ctx, "/f.txt/sub", 0o777); !os.IsNotExist(err) {
		t.Errorf("Mkdir dentro de un archivo: err = %v, esperado os.IsNotExist", err)
	}
}

func TestFileSystemPutNeedsExistingParentAndRejectsCollections(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	fs := e.fs(e.user(t, "maria"))

	if _, err := fs.OpenFile(ctx, "/no/existe/a.txt", writeFlags, 0o666); !os.IsNotExist(err) {
		t.Errorf("PUT con padre inexistente: err = %v, esperado os.IsNotExist (409)", err)
	}
	if err := fs.Mkdir(ctx, "/carpeta", 0o777); err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}
	if _, err := fs.OpenFile(ctx, "/carpeta", writeFlags, 0o666); err == nil {
		t.Error("PUT sobre una colección debería fallar")
	}
}

func TestFileSystemRemoveAllIsRecursiveAndGoesToTrash(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	u := e.user(t, "maria")
	fs := e.fs(u)

	for _, d := range []string{"/a", "/a/b"} {
		if err := fs.Mkdir(ctx, d, 0o777); err != nil {
			t.Fatalf("Mkdir(%s) falló: %v", d, err)
		}
	}
	putFile(t, fs, ctx, "/a/b/uno.txt", "1")
	putFile(t, fs, ctx, "/a/dos.txt", "2")
	putFile(t, fs, ctx, "/libre.txt", "3")

	// FileService.DeleteDirectory solo admite carpetas vacías; WebDAV DELETE
	// sobre una colección borra todo su contenido (RFC 4918 §9.6).
	if err := fs.RemoveAll(ctx, "/a"); err != nil {
		t.Fatalf("RemoveAll de una carpeta con contenido falló: %v", err)
	}
	for _, p := range []string{"/a", "/a/b", "/a/dos.txt", "/a/b/uno.txt"} {
		if _, err := fs.Stat(ctx, p); !os.IsNotExist(err) {
			t.Errorf("Stat(%s) tras borrar: err = %v, esperado os.IsNotExist", p, err)
		}
	}
	if _, err := fs.Stat(ctx, "/libre.txt"); err != nil {
		t.Errorf("el archivo fuera de la carpeta no debería haberse tocado: %v", err)
	}
	trash, err := e.files.ListTrash(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListTrash falló: %v", err)
	}
	if len(trash.Files) != 2 || len(trash.Directories) != 2 {
		t.Errorf("papelera = %d archivos, %d carpetas; esperado 2 y 2 (nada se borra para siempre con la papelera activa)",
			len(trash.Files), len(trash.Directories))
	}

	if err := fs.RemoveAll(ctx, "/"); !os.IsPermission(err) {
		t.Errorf("RemoveAll de la raíz: err = %v, esperado os.IsPermission", err)
	}
	if err := fs.RemoveAll(ctx, "/no-existe"); err != nil {
		t.Errorf("RemoveAll de algo inexistente debería ser un no-op (como os.RemoveAll): %v", err)
	}
}

func TestFileSystemRenameMovesFilesAndFolderTrees(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	fs := e.fs(e.user(t, "maria"))

	for _, d := range []string{"/origen", "/destino"} {
		if err := fs.Mkdir(ctx, d, 0o777); err != nil {
			t.Fatalf("Mkdir(%s) falló: %v", d, err)
		}
	}
	putFile(t, fs, ctx, "/origen/x.txt", "x")
	putFile(t, fs, ctx, "/origen/z.txt", "z")

	if err := fs.Rename(ctx, "/origen/x.txt", "/destino/y.txt"); err != nil {
		t.Fatalf("Rename de archivo falló: %v", err)
	}
	if got := readContent(t, fs, ctx, "/destino/y.txt"); got != "x" {
		t.Errorf("contenido tras mover = %q", got)
	}
	if _, err := fs.Stat(ctx, "/origen/x.txt"); !os.IsNotExist(err) {
		t.Errorf("el origen debería haber desaparecido: %v", err)
	}

	// Una carpeta se mueve con todo su árbol (ADR-030).
	if err := fs.Rename(ctx, "/origen", "/destino/origen2"); err != nil {
		t.Fatalf("Rename de carpeta falló: %v", err)
	}
	if got := readContent(t, fs, ctx, "/destino/origen2/z.txt"); got != "z" {
		t.Errorf("contenido tras mover la carpeta = %q", got)
	}

	if err := fs.Rename(ctx, "/destino/y.txt", "/no/existe/y.txt"); !os.IsNotExist(err) {
		t.Errorf("Rename a un padre inexistente: err = %v, esperado os.IsNotExist (409)", err)
	}
}

func TestTruncatedUploadIsNeverCommitted(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	u := e.user(t, "maria")
	fs := e.fs(u)
	putFile(t, fs, ctx, "/doc.txt", "versión buena")

	// El handler de x/net llama a Close() también cuando la copia del cuerpo
	// falló a mitad (cliente cortado). El tracker que rellena el handler HTTP
	// con el error del cuerpo es lo que impide confirmar la subida truncada.
	newCtx := func() (context.Context, *bodyTracker) {
		tr := &bodyTracker{}
		return withRequestInfo(ctx, &requestInfo{tracker: tr, method: "PUT"}), tr
	}

	// Sobrescritura truncada de un archivo que existe.
	ctx1, tr1 := newCtx()
	f, err := fs.OpenFile(ctx1, "/doc.txt", writeFlags, 0o666)
	if err != nil {
		t.Fatalf("OpenFile falló: %v", err)
	}
	io.WriteString(f, "contenido a medi")
	tr1.set(io.ErrUnexpectedEOF)
	if err := f.Close(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Close de una subida truncada: err = %v, esperado ErrUnexpectedEOF", err)
	}
	if got := readContent(t, fs, ctx, "/doc.txt"); got != "versión buena" {
		t.Errorf("contenido tras la subida truncada = %q, debería seguir siendo la versión buena", got)
	}
	versions, err := e.files.ListVersions(ctx, u.ID, fileMeta(t, e, u, "/", "doc.txt").ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("la subida truncada no debe crear una versión nueva: %d versiones", len(versions))
	}

	// Archivo nuevo truncado: no debe llegar a existir.
	ctx2, tr2 := newCtx()
	f2, err := fs.OpenFile(ctx2, "/nuevo.txt", writeFlags, 0o666)
	if err != nil {
		t.Fatalf("OpenFile falló: %v", err)
	}
	io.WriteString(f2, "medio fichero")
	tr2.set(io.ErrUnexpectedEOF)
	if err := f2.Close(); err == nil {
		t.Error("Close de una subida truncada debería devolver error")
	}
	if _, err := fs.Stat(ctx, "/nuevo.txt"); !os.IsNotExist(err) {
		t.Errorf("el archivo truncado no debería existir: err = %v", err)
	}
}

func TestOverwritingWithPutKeepsThePreviousContentAsAVersion(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	u := e.user(t, "maria")
	fs := e.fs(u)

	putFile(t, fs, ctx, "/informe.txt", "v1")
	putFile(t, fs, ctx, "/informe.txt", "v2")

	if got := readContent(t, fs, ctx, "/informe.txt"); got != "v2" {
		t.Errorf("contenido actual = %q, esperado v2", got)
	}
	versions, err := e.files.ListVersions(ctx, u.ID, fileMeta(t, e, u, "/", "informe.txt").ID)
	if err != nil {
		t.Fatalf("ListVersions falló: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("versiones = %d, esperada 1 (la v1 desplazada) -- WebDAV usa el mismo versionado que la API", len(versions))
	}
}

func TestPutOnANameHeldByTheTrashFails(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	fs := e.fs(e.user(t, "maria"))
	putFile(t, fs, ctx, "/x.txt", "viejo")
	if err := fs.RemoveAll(ctx, "/x.txt"); err != nil {
		t.Fatalf("RemoveAll falló: %v", err)
	}

	// §128: un upload no resucita/sobrescribe en silencio algo que está en
	// la papelera; misma regla que la API REST.
	f, err := fs.OpenFile(ctx, "/x.txt", writeFlags, 0o666)
	if err != nil {
		t.Fatalf("OpenFile falló: %v", err)
	}
	io.WriteString(f, "nuevo")
	if err := f.Close(); !os.IsExist(err) {
		t.Errorf("Close: err = %v, esperado os.IsExist (nombre ocupado por la papelera)", err)
	}
}

func TestFileSystemsOfDifferentUsersAreIsolated(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	a, b := e.fs(e.user(t, "ana")), e.fs(e.user(t, "beto"))

	putFile(t, a, ctx, "/secreto.txt", "solo de ana")

	if _, err := b.Stat(ctx, "/secreto.txt"); !os.IsNotExist(err) {
		t.Errorf("beto ve el archivo de ana: err = %v", err)
	}
	if err := b.RemoveAll(ctx, "/secreto.txt"); err != nil {
		t.Errorf("RemoveAll de algo que para beto no existe debería ser un no-op: %v", err)
	}
	if got := readContent(t, a, ctx, "/secreto.txt"); got != "solo de ana" {
		t.Errorf("el archivo de ana se alteró: %q", got)
	}
}

func TestReaddirFollowsTheHTTPFileContract(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	fs := e.fs(e.user(t, "maria"))
	putFile(t, fs, ctx, "/c.txt", "c")
	putFile(t, fs, ctx, "/a.txt", "a")
	if err := fs.Mkdir(ctx, "/b", 0o777); err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}

	dir, err := fs.OpenFile(ctx, "/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile de la raíz falló: %v", err)
	}
	defer dir.Close()

	first, err := dir.Readdir(2)
	if err != nil || len(first) != 2 || first[0].Name() != "a.txt" || first[1].Name() != "b" {
		t.Fatalf("Readdir(2) = %v, %v; esperado a.txt, b (ordenado por nombre)", names(first), err)
	}
	second, err := dir.Readdir(2)
	if err != nil || len(second) != 1 || second[0].Name() != "c.txt" {
		t.Fatalf("segundo Readdir(2) = %v, %v; esperado solo c.txt", names(second), err)
	}
	if rest, err := dir.Readdir(2); err != io.EOF || len(rest) != 0 {
		t.Errorf("Readdir agotado = %v, %v; esperado io.EOF", names(rest), err)
	}
}

func names(infos []os.FileInfo) []string {
	out := make([]string, 0, len(infos))
	for _, i := range infos {
		out = append(out, i.Name())
	}
	return out
}

func TestFileOperationsAreAuditedAsWebDAV(t *testing.T) {
	ctx := context.Background()
	e := newFSEnv(t, true)
	u := e.user(t, "maria")
	fs := e.fs(u)
	ipCtx := withRequestInfo(ctx, &requestInfo{ip: "203.0.113.9", method: "GET", tracker: &bodyTracker{}})

	putFile(t, fs, ipCtx, "/a.txt", "hola")
	readContent(t, fs, ipCtx, "/a.txt")
	if err := fs.Rename(ipCtx, "/a.txt", "/b.txt"); err != nil {
		t.Fatalf("Rename falló: %v", err)
	}
	if err := fs.RemoveAll(ipCtx, "/b.txt"); err != nil {
		t.Fatalf("RemoveAll falló: %v", err)
	}

	events, err := e.auditRepo.ListEvents(ctx, 50, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	seen := map[string]bool{}
	for _, ev := range events {
		if ev.Metadata["via"] == "webdav" {
			seen[ev.EventType] = true
			if ev.ActorUserID != u.ID || ev.IP != "203.0.113.9" {
				t.Errorf("evento %s: actor %q ip %q", ev.EventType, ev.ActorUserID, ev.IP)
			}
		}
	}
	for _, want := range []string{audit.EventUpload, audit.EventDownload, audit.EventMove, audit.EventDelete} {
		if !seen[want] {
			t.Errorf("falta el evento de auditoría %q con via=webdav (vistos: %v)", want, seen)
		}
	}
}

// Rename nunca sobrescribe por su cuenta: la sobrescritura solo existe dentro
// de un MOVE con Overwrite:T, después del RemoveAll(destino) que hace el
// handler de x/net. Sin ese paso previo, mover sobre algo que existe falla y
// no toca ni el origen ni el destino.
func TestRenameNeverOverwritesWithoutTheOverwriteHandshake(t *testing.T) {
	env := newFSEnv(t, true)
	fs := env.fs(env.user(t, "maria"))
	ctx := withRequestInfo(context.Background(), &requestInfo{method: "MOVE", tracker: &bodyTracker{}})

	putFile(t, fs, ctx, "/x.txt", "nuevo")
	putFile(t, fs, ctx, "/y.txt", "viejo")

	if err := fs.Rename(ctx, "/x.txt", "/y.txt"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("Rename sobre un destino existente sin handshake: err = %v, esperado os.ErrExist", err)
	}
	if got := readContent(t, fs, ctx, "/y.txt"); got != "viejo" {
		t.Errorf("el destino quedó con %q, esperado %q", got, "viejo")
	}
	if got := readContent(t, fs, ctx, "/x.txt"); got != "nuevo" {
		t.Errorf("el origen quedó con %q, esperado %q", got, "nuevo")
	}
}

// RemoveAll solo significa "borrar" fuera de un MOVE/COPY. Dentro de uno (y con
// la papelera activa) es el primer paso de una sobrescritura: no borra un
// archivo (lo hará el paso siguiente reemplazando su contenido) y rechaza una
// carpeta sin tocarla.
func TestRemoveAllInsideAnOverwriteDoesNotDeleteAFileAndRefusesACollection(t *testing.T) {
	env := newFSEnv(t, true)
	fs := env.fs(env.user(t, "maria"))
	plain := context.Background()
	move := withRequestInfo(plain, &requestInfo{method: "MOVE", tracker: &bodyTracker{}})
	del := withRequestInfo(plain, &requestInfo{method: "DELETE", tracker: &bodyTracker{}})

	putFile(t, fs, plain, "/y.txt", "viejo")
	if err := fs.Mkdir(plain, "/d", 0o755); err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}

	if err := fs.RemoveAll(move, "/y.txt"); err != nil {
		t.Fatalf("RemoveAll de un archivo dentro de un MOVE: %v", err)
	}
	if got := readContent(t, fs, plain, "/y.txt"); got != "viejo" {
		t.Errorf("el archivo debía seguir intacto, tiene %q", got)
	}
	if err := fs.RemoveAll(move, "/d"); err == nil {
		t.Error("RemoveAll de una carpeta dentro de un MOVE debería rechazarse")
	}
	if _, err := fs.Stat(plain, "/d"); err != nil {
		t.Errorf("la carpeta rechazada debía seguir existiendo: %v", err)
	}

	// Un DELETE de verdad sí borra (va a la papelera).
	if err := fs.RemoveAll(del, "/y.txt"); err != nil {
		t.Fatalf("RemoveAll dentro de un DELETE: %v", err)
	}
	if _, err := fs.Stat(plain, "/y.txt"); !os.IsNotExist(err) {
		t.Errorf("tras el DELETE el archivo no debería existir, err = %v", err)
	}
}
