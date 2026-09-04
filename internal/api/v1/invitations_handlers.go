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

type invitationResponse struct {
	ID        string     `json:"id"`
	RoleID    string     `json:"role_id,omitempty"`
	MaxUses   int        `json:"max_uses"`
	UseCount  int        `json:"use_count"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	Revoked   bool       `json:"revoked"`
}

func toInvitationResponse(i *auth.Invitation) invitationResponse {
	return invitationResponse{
		ID: i.ID, RoleID: i.RoleID, MaxUses: i.MaxUses, UseCount: i.UseCount,
		ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt, UsedAt: i.UsedAt, Revoked: i.RevokedAt != nil,
	}
}

type createInvitationRequest struct {
	Role     string `json:"role,omitempty"`
	MaxUses  int    `json:"max_uses,omitempty"`
	TTLHours int    `json:"ttl_hours,omitempty"`
}

type createInvitationResponse struct {
	Invitation invitationResponse `json:"invitation"`
	Token      string             `json:"token"` // solo se devuelve aquí, una vez (§78)
}

func (h *Handlers) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	var req createInvitationRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}

	var ttl time.Duration
	if req.TTLHours > 0 {
		ttl = time.Duration(req.TTLHours) * time.Hour
	}

	inv, token, err := h.Invitations.Create(r.Context(), auth.CreateInvitationInput{
		CreatedBy: actor.ID, RoleID: req.Role, MaxUses: req.MaxUses, TTL: ttl,
	})
	if err != nil {
		h.Logger.Error("creando invitación", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo crear la invitación.")
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventInvitationCreated, actor.ID, "invitation", inv.ID,
		security.ClientIP(r, h.TrustedProxies), nil)
	writeJSON(w, http.StatusCreated, createInvitationResponse{Invitation: toInvitationResponse(inv), Token: token})
}

func (h *Handlers) ListInvitations(w http.ResponseWriter, r *http.Request) {
	list, err := h.InvitationRepo.ListInvitations(r.Context())
	if err != nil {
		h.Logger.Error("listando invitaciones", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar las invitaciones.")
		return
	}
	out := make([]invitationResponse, 0, len(list))
	for _, inv := range list {
		out = append(out, toInvitationResponse(inv))
	}
	writeJSON(w, http.StatusOK, out)
}

type redeemInvitationRequest struct {
	Token       string `json:"token"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Password    string `json:"password"`
}

// RedeemInvitation es intencionadamente pública (sin RequireAuth): es el
// único mecanismo de auto-alta permitido, y solo funciona con un token de
// invitación válido (§21).
func (h *Handlers) RedeemInvitation(w http.ResponseWriter, r *http.Request) {
	var req redeemInvitationRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	if req.Token == "" || req.Username == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "invalid_request", "token, username y password (mínimo 8 caracteres) son obligatorios.")
		return
	}

	u, err := h.Invitations.Redeem(r.Context(), auth.RedeemInvitationInput{
		Token: req.Token, Username: req.Username, DisplayName: req.DisplayName,
		Email: req.Email, Password: req.Password,
	})
	if err != nil {
		writeRedeemError(w, err)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventUserCreated, u.ID, "user", u.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"via": "invitation"})
	writeJSON(w, http.StatusCreated, toUserResponse(u))
}

func writeRedeemError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvitationNotFound):
		writeError(w, http.StatusNotFound, "invitation_invalid", "Invitación no válida.")
	case errors.Is(err, auth.ErrInvitationExpired):
		writeError(w, http.StatusGone, "invitation_expired", "Esta invitación ha expirado.")
	case errors.Is(err, auth.ErrInvitationExhausted):
		writeError(w, http.StatusGone, "invitation_exhausted", "Esta invitación ya no tiene usos disponibles.")
	case errors.Is(err, auth.ErrInvitationRevoked):
		writeError(w, http.StatusGone, "invitation_revoked", "Esta invitación fue revocada.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar el registro.")
	}
}

func (h *Handlers) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.InvitationRepo.RevokeInvitation(r.Context(), id); err != nil {
		if errors.Is(err, auth.ErrInvitationNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Invitación no encontrada.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo revocar la invitación.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventInvitationRevoked, actor.ID, "invitation", id,
		security.ClientIP(r, h.TrustedProxies), nil)
	w.WriteHeader(http.StatusNoContent)
}
