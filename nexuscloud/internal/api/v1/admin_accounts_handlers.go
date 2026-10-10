package apiv1

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/users"
)

// Administración de cuentas de OTROS usuarios (ADR-042, B1). Todo va bajo
// RequireAdmin y RequireWritable; las acciones destructivas, además, bajo
// RequireRecentReauth (ver NewRouter). Cada handler comprueba primero que
// el actor puede gestionar al objetivo (un administrator no toca a un
// super_admin): esa regla vive en el dominio, no aquí.

// manageTarget resuelve {id}, comprueba que existe y que el actor puede
// gestionarlo. Si falla, ya ha escrito la respuesta.
func (h *Handlers) manageTarget(w http.ResponseWriter, r *http.Request) (actor *users.User, targetID string, ok bool) {
	actor, _ = UserFromContext(r.Context())
	targetID = chi.URLParam(r, "id")
	if _, err := h.UserRepo.GetUserByID(r.Context(), targetID); err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return nil, "", false
	}
	if err := h.UserSvc.EnsureCanManage(r.Context(), actor.ID, targetID); err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return nil, "", false
	}
	return actor, targetID, true
}

func (h *Handlers) recordAdmin(r *http.Request, event, actorID, targetType, targetID string, meta map[string]any) {
	h.AuditLog.Record(r.Context(), event, actorID, targetType, targetID, security.ClientIP(r, h.TrustedProxies), meta)
}

// --- Rol --------------------------------------------------------------------

type setRoleRequest struct {
	Role string `json:"role"`
}

// SetUserRole (PUT /users/{id}/role) deja a la cuenta con un único rol
// (Decisión 8). Pasar hacia o desde administrator/super_admin exige
// reautenticación; el resto de cambios, no.
func (h *Handlers) SetUserRole(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	var req setRoleRequest
	if err := readJSON(r, &req); err != nil || req.Role == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "role es obligatorio.")
		return
	}
	if err := users.ValidateRole(req.Role); err != nil {
		h.writeAccountAdminError(w, err, "")
		return
	}
	if _, err := h.UserRepo.GetUserByID(r.Context(), id); err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}
	current, err := h.UserSvc.PrimaryRole(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}
	if users.RoleChangeNeedsReauth(current, req.Role) && !h.checkRecentReauth(w, r) {
		return
	}

	previous, err := h.UserSvc.SetRole(r.Context(), actor.ID, id, req.Role)
	if err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}
	if previous != req.Role {
		h.recordAdmin(r, audit.EventRoleChanged, actor.ID, "user", id, map[string]any{"before": previous, "after": req.Role})
	}
	u, err := h.UserRepo.GetUserByID(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}
	resp := toUserResponse(u)
	resp.Role = req.Role
	writeJSON(w, http.StatusOK, resp)
}

// --- Contraseña y segundo factor -------------------------------------------

type resetPasswordRequest struct {
	Password string `json:"password"`
}

// ResetUserPassword (POST /users/{id}/password, con reautenticación) fija
// una contraseña nueva y revoca todas las sesiones y tokens de la cuenta
// (Decisión 9). La contraseña nueva nunca se registra ni se devuelve.
func (h *Handlers) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	var req resetPasswordRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	revoked, err := h.AccountAdmin.ResetPassword(r.Context(), actor.ID, id, req.Password)
	if err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}
	h.recordAdmin(r, audit.EventPasswordReset, actor.ID, "user", id,
		map[string]any{"api_tokens_revoked": revoked.APITokens, "webdav_tokens_revoked": revoked.WebDAVTokens})
	writeJSON(w, http.StatusOK, map[string]any{"revoked": revoked})
}

// RemoveUserTOTP (DELETE /users/{id}/totp, con reautenticación) quita el
// 2FA por TOTP y cierra las sesiones de la cuenta (Decisión 10).
func (h *Handlers) RemoveUserTOTP(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := h.AccountAdmin.RemoveTOTP(r.Context(), actor.ID, id); err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}
	h.recordAdmin(r, audit.EventTOTPRemoved, actor.ID, "user", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ListUserPasskeys (GET /users/{id}/passkeys): solo metadatos.
func (h *Handlers) ListUserPasskeys(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	creds, err := h.AccountAdmin.ListPasskeys(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}
	out := make([]webauthnCredentialResponse, 0, len(creds))
	for _, c := range creds {
		out = append(out, toWebAuthnCredentialResponse(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// RemoveUserPasskey (DELETE /users/{id}/passkeys/{credentialId}, con
// reautenticación) borra un passkey de otra cuenta y cierra sus sesiones.
func (h *Handlers) RemoveUserPasskey(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	credID := chi.URLParam(r, "credentialId")
	if err := h.AccountAdmin.RemovePasskey(r.Context(), actor.ID, id, credID); err != nil {
		h.writeAccountAdminError(w, err, "Passkey no encontrado.")
		return
	}
	h.recordAdmin(r, audit.EventWebAuthnCredentialRevoked, actor.ID, "user", id,
		map[string]any{"credential_id": credID, "by_admin": true})
	w.WriteHeader(http.StatusNoContent)
}

// --- Sesiones y tokens de otros (Decisión 12) ------------------------------

func (h *Handlers) ListUserSessions(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	sessions, err := h.SessionRepo.ListSessionsForUser(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "")
		return
	}
	out := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		if s.RevokedAt != nil {
			continue
		}
		out = append(out, toSessionResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) RevokeUserSession(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	sid := chi.URLParam(r, "sessionId")
	if err := h.SessionRepo.RevokeSession(r.Context(), sid, id); err != nil {
		h.writeAccountAdminError(w, err, "Sesión no encontrada.")
		return
	}
	h.recordAdmin(r, audit.EventSessionsRevoked, actor.ID, "user", id, map[string]any{"session_id": sid})
	w.WriteHeader(http.StatusNoContent)
}

// RevokeAllUserSessions (DELETE /users/{id}/sessions) cierra todas las
// sesiones de la cuenta; los tokens siguen (para eso está restablecer la
// contraseña o revocarlos uno a uno).
func (h *Handlers) RevokeAllUserSessions(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	if err := h.SessionRepo.RevokeAllForUser(r.Context(), id); err != nil {
		h.writeAccountAdminError(w, err, "")
		return
	}
	h.recordAdmin(r, audit.EventSessionsRevoked, actor.ID, "user", id, map[string]any{"all": true})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ListUserAPITokens(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	tokens, err := h.APITokens.List(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "")
		return
	}
	out := make([]apiTokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toAPITokenResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) RevokeUserAPIToken(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	tid := chi.URLParam(r, "tokenId")
	if err := h.APITokens.Revoke(r.Context(), tid, id); err != nil {
		h.writeAccountAdminError(w, err, "Token no encontrado.")
		return
	}
	h.recordAdmin(r, audit.EventAPITokenRevoked, actor.ID, "user", id, map[string]any{"token_id": tid, "by_admin": true})
	w.WriteHeader(http.StatusNoContent)
}

// Los tokens WebDAV se listan y revocan aunque WebDAV esté desactivado hoy:
// si se reactivara, volverían a valer (por eso se usa el repositorio, que
// existe siempre, y no h.WebDAVTokens).
func (h *Handlers) ListUserWebDAVTokens(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	tokens, err := h.WebDAVTokenRepo.ListTokensForUser(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "")
		return
	}
	out := make([]webdavTokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toWebDAVTokenResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) RevokeUserWebDAVToken(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	tid := chi.URLParam(r, "tokenId")
	if err := h.WebDAVTokenRepo.RevokeToken(r.Context(), tid, id); err != nil {
		h.writeAccountAdminError(w, err, "Token no encontrado.")
		return
	}
	h.recordAdmin(r, audit.EventWebDAVTokenRevoked, actor.ID, "user", id, map[string]any{"token_id": tid, "by_admin": true})
	w.WriteHeader(http.StatusNoContent)
}

// --- Comparticiones de otros (Decisión 12) ----------------------------------

// ListUserShares (GET /users/{id}/shares): lo que la cuenta tiene
// compartido y vigente, con el mismo formato que «compartido por mí».
func (h *Handlers) ListUserShares(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	shares, err := h.Files.ListSharesByMe(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "")
		return
	}
	out := make([]shareResponse, 0, len(shares))
	for _, s := range shares {
		out = append(out, toShareResponse(s, h.shareResponseExtra(r.Context(), s)))
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeUserShare reutiliza la comprobación de propiedad de RevokeShare con
// el dueño real como solicitante: solo revoca shares de ESA cuenta.
func (h *Handlers) RevokeUserShare(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	sid := chi.URLParam(r, "shareId")
	if err := h.Files.RevokeShare(r.Context(), id, sid); err != nil {
		writeShareError(w, err)
		return
	}
	h.recordAdmin(r, audit.EventShareRevoke, actor.ID, "share", sid, map[string]any{"owner_id": id, "by_admin": true})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ListUserAnonymousUploads(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	links, err := h.Files.ListAnonymousUploadLinks(r.Context(), id)
	if err != nil {
		writeAnonymousUploadError(w, err)
		return
	}
	out := make([]anonymousUploadResponse, 0, len(links))
	for _, link := range links {
		name, _ := h.Files.DirectoryNameForAnonymousUpload(r.Context(), link)
		out = append(out, toAnonymousUploadResponse(link, name))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) RevokeUserAnonymousUpload(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := h.manageTarget(w, r)
	if !ok {
		return
	}
	lid := chi.URLParam(r, "linkId")
	if err := h.Files.RevokeAnonymousUploadLink(r.Context(), id, lid); err != nil {
		writeAnonymousUploadError(w, err)
		return
	}
	h.recordAdmin(r, audit.EventAnonymousUploadLinkRevoked, actor.ID, "anonymous_upload", lid,
		map[string]any{"owner_id": id, "by_admin": true})
	w.WriteHeader(http.StatusNoContent)
}
