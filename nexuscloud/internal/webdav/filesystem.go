package webdav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path"
	"sort"
	"time"

	xwebdav "golang.org/x/net/webdav"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

// fileSystem adapta storage.FileService a webdav.FileSystem para UN usuario
// (cada usuario autenticado tiene la suya, ver Handler.handlerFor): la
// identidad del dueño es estructural, nunca sale del contexto de la
// petición, así que ninguna llamada puede operar sobre el árbol de otro.
// Todo pasa por FileService -- papelera, versionado, pools, validación de
// nombres -- exactamente igual que la API REST: nunca por el disco directo.
type fileSystem struct {
	files       *storage.FileService
	owner       string
	rootModTime time.Time
	audit       *audit.Recorder
}

func newFileSystem(files *storage.FileService, u *users.User, rec *audit.Recorder) *fileSystem {
	return &fileSystem{files: files, owner: u.ID, rootModTime: u.CreatedAt, audit: rec}
}

// cleanPath normaliza el path que entrega el handler de x/net (puede ser ""
// para la raíz, o traer ".." si un cliente lo intenta) a una ruta lógica
// absoluta y limpia; nada puede escapar de "/", el árbol del propio usuario.
func cleanPath(name string) string { return path.Clean("/" + name) }

// entry es lo que resuelve un path: la raíz, una carpeta o un archivo.
type entry struct {
	root bool
	dir  *storage.Directory
	file *storage.FileMeta
}

func (e *entry) isDir() bool { return e.root || e.dir != nil }

// lookup resuelve un path listando su carpeta padre (FileService no tiene
// "buscar por path", y List ya devuelve solo lo activo, sin papelera).
func (f *fileSystem) lookup(ctx context.Context, name string) (*entry, error) {
	clean := cleanPath(name)
	if clean == "/" {
		return &entry{root: true}, nil
	}
	res, err := f.files.List(ctx, f.owner, path.Dir(clean))
	if err != nil {
		return nil, mapError(err)
	}
	base := path.Base(clean)
	for _, d := range res.Directories {
		if d.Name == base {
			return &entry{dir: d}, nil
		}
	}
	for _, m := range res.Files {
		if m.Name == base {
			return &entry{file: m}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (f *fileSystem) infoOf(e *entry) *fileInfo {
	switch {
	case e.root:
		return &fileInfo{name: "/", isDir: true, modTime: f.rootModTime}
	case e.dir != nil:
		return &fileInfo{name: e.dir.Name, isDir: true, modTime: e.dir.CreatedAt}
	default:
		return &fileInfo{name: e.file.Name, size: e.file.SizeBytes, modTime: e.file.UpdatedAt, sha256: e.file.SHA256, mime: e.file.MimeType}
	}
}

func (f *fileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	e, err := f.lookup(ctx, name)
	if err != nil {
		return nil, err
	}
	return f.infoOf(e), nil
}

// requireParentDir comprueba que el padre de clean existe y es una carpeta:
// el handler de x/net traduce os.ErrNotExist a 409 Conflict (lo que exige
// RFC 4918 para MKCOL/PUT/MOVE con un intermedio inexistente).
func (f *fileSystem) requireParentDir(ctx context.Context, clean string) error {
	parent := path.Dir(clean)
	if parent == "/" {
		return nil
	}
	pe, err := f.lookup(ctx, parent)
	if err != nil {
		return err
	}
	if !pe.isDir() {
		return os.ErrNotExist
	}
	return nil
}

func (f *fileSystem) Mkdir(ctx context.Context, name string, _ os.FileMode) error {
	clean := cleanPath(name)
	if clean == "/" {
		return os.ErrExist
	}
	// FileService.Mkdir es idempotente, pero MKCOL sobre algo que ya existe
	// debe fallar (405) -- lo comprobamos aquí.
	if _, err := f.lookup(ctx, clean); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := f.requireParentDir(ctx, clean); err != nil {
		return err
	}
	if _, err := f.files.Mkdir(ctx, f.owner, path.Dir(clean), path.Base(clean), ""); err != nil {
		return mapError(err)
	}
	return nil
}

func (f *fileSystem) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (xwebdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return f.openForWrite(ctx, name)
	}
	return f.openForRead(ctx, name)
}

func (f *fileSystem) openForRead(ctx context.Context, name string) (xwebdav.File, error) {
	e, err := f.lookup(ctx, name)
	if err != nil {
		return nil, err
	}
	info := f.infoOf(e)
	if e.isDir() {
		return &dirFile{ctx: ctx, fs: f, path: cleanPath(name), info: info}, nil
	}

	_, rc, err := f.files.Download(ctx, f.owner, e.file.ID)
	if err != nil {
		return nil, mapError(err)
	}
	// http.ServeContent (GET/HEAD y Range) necesita Seek. El provider local
	// devuelve un *os.File, que lo tiene; un provider futuro que no lo
	// ofrezca falla aquí con un error claro (límite documentado en ADR-034).
	rs, ok := rc.(io.ReadSeeker)
	if !ok {
		rc.Close()
		return nil, errors.New("webdav: el provider de este pool no permite lecturas con Seek")
	}
	if ri := requestInfoFrom(ctx); ri != nil && ri.method == "GET" {
		f.audit.Record(ctx, audit.EventDownload, f.owner, "file", e.file.ID, ri.ip, map[string]any{"via": "webdav"})
	}
	return &readFile{rs: rs, closer: rc, info: info}, nil
}

func (f *fileSystem) openForWrite(ctx context.Context, name string) (xwebdav.File, error) {
	clean := cleanPath(name)
	if clean == "/" {
		return nil, os.ErrInvalid
	}
	if err := f.requireParentDir(ctx, clean); err != nil {
		return nil, err
	}
	if e, err := f.lookup(ctx, clean); err == nil && e.isDir() {
		return nil, os.ErrInvalid // PUT sobre una colección
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return newUploadFile(ctx, f, path.Dir(clean), path.Base(clean)), nil
}

// errOverwriteCollection es el motivo (visible en el log del handler) por el
// que un MOVE/COPY con Overwrite:T sobre una carpeta que existe se rechaza con
// 403 en vez de reemplazarla. Ver beginOverwrite.
var errOverwriteCollection = errors.New("webdav: con la papelera activa no se puede reemplazar una carpeta entera con MOVE/COPY (sus nombres quedarían reservados por la papelera); bórrala antes")

// RemoveAll sigue la semántica de WebDAV (RFC 4918 §9.6): borrar una
// colección borra TODO su contenido. FileService.DeleteDirectory solo admite
// carpetas vacías, así que se recorre el árbol; cada archivo va a la papelera
// (o se borra, con la papelera desactivada) igual que por la API. No es
// atómico: si algo falla a medias, lo ya borrado queda borrado.
//
// Dentro de un MOVE/COPY con la papelera activa no significa "borrar", sino
// "empezar una sobrescritura": ver beginOverwrite.
func (f *fileSystem) RemoveAll(ctx context.Context, name string) error {
	clean := cleanPath(name)
	if clean == "/" {
		return os.ErrPermission
	}
	if ri := requestInfoFrom(ctx); ri != nil && (ri.method == "MOVE" || ri.method == "COPY") && f.files.TrashEnabled() {
		return f.beginOverwrite(ctx, ri, clean)
	}
	e, err := f.lookup(ctx, clean)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // como os.RemoveAll; el handler ya hizo Stat antes para dar 404
		}
		return err
	}
	if e.file != nil {
		return f.deleteFile(ctx, e.file)
	}
	return f.removeTree(ctx, clean, e.dir)
}

// beginOverwrite atiende el RemoveAll(destino) que el handler de x/net hace
// antes de un MOVE/COPY con Overwrite:T sobre un destino que existe (RFC 4918
// §9.8.4/§9.9.3: "borrar el destino y luego crear/mover encima").
//
// Con la papelera activa, hacerlo literalmente es una trampa: el destino iría
// a la papelera, su nombre quedaría reservado (§128, ADR-030) y el paso
// siguiente (crear/renombrar encima) fallaría -- el cliente vería un error
// pero el destino ya habría salido del árbol. Por eso aquí NO se borra nada:
//
//   - Destino archivo: se anota la sobrescritura y se sigue. El COPY sube
//     encima (openForWrite) y el MOVE copia el contenido encima
//     (moveOverFile); en ambos el contenido anterior queda como versión del
//     destino, igual que un PUT sobre un archivo existente.
//   - Destino carpeta: se rechaza (403) sin tocar nada. Reemplazar un árbol
//     entero exigiría borrar y volver a crear los mismos nombres, imposible
//     mientras la papelera los reserve.
//
// Con la papelera desactivada no se llega aquí: el borrado es físico, los
// nombres quedan libres y la semántica completa del RFC funciona tal cual.
func (f *fileSystem) beginOverwrite(ctx context.Context, ri *requestInfo, dst string) error {
	e, err := f.lookup(ctx, dst)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // nada que sobrescribir, como os.RemoveAll
		}
		return err
	}
	if e.file == nil {
		return errOverwriteCollection
	}
	ri.overwriteOf = dst
	return nil
}

func (f *fileSystem) removeTree(ctx context.Context, dirPath string, dir *storage.Directory) error {
	res, err := f.files.List(ctx, f.owner, dirPath)
	if err != nil {
		return mapError(err)
	}
	for _, m := range res.Files {
		if err := f.deleteFile(ctx, m); err != nil {
			return err
		}
	}
	for _, d := range res.Directories {
		if err := f.removeTree(ctx, path.Join(dirPath, d.Name), d); err != nil {
			return err
		}
	}
	if err := f.files.DeleteDirectory(ctx, f.owner, dir.ID); err != nil {
		return mapError(err)
	}
	f.recordAudit(ctx, audit.EventDelete, "directory", dir.ID, nil)
	return nil
}

func (f *fileSystem) deleteFile(ctx context.Context, m *storage.FileMeta) error {
	if err := f.files.Delete(ctx, f.owner, m.ID); err != nil {
		return mapError(err)
	}
	f.recordAudit(ctx, audit.EventDelete, "file", m.ID, nil)
	return nil
}

// Rename implementa MOVE. Nunca sobrescribe por su cuenta: mover sobre un
// destino que existe solo es posible tras beginOverwrite (el RemoveAll previo
// que hace x/net con Overwrite:T); sin él, FileService.MoveFile rechaza el
// destino ocupado y no se toca nada.
func (f *fileSystem) Rename(ctx context.Context, oldName, newName string) error {
	oldClean, newClean := cleanPath(oldName), cleanPath(newName)
	if oldClean == "/" || newClean == "/" {
		return os.ErrPermission
	}
	if oldClean == newClean {
		return nil
	}
	src, err := f.lookup(ctx, oldClean)
	if err != nil {
		return err
	}
	if err := f.requireParentDir(ctx, newClean); err != nil {
		return err
	}
	if ri := requestInfoFrom(ctx); ri != nil && ri.overwriteOf == newClean {
		ri.overwriteOf = "" // el acuerdo es de un solo uso
		return f.moveOverFile(ctx, src, newClean)
	}

	newParent, newBase := path.Dir(newClean), path.Base(newClean)
	if src.file != nil {
		moved, err := f.files.MoveFile(ctx, f.owner, src.file.ID, &newParent, &newBase)
		if err != nil {
			return mapError(err)
		}
		f.recordAudit(ctx, audit.EventMove, "file", moved.ID, map[string]any{"parent_path": moved.ParentPath, "name": moved.Name})
		return nil
	}
	moved, err := f.files.MoveDirectory(ctx, f.owner, src.dir.ID, &newParent, &newBase)
	if err != nil {
		return mapError(err)
	}
	f.recordAudit(ctx, audit.EventMove, "directory", moved.ID, map[string]any{"parent_path": moved.ParentPath, "name": moved.Name})
	return nil
}

// moveOverFile completa un MOVE con Overwrite:T sobre un archivo que existe
// (ver beginOverwrite): el contenido del origen pasa a ser el vigente del
// destino -- y el anterior queda como versión, como en cualquier subida sobre
// un archivo existente -- y el origen se borra, que es lo que significa mover.
// Sube antes de borrar, así que un fallo intermedio deja copias de sobra,
// nunca pérdida.
func (f *fileSystem) moveOverFile(ctx context.Context, src *entry, dst string) error {
	if src.file == nil {
		return os.ErrExist // una carpeta no puede reemplazar a un archivo; no se ha tocado nada
	}
	_, rc, err := f.files.Download(ctx, f.owner, src.file.ID)
	if err != nil {
		return mapError(err)
	}
	defer rc.Close()

	meta, err := f.files.Upload(ctx, storage.UploadInput{OwnerID: f.owner, ParentPath: path.Dir(dst), Name: path.Base(dst), Content: rc})
	if err != nil {
		// Aquí solo se llega con la papelera activa (ver beginOverwrite): el
		// origen se borra a la papelera y sigue ocupando, así que la cuota
		// cuenta el contenido duplicado y este MOVE puede no caber (ADR-036).
		noteQuota(ctx, err)
		return mapError(err)
	}
	f.recordAudit(ctx, audit.EventUpload, "file", meta.ID, map[string]any{"name": meta.Name, "size_bytes": meta.SizeBytes})
	return f.deleteFile(ctx, src.file)
}

// recordAudit registra una operación de fichero hecha por WebDAV con los
// mismos tipos de evento que la API REST (§43 "auditarse"), marcada con
// via=webdav para distinguirlas.
func (f *fileSystem) recordAudit(ctx context.Context, eventType, targetType, targetID string, details map[string]any) {
	ip := ""
	if ri := requestInfoFrom(ctx); ri != nil {
		ip = ri.ip
	}
	meta := map[string]any{"via": "webdav"}
	for k, v := range details {
		meta[k] = v
	}
	f.audit.Record(ctx, eventType, f.owner, targetType, targetID, ip, meta)
}

// mapError traduce los errores de FileService a los de os que entiende el
// handler de x/net para escoger el código HTTP (409 con os.ErrNotExist en
// MKCOL/PUT, 405/403 con el resto).
//
// Devuelve un *os.PathError y no un error envuelto con %w a propósito:
// el handler de x/net usa os.IsNotExist, que -a diferencia de errors.Is- NO
// desenvuelve errores arbitrarios, solo PathError/LinkError/SyscallError. Un
// %w daría 404/405 donde el protocolo exige 409. El texto original de
// FileService (p.ej. "ya hay un elemento con ese nombre en la papelera")
// viaja en Path solo para que el log del handler conserve el motivo real.
func mapError(err error) error {
	kind := func(sentinel error) error {
		return &os.PathError{Op: "webdav", Path: err.Error(), Err: sentinel}
	}
	switch {
	case errors.Is(err, storage.ErrFileNotFound), errors.Is(err, storage.ErrDirectoryNotFound):
		return kind(os.ErrNotExist)
	case errors.Is(err, storage.ErrForbidden):
		return kind(os.ErrPermission)
	case errors.Is(err, storage.ErrNameOccupiedByTrash), errors.Is(err, storage.ErrDestinationOccupied):
		return kind(os.ErrExist)
	case errors.Is(err, storage.ErrInvalidName), errors.Is(err, storage.ErrInvalidPath),
		errors.Is(err, storage.ErrPathEscapesRoot), errors.Is(err, storage.ErrInvalidMoveDestination):
		return kind(os.ErrInvalid)
	default:
		return err
	}
}

// fileInfo implementa os.FileInfo más ETager y ContentTyper de x/net: el
// ETag es el SHA-256 real del contenido (mismo que FileMeta.SHA256) y el
// tipo MIME el que ya calculó FileService, en vez de la heurística de
// mtime+tamaño y de abrir el fichero para olfatear su tipo.
type fileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
	sha256  string
	mime    string
}

func (i *fileInfo) Name() string       { return i.name }
func (i *fileInfo) Size() int64        { return i.size }
func (i *fileInfo) ModTime() time.Time { return i.modTime }
func (i *fileInfo) IsDir() bool        { return i.isDir }
func (i *fileInfo) Sys() any           { return nil }
func (i *fileInfo) Mode() os.FileMode {
	if i.isDir {
		return os.ModeDir | 0o755
	}
	return 0o644
}

func (i *fileInfo) ETag(context.Context) (string, error) {
	if i.sha256 == "" {
		return "", xwebdav.ErrNotImplemented
	}
	return `"` + i.sha256 + `"`, nil
}

func (i *fileInfo) ContentType(context.Context) (string, error) {
	if i.mime == "" {
		return "", xwebdav.ErrNotImplemented
	}
	return i.mime, nil
}

// readFile es un archivo abierto para lectura (GET/HEAD/Range, o el origen
// de un COPY).
type readFile struct {
	rs     io.ReadSeeker
	closer io.Closer
	info   *fileInfo
}

func (r *readFile) Read(p []byte) (int, error)                { return r.rs.Read(p) }
func (r *readFile) Seek(off int64, whence int) (int64, error) { return r.rs.Seek(off, whence) }
func (r *readFile) Close() error                              { return r.closer.Close() }
func (r *readFile) Stat() (os.FileInfo, error)                { return r.info, nil }
func (r *readFile) Write([]byte) (int, error)                 { return 0, os.ErrPermission }
func (r *readFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, errors.New("webdav: no es una carpeta")
}

// dirFile es una carpeta abierta: solo sirve para Readdir (PROPFIND).
type dirFile struct {
	ctx     context.Context
	fs      *fileSystem
	path    string
	info    *fileInfo
	entries []os.FileInfo
	loaded  bool
	cursor  int
}

func (d *dirFile) Read([]byte) (int, error)       { return 0, errors.New("webdav: es una carpeta") }
func (d *dirFile) Seek(int64, int) (int64, error) { return 0, nil }
func (d *dirFile) Close() error                   { return nil }
func (d *dirFile) Stat() (os.FileInfo, error)     { return d.info, nil }
func (d *dirFile) Write([]byte) (int, error)      { return 0, os.ErrPermission }

// Readdir sigue el contrato de http.File: count <= 0 devuelve todo de una
// vez; count > 0 devuelve como mucho count entradas por llamada e io.EOF
// cuando ya no quedan.
func (d *dirFile) Readdir(count int) ([]os.FileInfo, error) {
	if !d.loaded {
		res, err := d.fs.files.List(d.ctx, d.fs.owner, d.path)
		if err != nil {
			return nil, mapError(err)
		}
		for _, dir := range res.Directories {
			d.entries = append(d.entries, &fileInfo{name: dir.Name, isDir: true, modTime: dir.CreatedAt})
		}
		for _, m := range res.Files {
			d.entries = append(d.entries, &fileInfo{name: m.Name, size: m.SizeBytes, modTime: m.UpdatedAt, sha256: m.SHA256, mime: m.MimeType})
		}
		sort.Slice(d.entries, func(i, j int) bool { return d.entries[i].Name() < d.entries[j].Name() })
		d.loaded = true
	}

	remaining := d.entries[d.cursor:]
	if count <= 0 {
		d.cursor = len(d.entries)
		return remaining, nil
	}
	if len(remaining) == 0 {
		return nil, io.EOF
	}
	if count > len(remaining) {
		count = len(remaining)
	}
	d.cursor += count
	return remaining[:count], nil
}

// uploadFile es un archivo abierto para escritura (PUT, o el destino de un
// COPY). El contenido se transmite por un io.Pipe directamente a
// FileService.Upload, que ya escribe a un temporal + rename atómico: sin
// spooling propio a disco (ficheros grandes sin doblar I/O) y un fallo nunca
// deja nada visible.
//
// El handler de x/net llama a Close() también cuando la copia del cuerpo
// falló a mitad (cliente cortado): sin protección, esa subida truncada se
// confirmaría como una versión nueva y válida. requestInfo.tracker recuerda
// si el cuerpo de la petición falló; en ese caso Close aborta la subida.
type uploadFile struct {
	ctx    context.Context
	fs     *fileSystem
	parent string
	name   string
	pw     *io.PipeWriter
	done   chan uploadResult
	sha    hash.Hash
	size   int64
	closed bool
	result uploadResult
}

type uploadResult struct {
	meta *storage.FileMeta
	err  error
}

func newUploadFile(ctx context.Context, fs *fileSystem, parent, name string) *uploadFile {
	pr, pw := io.Pipe()
	u := &uploadFile{ctx: ctx, fs: fs, parent: parent, name: name, pw: pw, done: make(chan uploadResult, 1), sha: sha256.New()}
	var sizeHint int64
	if ri := requestInfoFrom(ctx); ri != nil {
		sizeHint = ri.putSize // Content-Length del PUT: rechazo por cuota sin leer el cuerpo
	}
	go func() {
		meta, err := fs.files.Upload(ctx, storage.UploadInput{OwnerID: fs.owner, ParentPath: parent, Name: name, Content: pr, SizeHint: sizeHint})
		// Si Upload falló antes de leer todo (nombre inválido, ocupado por la
		// papelera...), el escritor recibe este error en vez de bloquearse.
		pr.CloseWithError(err)
		u.done <- uploadResult{meta: meta, err: err}
	}()
	return u
}

func (u *uploadFile) Write(p []byte) (int, error) {
	n, err := u.pw.Write(p)
	if n > 0 {
		u.sha.Write(p[:n])
		u.size += int64(n)
	}
	noteQuota(u.ctx, err) // si Upload cortó por cuota, es lo que llega aquí
	return n, err
}

// Stat lo llama el handler de x/net justo después de copiar el cuerpo y
// antes de Close, para construir el ETag de la respuesta: el SHA-256 ya es
// el definitivo (todo el contenido está escrito), el mismo que guardará
// FileService.
func (u *uploadFile) Stat() (os.FileInfo, error) {
	return &fileInfo{name: u.name, size: u.size, modTime: time.Now().UTC(), sha256: hex.EncodeToString(u.sha.Sum(nil))}, nil
}

func (u *uploadFile) Close() error {
	if u.closed {
		return u.result.err
	}
	u.closed = true

	if ri := requestInfoFrom(u.ctx); ri != nil {
		if bodyErr := ri.tracker.err(); bodyErr != nil {
			u.pw.CloseWithError(bodyErr)
			u.result = <-u.done
			return bodyErr
		}
	}
	u.pw.Close()
	u.result = <-u.done
	if u.result.err != nil {
		noteQuota(u.ctx, u.result.err)
		u.result.err = mapError(u.result.err)
		return u.result.err
	}
	u.fs.recordAudit(u.ctx, audit.EventUpload, "file", u.result.meta.ID, map[string]any{"name": u.result.meta.Name, "size_bytes": u.result.meta.SizeBytes})
	return nil
}

func (u *uploadFile) Read([]byte) (int, error)       { return 0, os.ErrPermission }
func (u *uploadFile) Seek(int64, int) (int64, error) { return 0, os.ErrPermission }
func (u *uploadFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, errors.New("webdav: no es una carpeta")
}
