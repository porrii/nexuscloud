package apiv1

import (
	"log/slog"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/backup"
	"github.com/porrii/nexuscloud/internal/clientupdates"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
	"github.com/porrii/nexuscloud/internal/webdav"
)

// Handlers agrupa las dependencias de todos los handlers de la API v1. Se
// construye una única vez en internal/server y se inyecta en el router.
type Handlers struct {
	Auth           *auth.Authenticator
	Hasher         *auth.Hasher
	Invitations    *auth.InvitationService
	InvitationRepo auth.InvitationRepository
	SessionRepo    auth.SessionRepository
	// APITokens (§78, ADR-037): tokens de acceso a la API REST completa, de
	// alcance todo-o-nada. A diferencia de WebDAVTokens, no depende de
	// ninguna opción de config -- siempre está disponible, como las
	// sesiones.
	APITokens      *auth.APITokenService
	UserSvc        *users.Service
	UserRepo       users.Repository
	Files          *storage.FileService
	AuditLog       *audit.Recorder
	AuditRepo      audit.Repository
	Logger         *slog.Logger
	TrustedProxies []string
	// BackupsDir/BackupRepo/BackupReceiveToken (ADR-029): solo se usan si
	// esta instancia actúa de RECEPTORA de backups de otro servidor
	// NexusCloud -- ver backup_remote_handlers.go. Con BackupReceiveToken
	// vacío (por defecto), NewRouter ni siquiera registra esas rutas
	// (secure by default, §3/§47).
	BackupsDir         string
	BackupRepo         backup.Repository
	BackupReceiveToken string
	// ClientUpdatesProxy (ADR-032): solo se usa si clientUpdates.enabled=true
	// en config.yaml -- ver client_updates_handlers.go. Con nil (por
	// defecto), NewRouter ni siquiera registra esas rutas, mismo criterio
	// exacto que BackupReceiveToken vacío.
	ClientUpdatesProxy *clientupdates.Proxy
	// WebAuthn (§25, ADR-033): solo se usa si security.webAuthn.enabled=true
	// en config.yaml -- ver webauthn_handlers.go. Con nil (por defecto),
	// NewRouter ni siquiera registra esas rutas, mismo criterio exacto que
	// ClientUpdatesProxy.
	WebAuthn *auth.WebAuthnService
	// WebDAVTokens (§43, ADR-034): solo se usa si webdav.enabled=true en
	// config.yaml -- ver webdav_handlers.go. Con nil (por defecto),
	// NewRouter ni siquiera registra las rutas de gestión de tokens.
	WebDAVTokens *webdav.TokenService
	// WebDAVPath es la ruta donde está montado WebDAV (webdav.path). Se
	// devuelve al crear un token para que la interfaz pueda mostrar la URL
	// real a configurar en el cliente en vez de suponer "/webdav". Solo
	// tiene efecto si WebDAVTokens != nil.
	WebDAVPath string
}
