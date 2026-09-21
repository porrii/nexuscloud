package apiv1

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
)

type createShareRequest struct {
	ResourceType       string     `json:"resource_type"`
	ResourceID         string     `json:"resource_id"`
	ShareType          string     `json:"share_type"`
	TargetUsername     string     `json:"target_username,omitempty"`
	TargetGroupID      string     `json:"target_group_id,omitempty"`
	Label              string     `json:"label,omitempty"`
	CanDownload        *bool      `json:"can_download,omitempty"`
	CanUpload          bool       `json:"can_upload,omitempty"`
	Password           string     `json:"password,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	MaxDownloads       *int       `json:"max_downloads,omitempty"`
	MaxUploadSizeBytes *int64     `json:"max_upload_size_bytes,omitempty"`
}

// CreateShare crea una compartición usuario→usuario, usuario→grupo o un
// enlace público (§37). Para un enlace, la respuesta incluye el token en
// claro una única vez -- igual que sesiones/invitaciones (§78) -- nunca se
// vuelve a mostrar después.
func (h *Handlers) CreateShare(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	var req createShareRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	if req.ResourceType != "file" && req.ResourceType != "directory" {
		writeError(w, http.StatusBadRequest, "invalid_request", "resource_type debe ser 'file' o 'directory'.")
		return
	}
	if req.ResourceID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "resource_id es obligatorio.")
		return
	}

	in := storage.CreateShareInput{
		ResourceIsDirectory: req.ResourceType == "directory",
		ResourceID:          req.ResourceID,
		Type:                storage.ShareType(req.ShareType),
		TargetGroupID:       req.TargetGroupID,
		Label:               req.Label,
		CanDownload:         true,
		CanUpload:           req.CanUpload,
		Password:            req.Password,
		ExpiresAt:           req.ExpiresAt,
		MaxDownloads:        req.MaxDownloads,
		MaxUploadSizeBytes:  req.MaxUploadSizeBytes,
	}
	if req.CanDownload != nil {
		in.CanDownload = *req.CanDownload
	}
	if req.TargetUsername != "" {
		target, err := h.UserRepo.GetUserByUsername(r.Context(), req.TargetUsername)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "El usuario destino no existe.")
			return
		}
		in.TargetUserID = target.ID
	}

	share, token, err := h.Files.CreateShare(r.Context(), u.ID, in)
	if err != nil {
		writeShareError(w, err)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventShareCreate, u.ID, "share", share.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"share_type": string(share.Type)})

	out := toShareResponse(share, h.shareResponseExtra(r.Context(), share))
	out.Token = token
	writeJSON(w, http.StatusCreated, out)
}

// ListShares devuelve "compartido por mí" (?direction=by-me, por defecto) o
// "compartido conmigo" (?direction=with-me), §143.
func (h *Handlers) ListShares(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())

	var shares []*storage.Share
	var err error
	if r.URL.Query().Get("direction") == "with-me" {
		shares, err = h.Files.ListSharesWithMe(r.Context(), u.ID)
	} else {
		shares, err = h.Files.ListSharesByMe(r.Context(), u.ID)
	}
	if err != nil {
		h.Logger.Error("listando shares", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar las comparticiones.")
		return
	}

	out := make([]shareResponse, 0, len(shares))
	for _, s := range shares {
		out = append(out, toShareResponse(s, h.shareResponseExtra(r.Context(), s)))
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeShare exige ser quien creó el share (§198); es un soft-revoke, ver
// storage.FileService.RevokeShare.
func (h *Handlers) RevokeShare(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.Files.RevokeShare(r.Context(), u.ID, id); err != nil {
		writeShareError(w, err)
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventShareRevoke, u.ID, "share", id, security.ClientIP(r, h.TrustedProxies), nil)
	w.WriteHeader(http.StatusNoContent)
}

// shareResponseExtra resuelve los nombres que toShareResponse (un mapper DTO
// puro) no puede completar por sí sola: nombre del recurso compartido,
// username del destinatario y nombre del grupo destino. Los fallos de
// resolución se ignoran en silencio (el campo simplemente queda vacío en la
// respuesta) en vez de romper el listado completo por un recurso huérfano.
func (h *Handlers) shareResponseExtra(ctx context.Context, s *storage.Share) shareResponseExtra {
	var extra shareResponseExtra
	if info, err := h.Files.ResourceInfoForShare(ctx, s); err == nil {
		extra.ResourceName = info.Name
	}
	if s.TargetUserID != "" {
		if u, err := h.UserRepo.GetUserByID(ctx, s.TargetUserID); err == nil {
			extra.TargetUsername = u.Username
		}
	}
	if s.TargetGroupID != "" {
		if groups, err := h.UserRepo.ListGroups(ctx); err == nil {
			for _, g := range groups {
				if g.ID == s.TargetGroupID {
					extra.TargetGroupName = g.Name
					break
				}
			}
		}
	}
	return extra
}

// sharedListingResponse añade al listado de una carpeta compartida si quien la
// mira puede subir a ella y con qué límite por archivo (§37, ADR-035), para que
// la interfaz sepa si ofrecer el botón de subir y qué tamaño anunciar. Son
// campos nuevos y aditivos: los clientes que no los conocen los ignoran.
// MaxUploadSizeBytes se omite si no hay límite (o si no puede subir).
type sharedListingResponse struct {
	listResponse
	CanUpload          bool   `json:"can_upload"`
	MaxUploadSizeBytes *int64 `json:"max_upload_size_bytes,omitempty"`
}

// ListSharedDirectory navega una carpeta a la que el usuario accede vía un
// share (directo o de un ancestro), no por propiedad -- ver
// storage.FileService.ListSharedDirectory.
func (h *Handlers) ListSharedDirectory(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	result, err := h.Files.ListSharedDirectory(r.Context(), u.ID, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	upload, err := h.Files.SharedUploadPermission(r.Context(), u.ID, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	out := sharedListingResponse{
		listResponse: listResponse{
			Directories: make([]directoryResponse, 0, len(result.Directories)),
			Files:       make([]fileResponse, 0, len(result.Files)),
		},
		CanUpload:          upload.Allowed,
		MaxUploadSizeBytes: upload.MaxSizeBytes,
	}
	for _, d := range result.Directories {
		out.Directories = append(out.Directories, toDirectoryResponse(d))
	}
	for _, f := range result.Files {
		out.Files = append(out.Files, toFileResponse(f))
	}
	writeJSON(w, http.StatusOK, out)
}

// UploadToSharedDirectory recibe el contenido como cuerpo crudo, en streaming
// y sin cargarlo entero en memoria, igual que UploadFile (§136-137). El destino
// sale SOLO del ID de la carpeta de la URL: la autorización se re-deriva de la
// base de datos en cada petición, sin ninguna subruta que el cliente pueda
// manipular. El archivo queda en el árbol del propietario de la carpeta; quién
// lo subió queda en la auditoría (§37, ADR-035).
func (h *Handlers) UploadToSharedDirectory(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "El parámetro 'name' es obligatorio.")
		return
	}

	meta, shareID, err := h.Files.UploadToSharedDirectory(r.Context(), storage.SharedUploadInput{
		RequesterID: u.ID, DirectoryID: id, Name: name, Content: r.Body,
	})
	if err != nil {
		writeSharedUploadError(w, err)
		return
	}

	metadata := map[string]any{"name": meta.Name, "size_bytes": meta.SizeBytes, "via": "shared_directory", "owner_id": meta.OwnerID}
	if shareID != "" {
		metadata["share_id"] = shareID
	}
	h.AuditLog.Record(r.Context(), audit.EventUpload, u.ID, "file", meta.ID, security.ClientIP(r, h.TrustedProxies), metadata)
	writeJSON(w, http.StatusCreated, toFileResponse(meta))
}

// writeNameUnavailable responde 409 cuando el nombre de una subida a una carpeta
// ajena (compartida, o por enlace público) ya está ocupado. No distingue si lo
// ocupa un archivo activo o algo de la papelera del propietario: quien sube no
// debe averiguar qué ha borrado el propietario, ni el mensaje le invita a
// «restaurarlo».
func writeNameUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "destination_occupied", "Ya hay un archivo con ese nombre en esta carpeta; no se sobrescribe.")
}

// writeSharedUploadError traduce los errores propios de subir a una carpeta
// compartida (sin permiso de subida, nombre ocupado, archivo por encima del
// límite, compartición desactivada) y delega el resto en los de cualquier
// operación de archivos.
func writeSharedUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrDestinationOccupied), errors.Is(err, storage.ErrNameOccupiedByTrash):
		writeNameUnavailable(w)
	case errors.Is(err, storage.ErrShareUploadNotAllowed):
		writeError(w, http.StatusForbidden, "upload_not_allowed", "Esta carpeta está compartida contigo sin permiso de subida.")
	case errors.Is(err, storage.ErrShareUploadTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "El archivo supera el límite de tamaño de esta carpeta compartida.")
	case errors.Is(err, storage.ErrSharingDisabled):
		writeError(w, http.StatusForbidden, "sharing_disabled", "La compartición está desactivada en esta instancia.")
	default:
		writeFileError(w, err)
	}
}

func writeShareError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "No tienes permiso sobre este elemento.")
	case errors.Is(err, storage.ErrShareNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Share no encontrado.")
	case errors.Is(err, storage.ErrFileNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Archivo no encontrado.")
	case errors.Is(err, storage.ErrDirectoryNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Carpeta no encontrada.")
	case errors.Is(err, storage.ErrSharingDisabled):
		writeError(w, http.StatusForbidden, "sharing_disabled", "La compartición está desactivada en esta instancia.")
	case errors.Is(err, storage.ErrPublicLinksDisabled):
		writeError(w, http.StatusForbidden, "public_links_disabled", "Los enlaces públicos están desactivados en esta instancia.")
	case errors.Is(err, storage.ErrInvalidShare):
		writeError(w, http.StatusBadRequest, "invalid_request", "Datos de compartición inválidos.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la operación.")
	}
}
