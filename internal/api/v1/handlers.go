package apiv1

import (
	"log/slog"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

// Handlers agrupa las dependencias de todos los handlers de la API v1. Se
// construye una única vez en internal/server y se inyecta en el router.
type Handlers struct {
	Auth           *auth.Authenticator
	Hasher         *auth.Hasher
	Invitations    *auth.InvitationService
	InvitationRepo auth.InvitationRepository
	SessionRepo    auth.SessionRepository
	UserSvc        *users.Service
	UserRepo       users.Repository
	Files          *storage.FileService
	AuditLog       *audit.Recorder
	AuditRepo      audit.Repository
	Logger         *slog.Logger
	TrustedProxies []string
}
