package apiv1

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
)

type listResponse struct {
	Directories []directoryResponse `json:"directories"`
	Files       []fileResponse      `json:"files"`
}

// ListFiles devuelve tanto subcarpetas como archivos de una misma ruta
// lógica, como un explorador de archivos real (§13, §44).
func (h *Handlers) ListFiles(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	parentPath := r.URL.Query().Get("path")

	result, err := h.Files.List(r.Context(), u.ID, parentPath)
	if err != nil {
		h.Logger.Error("listando archivos", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los archivos.")
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

// UploadFile recibe el contenido como cuerpo crudo de la petición, en
// streaming y sin cargarlo entero en memoria (§136-137). parent_path/name
// van en la query string para no necesitar parseo multipart.
func (h *Handlers) UploadFile(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	name := r.URL.Query().Get("name")
	parentPath := r.URL.Query().Get("path")
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "El parámetro 'name' es obligatorio.")
		return
	}

	meta, err := h.Files.Upload(r.Context(), storage.UploadInput{
		OwnerID: u.ID, ParentPath: parentPath, Name: name, Content: r.Body,
	})
	if err != nil {
		writeFileError(w, err)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventUpload, u.ID, "file", meta.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"name": meta.Name, "size_bytes": meta.SizeBytes})
	writeJSON(w, http.StatusCreated, toFileResponse(meta))
}

// DownloadFile transmite el contenido en streaming (§137). El soporte de
// Range/reanudación de descargas queda para una fase posterior (§41): no es
// un cambio de contrato romper este endpoint al añadirlo.
func (h *Handlers) DownloadFile(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	meta, rc, err := h.Files.Download(r.Context(), u.ID, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer rc.Close()

	h.AuditLog.Record(r.Context(), audit.EventDownload, u.ID, "file", meta.ID, security.ClientIP(r, h.TrustedProxies), nil)

	// Content-Disposition/Content-Type explícitos, nunca servidos "tal
	// cual" desde el filesystem (§192-194).
	w.Header().Set("Content-Type", meta.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.SizeBytes, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.Name))
	w.Header().Set("X-Content-SHA256", meta.SHA256)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := io.Copy(w, rc); err != nil {
		h.Logger.Warn("interrumpida la descarga de archivo", "file_id", meta.ID, "error", err)
	}
}

// DeleteFile mueve el archivo a la papelera por defecto (§16); con
// ?permanent=true (o si trash.enabled=false en la configuración) borra
// directamente y para siempre.
func (h *Handlers) DeleteFile(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	var err error
	if r.URL.Query().Get("permanent") == "true" {
		err = h.Files.PermanentlyDeleteFile(r.Context(), u.ID, id)
	} else {
		err = h.Files.Delete(r.Context(), u.ID, id)
	}
	if err != nil {
		writeFileError(w, err)
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventDelete, u.ID, "file", id, security.ClientIP(r, h.TrustedProxies), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) RestoreFile(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.Files.RestoreFile(r.Context(), u.ID, id); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type mkdirRequest struct {
	ParentPath string `json:"parent_path"`
	Name       string `json:"name"`
}

func (h *Handlers) Mkdir(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	var req mkdirRequest
	if err := readJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name es obligatorio.")
		return
	}
	dir, err := h.Files.Mkdir(r.Context(), u.ID, req.ParentPath, req.Name)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDirectoryResponse(dir))
}

// DeleteDirectory solo borra carpetas vacías -- de contenido activo --
// (§184-185): si contiene algo, devuelve 409 en vez de borrar en cascada
// sin confirmación explícita. Mueve a la papelera por defecto; con
// ?permanent=true borra para siempre.
func (h *Handlers) DeleteDirectory(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	var err error
	if r.URL.Query().Get("permanent") == "true" {
		err = h.Files.PermanentlyDeleteDirectory(r.Context(), u.ID, id)
	} else {
		err = h.Files.DeleteDirectory(r.Context(), u.ID, id)
	}
	if err != nil {
		writeFileError(w, err)
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventDelete, u.ID, "directory", id, security.ClientIP(r, h.TrustedProxies), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) RestoreDirectory(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.Files.RestoreDirectory(r.Context(), u.ID, id); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeFileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "No tienes permiso sobre este elemento.")
	case errors.Is(err, storage.ErrFileNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Archivo no encontrado.")
	case errors.Is(err, storage.ErrDirectoryNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Carpeta no encontrada.")
	case errors.Is(err, storage.ErrDirectoryNotEmpty):
		writeError(w, http.StatusConflict, "not_empty", "La carpeta no está vacía.")
	case errors.Is(err, storage.ErrNameOccupiedByTrash):
		writeError(w, http.StatusConflict, "name_occupied_by_trash", err.Error())
	case errors.Is(err, storage.ErrInvalidName), errors.Is(err, storage.ErrInvalidPath), errors.Is(err, storage.ErrPathEscapesRoot):
		writeError(w, http.StatusBadRequest, "invalid_request", "Nombre o ruta inválidos.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la operación.")
	}
}
