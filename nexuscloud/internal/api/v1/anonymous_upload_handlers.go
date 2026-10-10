package apiv1

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
)

type createAnonymousUploadLinkRequest struct {
	DirectoryID        string     `json:"directory_id"`
	Label              string     `json:"label,omitempty"`
	MaxUploadSizeBytes *int64     `json:"max_upload_size_bytes,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
}

// CreateAnonymousUploadLink crea un enlace de subida anónima (§38, ADR-039)
// sobre una carpeta PROPIA del usuario autenticado. La respuesta incluye el
// token en claro una única vez, igual que shares/tokens de API.
func (h *Handlers) CreateAnonymousUploadLink(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	var req createAnonymousUploadLinkRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	if req.DirectoryID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "directory_id es obligatorio.")
		return
	}

	link, token, err := h.Files.CreateAnonymousUploadLink(r.Context(), u.ID, req.DirectoryID, req.Label, req.MaxUploadSizeBytes, req.ExpiresAt)
	if err != nil {
		writeAnonymousUploadError(w, err)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventAnonymousUploadLinkCreated, u.ID, "anonymous_upload", link.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"directory_id": link.DirectoryID})

	name, _ := h.Files.DirectoryNameForAnonymousUpload(r.Context(), link)
	out := toAnonymousUploadResponse(link, name)
	out.Token = token
	writeJSON(w, http.StatusCreated, out)
}

// ListAnonymousUploadLinks devuelve solo los enlaces del usuario autenticado
// -- ver storage.FileService.ListAnonymousUploadLinks.
func (h *Handlers) ListAnonymousUploadLinks(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())

	links, err := h.Files.ListAnonymousUploadLinks(r.Context(), u.ID)
	if err != nil {
		writeAnonymousUploadError(w, err)
		return
	}

	out := make([]anonymousUploadResponse, 0, len(links))
	for _, link := range links {
		name, _ := h.Files.DirectoryNameForAnonymousUpload(r.Context(), link)
		out = append(out, toAnonymousUploadResponse(link, name))
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeAnonymousUploadLink exige ser quien creó el enlace (IDOR-safe, ver
// storage.SQLAnonymousUploadRepository.RevokeLink); es un soft-revoke.
func (h *Handlers) RevokeAnonymousUploadLink(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.Files.RevokeAnonymousUploadLink(r.Context(), u.ID, id); err != nil {
		writeAnonymousUploadError(w, err)
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventAnonymousUploadLinkRevoked, u.ID, "anonymous_upload", id, security.ClientIP(r, h.TrustedProxies), nil)
	w.WriteHeader(http.StatusNoContent)
}

// anonymousUploadInfoResponse es la respuesta pública del probe (§38): SOLO
// label y el límite de tamaño si lo hay -- nunca el propietario, la carpeta
// ni su contenido, coherente con que no existe ningún endpoint de
// navegación para este modelo.
type anonymousUploadInfoResponse struct {
	Label              string `json:"label,omitempty"`
	MaxUploadSizeBytes *int64 `json:"max_upload_size_bytes,omitempty"`
}

// GetAnonymousUploadLink es el probe público (§38, plan §7): un único 404
// genérico para caducado/revocado/inexistente -- a diferencia de
// GetPublicShare, aquí no hay ningún propietario autenticado al otro lado
// que necesite distinguir el motivo, así que distinguirlo solo daría más
// superficie de enumeración a quien solo tiene el token.
func (h *Handlers) GetAnonymousUploadLink(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	link, err := h.Files.ResolveAnonymousUploadForAccess(r.Context(), token)
	if err != nil {
		writeAnonymousUploadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, anonymousUploadInfoResponse{Label: link.Label, MaxUploadSizeBytes: link.MaxUploadSizeBytes})
}

// UploadAnonymousUploadLink recibe el contenido como cuerpo crudo, en
// streaming (mismo patrón que UploadPublicShare/UploadToSharedDirectory).
// Sin sub-ruta: el destino es siempre la raíz de la carpeta del enlace
// (§38) -- no hay concepto de navegar dentro.
func (h *Handlers) UploadAnonymousUploadLink(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "El parámetro 'name' es obligatorio.")
		return
	}

	meta, err := h.Files.UploadViaAnonymousLink(r.Context(), token, name, r.Body, r.ContentLength)
	if err != nil {
		writeAnonymousUploadError(w, err)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventUpload, "", "file", meta.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"name": meta.Name, "size_bytes": meta.SizeBytes, "via": "anonymous_upload"})
	writeJSON(w, http.StatusCreated, toFileResponse(meta))
}

// writeAnonymousUploadError traduce los errores propios de la subida
// anónima; el resto (nombre inválido, ruta inválida...) se delega en
// writeFileError. Not-found/revocado/caducado comparten un único 404
// genérico a propósito -- ver GetAnonymousUploadLink.
func writeAnonymousUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrAnonymousUploadDisabled):
		writeError(w, http.StatusForbidden, "anonymous_upload_disabled", "La subida anónima está desactivada en esta instancia.")
	case errors.Is(err, storage.ErrAnonymousUploadNotFound),
		errors.Is(err, storage.ErrAnonymousUploadRevoked),
		errors.Is(err, storage.ErrAnonymousUploadExpired):
		writeError(w, http.StatusNotFound, "not_found", "Enlace no encontrado.")
	case errors.Is(err, storage.ErrAnonymousUploadForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "No eres el propietario de esa carpeta.")
	case errors.Is(err, storage.ErrAnonymousUploadTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "El archivo supera el límite de tamaño de este enlace.")
	case errors.Is(err, storage.ErrDestinationOccupied), errors.Is(err, storage.ErrNameOccupiedByTrash):
		writeNameUnavailable(w)
	case errors.Is(err, storage.ErrQuotaExceeded):
		// Es la cuota del propietario del enlace (ADR-036): un anónimo no debe
		// averiguar cuánto usa ni cuánto tiene.
		writeNoSpaceInFolder(w)
	default:
		writeFileError(w, err)
	}
}
