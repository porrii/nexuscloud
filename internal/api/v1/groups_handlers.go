package apiv1

import "net/http"

// ListGroups expone los grupos existentes (§22) para poder elegir un
// destino al crear un share de tipo grupo (§37). Cualquier usuario
// autenticado puede listarlos -- el nombre de un grupo no es información
// sensible, y son los propios administradores quienes los crean para que se
// use precisamente para esto.
func (h *Handlers) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.UserRepo.ListGroups(r.Context())
	if err != nil {
		h.Logger.Error("listando grupos", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los grupos.")
		return
	}
	out := make([]groupResponse, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroupResponse(g))
	}
	writeJSON(w, http.StatusOK, out)
}
