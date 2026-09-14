package apiv1

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/users"
)

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, toUserResponse(u))
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
		out = append(out, toUserResponse(u))
	}
	writeJSON(w, http.StatusOK, out)
}

type createUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Password    string `json:"password"`
	Role        string `json:"role,omitempty"`
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

	hash, err := h.Hasher.Hash(req.Password)
	if err != nil {
		h.Logger.Error("hasheando contraseña", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo crear el usuario.")
		return
	}

	u, err := h.UserSvc.CreateUser(r.Context(), users.CreateUserInput{
		Username: req.Username, DisplayName: req.DisplayName, Email: req.Email,
		PasswordHash: hash, Role: req.Role,
	})
	if err != nil {
		writeCreateUserError(w, err)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventUserCreated, actor.ID, "user", u.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"username": u.Username})
	writeJSON(w, http.StatusCreated, toUserResponse(u))
}

func writeCreateUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, users.ErrInvalidUsername):
		writeError(w, http.StatusBadRequest, "invalid_username", err.Error())
	case errors.Is(err, users.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "already_exists", "Ya existe un usuario con ese nombre.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo crear el usuario.")
	}
}

type patchUserRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Email       *string `json:"email,omitempty"`
	Status      *string `json:"status,omitempty"` // "active" | "disabled"
}

func (h *Handlers) PatchUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	u, err := h.UserRepo.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Usuario no encontrado.")
		return
	}
	var req patchUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
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
		case users.StatusActive, users.StatusDisabled:
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
	writeJSON(w, http.StatusOK, toUserResponse(u))
}

// DeleteUser exige reautenticación reforzada solo a nivel de política
// documentada por ahora (§126); la aplicación técnica (p.ej. exigir
// contraseña reciente) queda para una fase posterior sin cambiar este
// contrato de API.
func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if id == actor.ID {
		writeError(w, http.StatusBadRequest, "invalid_request", "No puedes eliminar tu propia cuenta.")
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
