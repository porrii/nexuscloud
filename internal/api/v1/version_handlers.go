package apiv1

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/storage"
)

// ListFileVersions devuelve el historial de un archivo, más reciente
// primero (§15).
func (h *Handlers) ListFileVersions(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	versions, err := h.Files.ListVersions(r.Context(), u.ID, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	out := make([]versionResponse, 0, len(versions))
	for _, v := range versions {
		out = append(out, toVersionResponse(v))
	}
	writeJSON(w, http.StatusOK, out)
}

func parseVersionNum(r *http.Request) (int, error) {
	return strconv.Atoi(chi.URLParam(r, "versionNum"))
}

// DownloadFileVersion descarga el contenido de una versión concreta, no la
// actual.
func (h *Handlers) DownloadFileVersion(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	versionNum, err := parseVersionNum(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Número de versión inválido.")
		return
	}

	v, rc, err := h.Files.DownloadVersion(r.Context(), u.ID, id, versionNum)
	if err != nil {
		writeFileError(w, err)
		return
	}

	name := fmt.Sprintf("v%d", v.VersionNum)
	if err := serveFileContent(w, r, name, v.MimeType, v.SHA256, v.SizeBytes, rc); err != nil {
		h.Logger.Warn("interrumpida la descarga de una versión", "file_id", id, "version", versionNum, "error", err)
	}
}

// RestoreFileVersion hace que una versión antigua vuelva a ser el contenido
// vigente; la versión actual pasa a su vez al historial (§15).
func (h *Handlers) RestoreFileVersion(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	versionNum, err := parseVersionNum(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Número de versión inválido.")
		return
	}

	meta, err := h.Files.RestoreVersion(r.Context(), u.ID, id, versionNum)
	if err != nil {
		if errors.Is(err, storage.ErrVersionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Versión no encontrada.")
			return
		}
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toFileResponse(meta))
}
