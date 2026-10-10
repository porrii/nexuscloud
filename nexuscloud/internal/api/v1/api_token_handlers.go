package apiv1

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/security"
)

// apiTokenResponse nunca expone TokenHash (§172): el token en claro solo
// existe en la respuesta de creación, una única vez (§78), igual que las
// sesiones, las invitaciones y los tokens WebDAV.
type apiTokenResponse struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func toAPITokenResponse(t *auth.APIToken) apiTokenResponse {
	return apiTokenResponse{ID: t.ID, Label: t.Label, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt}
}

// apiTokenCreatedResponse añade el secreto (una única vez).
type apiTokenCreatedResponse struct {
	apiTokenResponse
	Token string `json:"token"`
}

// createAPITokenRequest.ExpiresAt es una fecha absoluta en RFC3339 (mismo
// criterio que shares.expires_at), no una duración: quien llama (web o CLI)
// calcula "ahora + N días" antes de mandarla. Ausente/null = nunca expira.
type createAPITokenRequest struct {
	Label     string     `json:"label"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// CreateAPIToken genera un token de acceso a la API REST completa para el
// usuario autenticado (§78, ADR-037), de alcance todo-o-nada: actúa
// exactamente como él. El cuerpo es opcional; sin label se usa uno por
// defecto, sin expires_at el token no expira.
func (h *Handlers) CreateAPIToken(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	var req createAPITokenRequest
	_ = readJSON(r, &req) // cuerpo vacío = sin label ni expiración, no es un error

	tok, plain, err := h.APITokens.Create(r.Context(), u.ID, req.Label, req.ExpiresAt)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidAPITokenLabel) {
			writeError(w, http.StatusBadRequest, "invalid_request", "El nombre del token es demasiado largo (máximo 100 caracteres).")
			return
		}
		h.Logger.Error("creando token de API", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo crear el token.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventAPITokenCreated, u.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies), map[string]any{"label": tok.Label})
	writeJSON(w, http.StatusCreated, apiTokenCreatedResponse{
		apiTokenResponse: toAPITokenResponse(tok),
		Token:            plain,
	})
}

// ListAPITokens devuelve los tokens del propio usuario, para que los
// gestione desde su cuenta.
func (h *Handlers) ListAPITokens(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	tokens, err := h.APITokens.List(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("listando tokens de API", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los tokens.")
		return
	}
	out := make([]apiTokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toAPITokenResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeAPIToken exige, vía APITokenRepository.RevokeToken, que el token
// pertenezca al usuario autenticado (§198 IDOR).
func (h *Handlers) RevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.APITokens.Revoke(r.Context(), id, u.ID); err != nil {
		if errors.Is(err, auth.ErrAPITokenNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Token no encontrado.")
			return
		}
		h.Logger.Error("revocando token de API", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo revocar el token.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventAPITokenRevoked, u.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies), map[string]any{"token_id": id})
	w.WriteHeader(http.StatusNoContent)
}
