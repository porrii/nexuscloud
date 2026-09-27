package apiv1

import (
	"net/http"
	"strconv"
	"time"

	"github.com/porrii/nexuscloud/internal/audit"
)

// recentActivityEventTypes (§88, ADR-038) son los tipos de evento que
// cuentan como "tu actividad sobre tus archivos" -- el ejemplo del propio
// spec ("Ivan subió: documento.pdf") es justo esta clase de cosas. Quedan
// fuera a propósito login/sesión, cuotas, tokens, WebAuthn, administración:
// no son la actividad que describe §88. Decisión de contenido, no de
// arquitectura -- se puede ajustar sin tocar esquema ni el repositorio.
var recentActivityEventTypes = []string{
	audit.EventUpload,
	audit.EventDownload,
	audit.EventDelete,
	audit.EventMove,
	audit.EventShareCreate,
	audit.EventShareRevoke,
}

const defaultActivityLimit = 20

type activityEventResponse struct {
	ID         string         `json:"id"`
	OccurredAt time.Time      `json:"occurred_at"`
	EventType  string         `json:"event_type"`
	TargetType string         `json:"target_type,omitempty"`
	TargetID   string         `json:"target_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// ListActivity da el feed de "Recientes" (§88, ADR-038) del usuario
// autenticado: es un widget de dashboard, no el listado administrativo
// completo (GET /audit) -- limit por defecto bajo, tope 100. El texto
// humano ("Ivan subió documento.pdf, hace 5 minutos") lo construye el
// CLIENTE a partir de event_type + metadata, no el servidor (mismo criterio
// que formatDate/formatBytes, ya del lado del cliente en toda la web).
func (h *Handlers) ListActivity(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())

	limit := defaultActivityLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}

	events, err := h.AuditRepo.ListEventsForActor(r.Context(), u.ID, recentActivityEventTypes, limit, 0)
	if err != nil {
		h.Logger.Error("listando actividad reciente", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo listar la actividad reciente.")
		return
	}
	out := make([]activityEventResponse, 0, len(events))
	for _, e := range events {
		out = append(out, activityEventResponse{
			ID: e.ID, OccurredAt: e.OccurredAt, EventType: e.EventType,
			TargetType: e.TargetType, TargetID: e.TargetID, Metadata: e.Metadata,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
