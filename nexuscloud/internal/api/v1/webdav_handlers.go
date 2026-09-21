package apiv1

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/webdav"
)

// webdavTokenResponse nunca expone TokenHash (§172): el token en claro solo
// existe en la respuesta de creación, una única vez (§78), igual que las
// sesiones y las invitaciones.
type webdavTokenResponse struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func toWebDAVTokenResponse(t *webdav.Token) webdavTokenResponse {
	return webdavTokenResponse{ID: t.ID, Label: t.Label, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt}
}

// webdavTokenCreatedResponse añade el secreto (una única vez) y la ruta de
// montaje: con el token y el usuario, el cliente ya tiene todo lo necesario
// para configurar la conexión sin tener que adivinar la URL.
type webdavTokenCreatedResponse struct {
	webdavTokenResponse
	Token      string `json:"token"`
	WebDAVPath string `json:"webdav_path"`
}

type createWebDAVTokenRequest struct {
	Label string `json:"label"`
}

// CreateWebDAVToken genera un token de acceso WebDAV para el usuario
// autenticado (§43, ADR-034): la contraseña que su cliente WebDAV usará por
// HTTP Basic. El cuerpo es opcional; sin label se llama "WebDAV".
func (h *Handlers) CreateWebDAVToken(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	var req createWebDAVTokenRequest
	_ = readJSON(r, &req) // cuerpo vacío = sin label, no es un error

	tok, plain, err := h.WebDAVTokens.Create(r.Context(), u.ID, req.Label)
	if err != nil {
		if errors.Is(err, webdav.ErrInvalidLabel) {
			writeError(w, http.StatusBadRequest, "invalid_request", "El nombre del token es demasiado largo (máximo 100 caracteres).")
			return
		}
		h.Logger.Error("creando token WebDAV", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo crear el token.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventWebDAVTokenCreated, u.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies), map[string]any{"label": tok.Label})
	writeJSON(w, http.StatusCreated, webdavTokenCreatedResponse{
		webdavTokenResponse: toWebDAVTokenResponse(tok),
		Token:               plain,
		WebDAVPath:          h.WebDAVPath,
	})
}

// ListWebDAVTokens devuelve los tokens del propio usuario, para que los
// gestione desde su cuenta.
func (h *Handlers) ListWebDAVTokens(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	tokens, err := h.WebDAVTokens.List(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("listando tokens WebDAV", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los tokens.")
		return
	}
	out := make([]webdavTokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toWebDAVTokenResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeWebDAVToken exige, vía TokenRepository.RevokeToken, que el token
// pertenezca al usuario autenticado (§198 IDOR).
func (h *Handlers) RevokeWebDAVToken(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.WebDAVTokens.Revoke(r.Context(), id, u.ID); err != nil {
		if errors.Is(err, webdav.ErrTokenNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Token no encontrado.")
			return
		}
		h.Logger.Error("revocando token WebDAV", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo revocar el token.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventWebDAVTokenRevoked, u.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies), map[string]any{"token_id": id})
	w.WriteHeader(http.StatusNoContent)
}
