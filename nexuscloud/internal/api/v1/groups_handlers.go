package apiv1

import (
	"encoding/json"
	"errors"
	"net/http"
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
	// QuotaBytes es obligatorio y tri-estado: null = el grupo deja de aportar
	// cuota, 0 = ilimitada para sus miembros, >0 = límite por miembro.
	QuotaBytes json.RawMessage `json:"quota_bytes"`
}

// PatchGroup fija la cuota por miembro de un grupo (§24, ADR-036). De momento
// es lo único que se puede cambiar de un grupo, así que el campo es
// obligatorio: un PATCH sin él no haría nada. Admin-only.
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
	if !quota.present {
		writeError(w, http.StatusBadRequest, "invalid_request", "quota_bytes es obligatorio (null = sin cuota, 0 = ilimitada).")
		return
	}

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
	writeJSON(w, http.StatusOK, toGroupResponse(group))
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
