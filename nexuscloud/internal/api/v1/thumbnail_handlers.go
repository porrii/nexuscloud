package apiv1

import (
	"bytes"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
)

// GetFileThumbnail sirve la miniatura de un archivo, generándola bajo
// demanda si hace falta (§34, ADR-041 Decisión 1) -- mismo chequeo de
// propiedad/compartición que DownloadFile (GenerateOrGetThumbnail lo hace
// internamente). Con thumbnails.enabled=false, writeFileError responde 404
// (ErrThumbnailsDisabled) -- nunca se registra un flag propio en Handlers
// para desactivar la ruta entera, mismo criterio que
// sharing.anonymousUploadEnabled: el interruptor se comprueba dentro de
// FileService, no aquí.
//
// Reutiliza serveFileContent para heredar Content-Disposition/
// X-Frame-Options/nosniff sin duplicar esa lógica (nota del pase de
// seguridad sobre el diseño). Siempre JPEG (Decisión 2), sea cual sea el
// archivo original -- por eso mimeType/nombre van fijos aquí, no los de
// meta. Sin X-Content-SHA256 real: esos bytes son un derivado (el redimensionado),
// no el contenido original, así que su sha256 no pinta nada útil ahí.
func (h *Handlers) GetFileThumbnail(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	meta, data, becameFailed, err := h.Files.GenerateOrGetThumbnail(r.Context(), u.ID, id)
	if becameFailed {
		// Solo al agotar los reintentos (§34 Decisión 1), nunca en cada
		// intento intermedio -- becameFailed implica err != nil siempre
		// (ver el propio contrato de GenerateOrGetThumbnail).
		h.AuditLog.Record(r.Context(), audit.EventThumbnailGenerationFailed, u.ID, "file", id,
			security.ClientIP(r, h.TrustedProxies), map[string]any{"error": err.Error()})
	}
	if err != nil {
		writeFileError(w, err)
		return
	}

	name := meta.Name + ".jpg"
	if err := serveFileContent(w, r, name, "image/jpeg", "", int64(len(data)), io.NopCloser(bytes.NewReader(data))); err != nil {
		h.Logger.Warn("interrumpida la descarga de miniatura", "file_id", meta.ID, "error", err)
	}
}

// ListThumbnailJobs es admin-only (montado bajo RequireAdmin en router.go)
// -- visibilidad sobre la cola persistente de miniaturas (§34, ADR-041
// Decisión 1): qué está pendiente o qué se dio por fallido tras agotar
// reintentos. Es precisamente la razón por la que se eligió una tabla en
// vez de una cola en memoria -- sin esto, un admin no tendría forma de
// saber que ffmpeg no está instalado salvo mirando logs.
func (h *Handlers) ListThumbnailJobs(w http.ResponseWriter, r *http.Request) {
	status := storage.ThumbnailJobPending
	if v := r.URL.Query().Get("status"); v != "" {
		switch storage.ThumbnailJobStatus(v) {
		case storage.ThumbnailJobPending, storage.ThumbnailJobFailed:
			status = storage.ThumbnailJobStatus(v)
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", "El parámetro 'status' debe ser 'pending' o 'failed'.")
			return
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	jobs, err := h.Files.ListThumbnailJobs(r.Context(), status, limit, offset)
	if err != nil {
		h.Logger.Error("listando jobs de miniatura", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los jobs de miniatura.")
		return
	}
	out := make([]thumbnailJobResponse, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toThumbnailJobResponse(j))
	}
	writeJSON(w, http.StatusOK, out)
}
