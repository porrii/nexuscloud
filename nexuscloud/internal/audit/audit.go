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
)

type Event struct {
	ID          string
	OccurredAt  time.Time
	ActorUserID string // vacío si el evento no tiene actor autenticado (p.ej. login_failed de usuario inexistente)
	EventType   string
	TargetType  string
	TargetID    string
	IP          string
	Metadata    map[string]any
}

type Repository interface {
	RecordEvent(ctx context.Context, e *Event) error
	ListEvents(ctx context.Context, limit, offset int) ([]*Event, error)
}
