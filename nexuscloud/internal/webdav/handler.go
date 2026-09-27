package webdav

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	xwebdav "golang.org/x/net/webdav"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

// chi solo conoce los métodos HTTP estándar: sin registrar estos, un
// Mount(...) de este handler respondería 405 a PROPFIND/MKCOL/MOVE/... antes
// de llegar a él. RegisterMethod es global e idempotente, y debe ejecutarse
// antes de construir el router -- de ahí el init().
func init() {
	for _, m := range []string{"PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK"} {
		chi.RegisterMethod(m)
	}
}

// Options son los ajustes de WebDAV que vienen de la configuración (§43
// "configurarse" y "limitarse").
type Options struct {
	// Prefix es la ruta donde está montado el handler (p.ej. "/webdav"), sin
	// barra final: x/net la necesita para generar bien los href.
	Prefix string
	// ReadOnly limita WebDAV a OPTIONS/GET/HEAD/PROPFIND.
	ReadOnly bool
	// MaxUploadSizeBytes limita el tamaño de una subida (0 = sin límite).
	MaxUploadSizeBytes int64
	TrustedProxies     []string
}

// Handler es el módulo WebDAV completo: autentica con HTTP Basic (usuario +
// token de acceso WebDAV, nunca la contraseña de la cuenta, ADR-034), aplica
// los límites configurados y delega en x/net/webdav sobre el árbol de
// ficheros del usuario autenticado.
type Handler struct {
	opts   Options
	files  *storage.FileService
	tokens *TokenService
	audit  *audit.Recorder
	logger *slog.Logger

	mu      sync.Mutex
	perUser map[string]*xwebdav.Handler
}

func NewHandler(files *storage.FileService, tokens *TokenService, rec *audit.Recorder, opts Options, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		opts: opts, files: files, tokens: tokens, audit: rec, logger: logger,
		perUser: make(map[string]*xwebdav.Handler),
	}
}

// requestInfo viaja en el contexto de cada petición hacia el FileSystem.
type requestInfo struct {
	ip      string
	method  string
	tracker *bodyTracker
	// overwriteOf es el destino (ruta normalizada) de un MOVE/COPY con
	// Overwrite:T cuyo "borrado previo" ya se atendió como inicio de
	// sobrescritura; vacío si no hay ninguna en curso. Ver
	// fileSystem.beginOverwrite. Vive en la petición y solo la toca el
	// goroutine que la sirve.
	overwriteOf string
	// putSize es el Content-Length anunciado por un PUT (0 = desconocido o no
	// es un PUT): sirve para rechazar por cuota antes de leer el cuerpo.
	putSize int64
	// quotaExceeded se activa cuando una subida de esta petición fue rechazada
	// por la cuota del propietario; ver quotaStatusWriter.
	quotaExceeded atomic.Bool
}

// noteQuota marca la petición como rechazada por cuota si err lo es, para que
// la respuesta salga como 507 (ver quotaStatusWriter).
func noteQuota(ctx context.Context, err error) {
	if errors.Is(err, storage.ErrQuotaExceeded) {
		if ri := requestInfoFrom(ctx); ri != nil {
			ri.quotaExceeded.Store(true)
		}
	}
}

// quotaExceededMessage es el cuerpo de la respuesta 507 (mismo texto que la
// API REST, internal/api/v1/quota_handlers.go).
const quotaExceededMessage = "No hay espacio suficiente: has alcanzado tu cuota de almacenamiento. Libera espacio (la papelera y las versiones anteriores también cuentan) o pide al administrador que la amplíe."

// quotaStatusWriter cambia a 507 Insufficient Storage la respuesta de error de
// una petición rechazada por la cuota (ADR-036). El handler de x/net responde
// 405 a cualquier PUT que falla (y 500 o 403 a un COPY o un MOVE), y un cliente
// WebDAV solo entiende «no hay espacio» con 507 (RFC 4918); además el cuerpo
// que escribiría x/net («Method Not Allowed») no explica nada.
type quotaStatusWriter struct {
	http.ResponseWriter
	ri          *requestInfo
	rewritten   bool
	messageSent bool
}

func (w *quotaStatusWriter) WriteHeader(code int) {
	if code >= 400 && w.ri.quotaExceeded.Load() {
		w.rewritten = true
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		code = http.StatusInsufficientStorage
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *quotaStatusWriter) Write(p []byte) (int, error) {
	if !w.rewritten {
		return w.ResponseWriter.Write(p)
	}
	// Se descarta el texto de x/net y se escribe el motivo real, una sola vez.
	if !w.messageSent {
		w.messageSent = true
		if _, err := io.WriteString(w.ResponseWriter, quotaExceededMessage); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Unwrap deja que http.ResponseController alcance el ResponseWriter real.
func (w *quotaStatusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type ctxKey struct{}

func withRequestInfo(ctx context.Context, ri *requestInfo) context.Context {
	return context.WithValue(ctx, ctxKey{}, ri)
}

func requestInfoFrom(ctx context.Context) *requestInfo {
	ri, _ := ctx.Value(ctxKey{}).(*requestInfo)
	return ri
}

// bodyTracker recuerda el primer error (distinto de io.EOF) que dio el
// cuerpo de la petición. Ver uploadFile: el handler de x/net llama a Close()
// aunque la copia del cuerpo haya fallado, y solo así puede distinguirse una
// subida completa de una cortada a mitad.
type bodyTracker struct {
	mu sync.Mutex
	e  error
}

func (t *bodyTracker) set(err error) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.e == nil {
		t.e = err
	}
}

// err es seguro sobre un *bodyTracker nil (peticiones sin cuerpo).
func (t *bodyTracker) err() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.e
}

type trackingBody struct {
	io.ReadCloser
	t *bodyTracker
}

func (b *trackingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.t.set(err)
	return n, err
}

// isReadMethod es la lista de métodos permitidos con readOnly=true.
func isReadMethod(m string) bool {
	switch m {
	case http.MethodOptions, http.MethodGet, http.MethodHead, "PROPFIND":
		return true
	}
	return false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := security.ClientIP(r, h.opts.TrustedProxies)

	username, token, ok := r.BasicAuth()
	if !ok {
		// Primer intento normal de cualquier cliente WebDAV (aún no sabe que
		// hay que autenticarse): no es un fallo que auditar.
		challenge(w)
		return
	}
	u, err := h.tokens.Authenticate(r.Context(), username, token)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			h.audit.Record(r.Context(), audit.EventWebDAVAuthFailed, "", "user", username, ip, nil)
			challenge(w)
			return
		}
		h.logger.Error("autenticando petición WebDAV", "error", err)
		http.Error(w, "Error interno.", http.StatusInternalServerError)
		return
	}

	if h.opts.ReadOnly && !isReadMethod(r.Method) {
		http.Error(w, "WebDAV está en modo solo lectura en esta instancia.", http.StatusForbidden)
		return
	}
	limit := h.opts.MaxUploadSizeBytes
	if limit > 0 && r.ContentLength > limit {
		http.Error(w, "El contenido supera el tamaño máximo de subida de esta instancia.", http.StatusRequestEntityTooLarge)
		return
	}

	tracker := &bodyTracker{}
	if r.Body != nil && r.Body != http.NoBody {
		body := r.Body
		if limit > 0 {
			// Cubre las subidas sin Content-Length (chunked): el error
			// "request body too large" también lo recoge el tracker.
			body = http.MaxBytesReader(w, body, limit)
		}
		r.Body = &trackingBody{ReadCloser: body, t: tracker}
	}

	ri := &requestInfo{ip: ip, method: r.Method, tracker: tracker}
	if r.Method == http.MethodPut && r.ContentLength > 0 {
		ri.putSize = r.ContentLength
	}
	ctx := withRequestInfo(r.Context(), ri)
	h.handlerFor(u).ServeHTTP(&quotaStatusWriter{ResponseWriter: w, ri: ri}, r.WithContext(ctx))
}

func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="NexusCloud WebDAV", charset="UTF-8"`)
	http.Error(w, "No autorizado.", http.StatusUnauthorized)
}

// handlerFor devuelve el webdav.Handler del usuario, creándolo la primera
// vez. Cada usuario tiene el suyo -- con su FileSystem y su LockSystem
// propios -- por dos motivos: la identidad del dueño es estructural en vez de
// ambiental, y los locks de x/net se indexan por path relativo: una única
// MemLS compartida haría que dos usuarios con un "/informe.doc" se bloqueasen
// entre sí.
func (h *Handler) handlerFor(u *users.User) *xwebdav.Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d, ok := h.perUser[u.ID]; ok {
		return d
	}
	d := &xwebdav.Handler{
		Prefix:     h.opts.Prefix,
		FileSystem: newFileSystem(h.files, u, h.audit),
		LockSystem: newLockSystem(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				h.logger.Debug("petición WebDAV con error", "method", r.Method, "path", r.URL.Path, "error", err)
			}
		},
	}
	h.perUser[u.ID] = d
	return d
}
