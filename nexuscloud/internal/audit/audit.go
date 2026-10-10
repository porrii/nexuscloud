// Package audit registra eventos de seguridad y actividad (§31). Se
// almacena separado de los logs de aplicación (§176): internal/logging es
// para operar el servicio, este paquete es el rastro de auditoría.
package audit

import (
	"context"
	"time"
)

// Tipos de evento (§31). No es una lista cerrada: los handlers de fases
// futuras (sharing, backups, snapshots...) añadirán las suyas.
const (
	EventLogin             = "login"
	EventLogout            = "logout"
	EventLoginFailed       = "login_failed"
	EventUserCreated       = "user_created"
	EventUserDisabled      = "user_disabled"
	EventUserDeleted       = "user_deleted"
	EventInvitationCreated = "invitation_created"
	EventInvitationRevoked = "invitation_revoked"
	EventUpload            = "upload"
	EventDownload          = "download"
	EventDelete            = "delete"
	EventMove              = "move"
	EventShareCreate       = "share_create"
	EventShareRevoke       = "share_revoke"
	EventConfigChanged     = "config_changed"
	EventGroupCreated      = "group_created"
	EventGroupMemberAdded  = "group_member_added"
	// EventQuotaChanged (ADR-036): un administrador cambia la cuota de un
	// usuario o de un grupo; los metadatos llevan el valor anterior (before) y
	// el nuevo (after), con null = sin cuota propia (hereda).
	EventQuotaChanged = "quota_changed"

	EventWebAuthnCredentialRegistered = "webauthn_credential_registered"
	EventWebAuthnCredentialRevoked    = "webauthn_credential_revoked"

	// WebDAV (§43, ADR-034): las operaciones sobre ficheros reutilizan los
	// tipos de la API REST (upload/download/delete/move) con via=webdav;
	// estos tres son propios del módulo.
	EventWebDAVTokenCreated = "webdav_token_created"
	EventWebDAVTokenRevoked = "webdav_token_revoked"
	EventAPITokenCreated    = "api_token_created"
	EventAPITokenRevoked    = "api_token_revoked"
	EventWebDAVAuthFailed   = "webdav_auth_failed"

	// Favoritos (§87, ADR-038): NO entran en el feed de "Recientes" (§88) --
	// marcar un favorito no es la clase de actividad que describe su
	// ejemplo ("Ivan subió: documento.pdf"). Se auditan igual que el resto
	// de altas/bajas de credenciales/comparticiones por consistencia.
	EventFavoriteAdded   = "favorite_added"
	EventFavoriteRemoved = "favorite_removed"

	// Subida anónima (§38, ADR-039): crear/revocar el enlace son eventos
	// propios; la subida en sí reutiliza EventUpload con via=anonymous_upload
	// en los metadatos (mismo criterio que via=shared_directory/public_share).
	EventAnonymousUploadLinkCreated = "anonymous_upload_link_created"
	EventAnonymousUploadLinkRevoked = "anonymous_upload_link_revoked"

	// Miniaturas (§34, ADR-041): solo al agotar los reintentos de un job
	// (thumbnail_jobs.status pasa a failed), nunca en cada intento
	// intermedio ni en éxito -- sería ruido, no señal de auditoría. Sin
	// actor humano (lo dispara el bucle en segundo plano o, bajo demanda,
	// la propia petición del dueño del archivo) -- ActorUserID puede ir
	// vacío, mismo criterio que EventLoginFailed. NO entra en
	// recentActivityEventTypes (activity_handlers.go), mismo criterio que
	// EventFavoriteAdded/Removed.
	EventThumbnailGenerationFailed = "thumbnail_generation_failed"

	// Administración de cuentas (ADR-042 Decisión 3, B1). Actor = el
	// administrador; objetivo = la cuenta o el grupo afectado.
	EventReauthenticated    = "reauthenticated"
	EventRoleChanged        = "role_changed"
	EventPasswordReset      = "password_reset"
	EventTOTPRemoved        = "totp_removed"
	EventSessionsRevoked    = "sessions_revoked"
	EventGroupMemberRemoved = "group_member_removed"
	EventGroupRenamed       = "group_renamed"
	EventGroupDeleted       = "group_deleted"
)

// Event lleva etiquetas JSON snake_case porque GET /audit lo serializa tal
// cual (ADR-042 Decisión 7); antes salían los nombres de campo de Go.
type Event struct {
	ID          string         `json:"id"`
	OccurredAt  time.Time      `json:"occurred_at"`
	ActorUserID string         `json:"actor_user_id,omitempty"` // vacío si el evento no tiene actor autenticado (p.ej. login_failed de usuario inexistente)
	EventType   string         `json:"event_type"`
	TargetType  string         `json:"target_type,omitempty"`
	TargetID    string         `json:"target_id,omitempty"`
	IP          string         `json:"ip,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type Repository interface {
	RecordEvent(ctx context.Context, e *Event) error
	ListEvents(ctx context.Context, limit, offset int) ([]*Event, error)
	// ListEventsForActor da el feed de "Recientes" (§88, ADR-038): eventos
	// de actorUserID cuyo event_type esté en eventTypes (nunca vacío -- qué
	// cuenta como "actividad" lo decide el llamador, no este paquete).
	ListEventsForActor(ctx context.Context, actorUserID string, eventTypes []string, limit, offset int) ([]*Event, error)
}
