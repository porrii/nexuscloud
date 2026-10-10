package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/accountadmin"
	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/users"
	"github.com/porrii/nexuscloud/internal/version"
	"github.com/porrii/nexuscloud/internal/webdav"
)

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	isAdmin, err := h.UserSvc.IsAdmin(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("comprobando privilegios de administrador", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la operación.")
		return
	}
	resp, err := h.withRole(r.Context(), u)
	if err != nil {
		h.Logger.Error("consultando el rol", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la operación.")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		userResponse:  resp,
		IsAdmin:       isAdmin,
		ServerVersion: version.Version,
		Capabilities: []string{
			CapabilityReadOnlyRole, CapabilityDisabledOwnerLinks,
			CapabilityAccountAdmin, CapabilityReauthentication,
		},
	})
}

// withRole es toUserResponse más el rol de la cuenta (ADR-042 Decisión 8).
func (h *Handlers) withRole(ctx context.Context, u *users.User) (userResponse, error) {
	out := toUserResponse(u)
	role, err := h.UserSvc.PrimaryRole(ctx, u.ID)
	if err != nil {
		return out, err
	}
	out.Role = role
	return out, nil
}

func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := h.UserRepo.ListUsers(r.Context())
	if err != nil {
		h.Logger.Error("listando usuarios", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los usuarios.")
		return
	}
	out := make([]userResponse, 0, len(list))
	for _, u := range list {
		resp, err := h.withRole(r.Context(), u)
		if err != nil {
			h.Logger.Error("consultando el rol", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los usuarios.")
			return
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

// writeAccountAdminError traduce los errores de dominio de la administración
// de cuentas (ADR-042) a respuestas HTTP. Lo que no reconoce va al log y
// sale como error interno genérico.
func (h *Handlers) writeAccountAdminError(w http.ResponseWriter, err error, notFoundMsg string) {
	var pubErr *users.PublicationsError
	switch {
	case errors.As(err, &pubErr):
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{
			"code":    "has_publications",
			"message": "Antes de pasar la cuenta a solo lectura, revoca sus enlaces públicos, sus enlaces de subida y las comparticiones con permiso de subida.",
			"counts": map[string]int{
				"public_links":      pubErr.Counts.PublicLinks,
				"anonymous_uploads": pubErr.Counts.AnonymousUploads,
				"upload_shares":     pubErr.Counts.UploadShares,
			},
		}})
	case errors.Is(err, users.ErrNotFound), errors.Is(err, auth.ErrSessionNotFound),
		errors.Is(err, auth.ErrAPITokenNotFound), errors.Is(err, webdav.ErrTokenNotFound),
		errors.Is(err, auth.ErrWebAuthnCredentialNotFound):
		writeError(w, http.StatusNotFound, "not_found", notFoundMsg)
	case errors.Is(err, users.ErrSuperAdminProtected):
		writeError(w, http.StatusForbidden, "super_admin_protected", "Solo un superadministrador puede gestionar a otro superadministrador o conceder ese rol.")
	case errors.Is(err, users.ErrSelfRoleChange):
		writeError(w, http.StatusBadRequest, "invalid_request", "No puedes cambiar tu propio rol.")
	case errors.Is(err, accountadmin.ErrSelfAction):
		writeError(w, http.StatusBadRequest, "invalid_request", "Esta acción es para otra cuenta; la tuya se gestiona desde tu perfil.")
	case errors.Is(err, users.ErrLastSuperAdmin):
		writeError(w, http.StatusConflict, "last_super_admin", "Tiene que quedar al menos un superadministrador activo.")
	case errors.Is(err, users.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid_role", err.Error())
	case errors.Is(err, accountadmin.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, "invalid_request", "La contraseña debe tener al menos 8 caracteres.")
	case errors.Is(err, accountadmin.ErrTOTPNotEnabled):
		writeError(w, http.StatusConflict, "totp_not_enabled", "La cuenta no tiene la verificación en dos pasos activada.")
	case errors.Is(err, users.ErrInvalidGroupName):
		writeError(w, http.StatusBadRequest, "invalid_request", "El nombre del grupo es obligatorio (máximo 255 caracteres).")
	case errors.Is(err, users.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "already_exists", "Ya existe un grupo con ese nombre.")
	default:
		h.Logger.Error("administración de cuentas", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la operación.")
	}
}

type createUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Password    string `json:"password"`
	Role        string `json:"role,omitempty"`
	// QuotaBytes: cuota propia opcional (§24); ausente o null = hereda.
	QuotaBytes json.RawMessage `json:"quota_bytes,omitempty"`
}

// CreateUser es la vía de alta directa por administrador (§21), alternativa
// a canjear una invitación.
func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	var req createUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	if req.Username == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "invalid_request", "username y password (mínimo 8 caracteres) son obligatorios.")
		return
	}
	quota, err := parseQuotaField(req.QuotaBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// Dar de alta un super_admin es conceder ese rol (ADR-042 Decisión 8).
	if req.Role == users.RoleSuperAdmin {
		actorRole, err := h.UserSvc.PrimaryRole(r.Context(), actor.ID)
		if err != nil {
			h.writeAccountAdminError(w, err, "Usuario no encontrado.")
			return
		}
		if actorRole != users.RoleSuperAdmin {
			h.writeAccountAdminError(w, users.ErrSuperAdminProtected, "")
			return
		}
	}

	hash, err := h.Hasher.Hash(req.Password)
	if err != nil {
		h.Logger.Error("hasheando contraseña", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo crear el usuario.")
		return
	}

	u, err := h.UserSvc.CreateUser(r.Context(), users.CreateUserInput{
		Username: req.Username, DisplayName: req.DisplayName, Email: req.Email,
		PasswordHash: hash, Role: req.Role, QuotaBytes: quota.value,
	})
	if err != nil {
		writeCreateUserError(w, err)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventUserCreated, actor.ID, "user", u.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"username": u.Username})
	resp := toUserResponse(u)
	resp.Role = req.Role
	if resp.Role == "" {
		resp.Role = users.RoleUser
	}
	writeJSON(w, http.StatusCreated, resp)
}

func writeCreateUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, users.ErrInvalidUsername):
		writeError(w, http.StatusBadRequest, "invalid_username", err.Error())
	case errors.Is(err, users.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "already_exists", "Ya existe un usuario con ese nombre.")
	case errors.Is(err, users.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid_role", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo crear el usuario.")
	}
}

type patchUserRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Email       *string `json:"email,omitempty"`
	Status      *string `json:"status,omitempty"` // "active" | "disabled"
	// QuotaBytes es tri-estado (§24, ADR-036): ausente = no se toca, null =
	// sin cuota propia (hereda del grupo o la global), 0 = ilimitada, >0 =
	// límite en bytes. Por eso es un json.RawMessage y no un *int64.
	QuotaBytes json.RawMessage `json:"quota_bytes,omitempty"`
}

func (h *Handlers) PatchUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	u, err := h.UserRepo.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Usuario no encontrado.")
		return
	}
	// Un administrator no edita a un super_admin (ADR-042 Decisión 8).
	if err := h.UserSvc.EnsureCanManage(r.Context(), actor.ID, id); err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}
	var req patchUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	quota, err := parseQuotaField(req.QuotaBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	quotaBefore := u.QuotaBytes
	if quota.present {
		u.QuotaBytes = quota.value
	}
	if req.DisplayName != nil {
		u.DisplayName = *req.DisplayName
	}
	if req.Email != nil {
		u.Email = *req.Email
	}
	statusChanged := false
	if req.Status != nil {
		switch users.Status(*req.Status) {
		case users.StatusDisabled:
			// Mismo criterio que DeleteUser: desactivarse a uno mismo puede
			// dejar la instancia sin ningún administrador capaz de deshacerlo.
			if id == actor.ID {
				writeError(w, http.StatusBadRequest, "invalid_request", "No puedes desactivar tu propia cuenta.")
				return
			}
			// Nunca cero super_admin activos (ADR-042 Decisión 8).
			if u.Status != users.StatusDisabled {
				if err := h.UserSvc.EnsureCanRemove(r.Context(), actor.ID, id); err != nil {
					h.writeAccountAdminError(w, err, "Usuario no encontrado.")
					return
				}
			}
			statusChanged = u.Status != users.StatusDisabled
			u.Status = users.StatusDisabled
		case users.StatusActive:
			statusChanged = u.Status != users.Status(*req.Status)
			u.Status = users.Status(*req.Status)
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", "status debe ser 'active' o 'disabled'.")
			return
		}
	}

	u.UpdatedAt = time.Now().UTC()
	if err := h.UserRepo.UpdateUser(r.Context(), u); err != nil {
		h.Logger.Error("actualizando usuario", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo actualizar el usuario.")
		return
	}
	if statusChanged && u.Status == users.StatusDisabled {
		h.AuditLog.Record(r.Context(), audit.EventUserDisabled, actor.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies), nil)
	}
	if quota.present && !sameQuota(quotaBefore, quota.value) {
		h.AuditLog.Record(r.Context(), audit.EventQuotaChanged, actor.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies),
			map[string]any{"before": quotaValue(quotaBefore), "after": quotaValue(quota.value)})
	}
	resp, err := h.withRole(r.Context(), u)
	if err != nil {
		h.Logger.Error("consultando el rol", "error", err)
	}
	writeJSON(w, http.StatusOK, resp)
}

// DeleteUser exige reautenticación reforzada (§126, ADR-042 Decisión 2: el
// router lo monta tras RequireRecentReauth). Un administrator no borra a un
// super_admin, y nunca se borra el último super_admin activo.
func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if id == actor.ID {
		writeError(w, http.StatusBadRequest, "invalid_request", "No puedes eliminar tu propia cuenta.")
		return
	}
	if err := h.UserSvc.EnsureCanRemove(r.Context(), actor.ID, id); err != nil {
		h.writeAccountAdminError(w, err, "Usuario no encontrado.")
		return
	}

	if err := h.UserRepo.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, users.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Usuario no encontrado.")
			return
		}
		h.Logger.Error("eliminando usuario", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo eliminar el usuario.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventUserDeleted, actor.ID, "user", id, security.ClientIP(r, h.TrustedProxies), nil)
	w.WriteHeader(http.StatusNoContent)
}
