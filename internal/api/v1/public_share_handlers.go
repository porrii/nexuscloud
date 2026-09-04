package apiv1

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
)

// publicShareInfoResponse es la respuesta del probe de metadata (§37, §6 del
// plan de Sharing): si el enlace tiene contraseña y no llega una correcta en
// X-Share-Password, solo se revela el estado del ciclo de vida --
// nunca nombre/tamaño/tipo, que de lo contrario dejarían la contraseña sin
// ningún propósito real.
type publicShareInfoResponse struct {
	RequiresPassword  bool   `json:"requires_password"`
	PasswordIncorrect bool   `json:"password_incorrect,omitempty"`
	Revoked           bool   `json:"revoked,omitempty"`
	Expired           bool   `json:"expired,omitempty"`
	Exhausted         bool   `json:"exhausted,omitempty"`
	ResourceType      string `json:"resource_type,omitempty"`
	Name              string `json:"name,omitempty"`
	SizeBytes         int64  `json:"size_bytes,omitempty"`
	CanDownload       bool   `json:"can_download,omitempty"`
	CanUpload         bool   `json:"can_upload,omitempty"`
	Label             string `json:"label,omitempty"`
}

// GetPublicShare no exige contraseña para responder: el estado de ciclo de
// vida (revocado/expirado/agotado) siempre es visible para quien ya posee el
// token (el secreto real, 256 bits de entropía); la contraseña es un
// segundo factor que solo protege el CONTENIDO (nombre, tamaño, descarga).
func (h *Handlers) GetPublicShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	password := r.Header.Get("X-Share-Password")

	share, err := h.Files.ResolvePublicShare(r.Context(), token)
	if err != nil {
		writePublicShareError(w, err)
		return
	}

	out := publicShareInfoResponse{
		Revoked:   share.IsRevoked(),
		Expired:   share.IsExpired(time.Now().UTC()),
		Exhausted: share.IsExhausted(),
	}

	unlocked, err := h.Files.ResolvePublicShareForAccess(r.Context(), token, password)
	switch {
	case err == nil:
		out.CanDownload = unlocked.CanDownload
		out.CanUpload = unlocked.CanUpload
		out.Label = unlocked.Label
		if info, infoErr := h.Files.ResourceInfoForShare(r.Context(), unlocked); infoErr == nil {
			out.ResourceType = "file"
			if info.IsDirectory {
				out.ResourceType = "directory"
			}
			out.Name = info.Name
			out.SizeBytes = info.SizeBytes
		}
	case errors.Is(err, storage.ErrSharePasswordRequired):
		out.RequiresPassword = true
	case errors.Is(err, storage.ErrSharePasswordIncorrect):
		out.RequiresPassword = true
		out.PasswordIncorrect = true
	}
	// Cualquier otro error (revocado/expirado/agotado) ya quedó reflejado en
	// los booleanos de ciclo de vida de arriba; no hace falta repetirlo.

	writeJSON(w, http.StatusOK, out)
}

// DownloadPublicShare sirve el contenido, incrementando el contador de
// descargas de forma atómica (§37 "límite de descargas").
func (h *Handlers) DownloadPublicShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	password := r.Header.Get("X-Share-Password")
	subPath := r.URL.Query().Get("path")

	meta, rc, err := h.Files.DownloadViaPublicShare(r.Context(), token, password, subPath)
	if err != nil {
		writePublicShareError(w, err)
		return
	}
	defer rc.Close()

	h.AuditLog.Record(r.Context(), audit.EventDownload, "", "file", meta.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"via": "public_share"})

	w.Header().Set("Content-Type", meta.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.SizeBytes, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.Name))
	w.Header().Set("X-Content-SHA256", meta.SHA256)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := io.Copy(w, rc); err != nil {
		h.Logger.Warn("interrumpida la descarga vía enlace público", "file_id", meta.ID, "error", err)
	}
}

// BrowsePublicShare lista el contenido de un enlace de carpeta (o una
// subcarpeta suya vía ?path=, relativo a la raíz compartida).
func (h *Handlers) BrowsePublicShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	password := r.Header.Get("X-Share-Password")
	subPath := r.URL.Query().Get("path")

	result, err := h.Files.BrowsePublicShare(r.Context(), token, password, subPath)
	if err != nil {
		writePublicShareError(w, err)
		return
	}
	out := listResponse{
		Directories: make([]directoryResponse, 0, len(result.Directories)),
		Files:       make([]fileResponse, 0, len(result.Files)),
	}
	for _, d := range result.Directories {
		out.Directories = append(out.Directories, toDirectoryResponse(d))
	}
	for _, f := range result.Files {
		out.Files = append(out.Files, toFileResponse(f))
	}
	writeJSON(w, http.StatusOK, out)
}

// UploadPublicShare exige un enlace de carpeta con permiso de subida (§37
// "Subida"). El límite de tamaño del enlace, si tiene, se aplica dentro de
// storage.FileService.UploadViaPublicShare (corta la lectura con error en
// vez de truncar en silencio).
func (h *Handlers) UploadPublicShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	password := r.Header.Get("X-Share-Password")
	subPath := r.URL.Query().Get("path")
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "El parámetro 'name' es obligatorio.")
		return
	}

	meta, err := h.Files.UploadViaPublicShare(r.Context(), storage.PublicUploadInput{
		Token: token, Password: password, SubPath: subPath, Name: name, Content: r.Body,
	})
	if err != nil {
		writePublicShareError(w, err)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventUpload, "", "file", meta.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"name": meta.Name, "size_bytes": meta.SizeBytes, "via": "public_share"})
	writeJSON(w, http.StatusCreated, toFileResponse(meta))
}

func writePublicShareError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrPublicLinksDisabled):
		writeError(w, http.StatusForbidden, "public_links_disabled", "Los enlaces públicos están desactivados en esta instancia.")
	case errors.Is(err, storage.ErrShareNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Enlace no encontrado.")
	case errors.Is(err, storage.ErrShareRevoked):
		writeError(w, http.StatusGone, "share_revoked", "Este enlace ha sido revocado.")
	case errors.Is(err, storage.ErrShareExpired):
		writeError(w, http.StatusGone, "share_expired", "Este enlace ha expirado.")
	case errors.Is(err, storage.ErrShareExhausted):
		writeError(w, http.StatusGone, "share_exhausted", "Este enlace alcanzó su límite de descargas.")
	case errors.Is(err, storage.ErrSharePasswordRequired):
		writeError(w, http.StatusUnauthorized, "password_required", "Este enlace requiere contraseña.")
	case errors.Is(err, storage.ErrSharePasswordIncorrect):
		writeError(w, http.StatusUnauthorized, "invalid_password", "Contraseña incorrecta.")
	case errors.Is(err, storage.ErrShareDownloadNotAllowed):
		writeError(w, http.StatusForbidden, "download_not_allowed", "Este enlace no permite descarga.")
	case errors.Is(err, storage.ErrShareUploadNotAllowed):
		writeError(w, http.StatusForbidden, "upload_not_allowed", "Este enlace no permite subida.")
	case errors.Is(err, storage.ErrShareUploadTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "El archivo supera el límite de tamaño de este enlace.")
	case errors.Is(err, storage.ErrFileNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Archivo no encontrado.")
	case errors.Is(err, storage.ErrDirectoryNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Carpeta no encontrada.")
	case errors.Is(err, storage.ErrForbidden), errors.Is(err, storage.ErrInvalidPath):
		writeError(w, http.StatusForbidden, "forbidden", "Acceso no permitido.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la operación.")
	}
}
