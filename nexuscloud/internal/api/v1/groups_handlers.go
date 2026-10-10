package apiv1

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/users"
)

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

type createGroupRequest struct {
	Name string `json:"name"`
	// QuotaBytes: cuota por miembro opcional (§24); ausente o null = el grupo
	// no aporta cuota.
	QuotaBytes json.RawMessage `json:"quota_bytes,omitempty"`
}

// CreateGroup cierra el hueco encontrado en la auditoría "todo por
// comandos" (2026-09-15): users.Repository.CreateGroup existía desde antes,
// pero ningún handler HTTP lo invocaba -- la única vía de crear un grupo
// era un INSERT SQL a mano. Admin-only, mismo criterio que CreateUser.
func (h *Handlers) CreateGroup(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	var req createGroupRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name es obligatorio.")
		return
	}
	quota, err := parseQuotaField(req.QuotaBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	g := &users.Group{ID: idgen.New(), Name: req.Name, QuotaBytes: quota.value, CreatedAt: time.Now().UTC()}
	if err := h.UserRepo.CreateGroup(r.Context(), g); err != nil {
		if errors.Is(err, users.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "already_exists", "Ya existe un grupo con ese nombre.")
			return
		}
		h.Logger.Error("creando grupo", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo crear el grupo.")
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventGroupCreated, actor.ID, "group", g.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"name": g.Name})
	writeJSON(w, http.StatusCreated, toGroupResponse(g))
}

type patchGroupRequest struct {
	// Name (ADR-042 Decisión 11): ausente = no se renombra.
	Name *string `json:"name,omitempty"`
	// QuotaBytes es tri-estado: null = el grupo deja de aportar cuota, 0 =
	// ilimitada para sus miembros, >0 = límite por miembro; ausente = no se
	// toca. Hace falta al menos uno de los dos campos.
	QuotaBytes json.RawMessage `json:"quota_bytes"`
}

// PatchGroup renombra el grupo (ADR-042 Decisión 11) y/o fija su cuota por
// miembro (§24, ADR-036). Un PATCH sin ninguno de los dos campos no haría
// nada y se rechaza. Admin-only.
func (h *Handlers) PatchGroup(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	group, err := h.UserRepo.GetGroupByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Grupo no encontrado.")
			return
		}
		h.Logger.Error("buscando grupo", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo actualizar el grupo.")
		return
	}

	var req patchGroupRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	quota, err := parseQuotaField(req.QuotaBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !quota.present && req.Name == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Indica name y/o quota_bytes (null = sin cuota, 0 = ilimitada).")
		return
	}

	if req.Name != nil && strings.TrimSpace(*req.Name) != group.Name {
		newName := strings.TrimSpace(*req.Name)
		if err := h.UserSvc.RenameGroup(r.Context(), id, newName); err != nil {
			h.writeAccountAdminError(w, err, "Grupo no encontrado.")
			return
		}
		h.AuditLog.Record(r.Context(), audit.EventGroupRenamed, actor.ID, "group", group.ID, security.ClientIP(r, h.TrustedProxies),
			map[string]any{"before": group.Name, "after": newName})
		group.Name = newName
	}

	if quota.present {
		if err := h.UserSvc.SetGroupQuota(r.Context(), id, quota.value); err != nil {
			if errors.Is(err, users.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Grupo no encontrado.")
				return
			}
			h.Logger.Error("actualizando la cuota del grupo", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo actualizar el grupo.")
			return
		}
		if !sameQuota(group.QuotaBytes, quota.value) {
			h.AuditLog.Record(r.Context(), audit.EventQuotaChanged, actor.ID, "group", group.ID, security.ClientIP(r, h.TrustedProxies),
				map[string]any{"name": group.Name, "before": quotaValue(group.QuotaBytes), "after": quotaValue(quota.value)})
		}
		group.QuotaBytes = quota.value
	}
	writeJSON(w, http.StatusOK, toGroupResponse(group))
}

// ListGroupMembers (GET /groups/{id}/members, ADR-042 Decisión 11).
func (h *Handlers) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.UserSvc.ListGroupMembers(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeAccountAdminError(w, err, "Grupo no encontrado.")
		return
	}
	out := make([]userResponse, 0, len(members))
	for _, u := range members {
		out = append(out, toUserResponse(u))
	}
	writeJSON(w, http.StatusOK, out)
}

// RemoveGroupMember (DELETE /groups/{id}/members/{userId}).
func (h *Handlers) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	groupID, userID := chi.URLParam(r, "id"), chi.URLParam(r, "userId")
	if err := h.UserSvc.RemoveGroupMember(r.Context(), groupID, userID); err != nil {
		h.writeAccountAdminError(w, err, "Ese usuario no es miembro del grupo.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventGroupMemberRemoved, actor.ID, "group", groupID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

// DeleteGroup (DELETE /groups/{id}?expected_shares=N, con reautenticación).
// Borrar un grupo se lleva por la cascada de la FK sus membresías y las
// comparticiones dirigidas a él (ADR-042 Decisión 11). Para que nadie las
// pierda sin saberlo, el cliente tiene que confirmar cuántas son: sin
// expected_shares, o con un número que ya no coincide, responde 409
// confirm_required con el recuento actual y no borra nada.
func (h *Handlers) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	group, err := h.UserRepo.GetGroupByID(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "Grupo no encontrado.")
		return
	}
	shares, err := h.Files.CountActiveSharesForGroup(r.Context(), id)
	if err != nil {
		h.writeAccountAdminError(w, err, "")
		return
	}
	expected, convErr := strconv.Atoi(r.URL.Query().Get("expected_shares"))
	if convErr != nil || expected != shares {
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{
			"code":    "confirm_required",
			"message": "Confirma el borrado: se perderán las comparticiones dirigidas a este grupo.",
			"shares":  shares,
		}})
		return
	}
	if err := h.UserSvc.DeleteGroup(r.Context(), id); err != nil {
		h.writeAccountAdminError(w, err, "Grupo no encontrado.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventGroupDeleted, actor.ID, "group", id,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"name": group.Name, "shares_lost": shares})
	w.WriteHeader(http.StatusNoContent)
}

type addGroupMemberRequest struct {
	UserID string `json:"user_id"`
}

// AddGroupMember es el otro lado del mismo hueco que CreateGroup: sin esto,
// meter un usuario en un grupo también exigía un INSERT SQL a mano.
// Comprueba grupo Y usuario ANTES de llamar a AddUserToGroup a propósito --
// esa función trata una fila duplicada como éxito silencioso (idempotente),
// así que un id inexistente solo se distinguiría de una violación de FK
// cruda si no se comprobara aquí primero, filtrando un error interno al
// cliente en vez de un 404 legible.
func (h *Handlers) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFromContext(r.Context())
	groupID := chi.URLParam(r, "id")

	group, err := h.UserRepo.GetGroupByID(r.Context(), groupID)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Grupo no encontrado.")
			return
		}
		h.Logger.Error("buscando grupo", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo añadir el miembro.")
		return
	}

	var req addGroupMemberRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "user_id es obligatorio.")
		return
	}
	if _, err := h.UserRepo.GetUserByID(r.Context(), req.UserID); err != nil {
		if errors.Is(err, users.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Usuario no encontrado.")
			return
		}
		h.Logger.Error("buscando usuario", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo añadir el miembro.")
		return
	}

	if err := h.UserRepo.AddUserToGroup(r.Context(), req.UserID, group.ID); err != nil {
		h.Logger.Error("añadiendo miembro a grupo", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo añadir el miembro.")
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventGroupMemberAdded, actor.ID, "group", group.ID,
		security.ClientIP(r, h.TrustedProxies), map[string]any{"user_id": req.UserID})
	w.WriteHeader(http.StatusNoContent)
}
