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
	EventConfigChanged     = "config_changed"
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
