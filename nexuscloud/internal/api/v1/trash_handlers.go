package apiv1

import "net/http"

// ListTrash devuelve la papelera del usuario autenticado: vista plana
// (sin jerarquía de carpetas), igual que la papelera de Windows/Google
// Drive (§16).
func (h *Handlers) ListTrash(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())

	result, err := h.Files.ListTrash(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("listando papelera", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo listar la papelera.")
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
