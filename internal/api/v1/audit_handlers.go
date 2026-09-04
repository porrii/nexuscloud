package apiv1

import (
	"net/http"
	"strconv"
)

// ListAuditEvents es admin-only (montado bajo RequireAdmin en router.go).
func (h *Handlers) ListAuditEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	events, err := h.AuditRepo.ListEvents(r.Context(), limit, offset)
	if err != nil {
		h.Logger.Error("listando eventos de auditoría", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los eventos de auditoría.")
		return
	}
	writeJSON(w, http.StatusOK, events)
}
