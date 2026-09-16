package apiv1

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/security"
)

// NewRouter construye el árbol de rutas /api/v1 (§42). loginLimiter aplica
// rate limiting específico al endpoint de login (§27, más estricto);
// apiLimiter cubre el resto de la API; publicLimiter cubre los enlaces
// públicos de compartición (§37), la otra superficie sin sesión.
func NewRouter(h *Handlers, loginLimiter, apiLimiter, publicLimiter *security.RateLimiter) http.Handler {
	r := chi.NewRouter()
	keyFunc := func(req *http.Request) string { return security.ClientIP(req, h.TrustedProxies) }

	r.Use(apiLimiter.Middleware(keyFunc))

	// Rutas públicas: login e invitations/redeem son los dos únicos puntos
	// de entrada sin sesión previa (§21: no hay registro público).
	r.Group(func(r chi.Router) {
		r.Use(loginLimiter.Middleware(keyFunc))
		r.Post("/auth/login", h.Login)
	})
	r.Post("/invitations/redeem", h.RedeemInvitation)

	// Enlaces públicos (§37): sin sesión, autorizados solo por el token en
	// la URL (y contraseña opcional vía cabecera X-Share-Password). Rate
	// limit propio y más estricto -- objetivo obvio de fuerza bruta sobre la
	// contraseña, igual motivo que loginLimiter.
	r.Group(func(r chi.Router) {
		r.Use(publicLimiter.Middleware(keyFunc))
		r.Get("/public/shares/{token}", h.GetPublicShare)
		r.Get("/public/shares/{token}/download", h.DownloadPublicShare)
		r.Get("/public/shares/{token}/browse", h.BrowsePublicShare)
		r.Post("/public/shares/{token}/upload", h.UploadPublicShare)

		// Actualizaciones del cliente de escritorio (ADR-032): sin sesión a
		// propósito, igual que los enlaces públicos -- instalar/actualizar
		// debe funcionar antes de poder haber iniciado sesión. El canal
		// (releases.<channel>.json) es un detalle de configuración del
		// SERVIDOR (ClientUpdatesProxy ya sabe cuál es el suyo), así que la
		// URL no lo expone -- el cliente solo pide "el feed", sin más. Con
		// ClientUpdatesProxy == nil (clientUpdates.enabled=false, por
		// defecto), estas dos rutas ni se registran.
		if h.ClientUpdatesProxy != nil {
			r.Get("/public/client-updates/releases.json", h.GetClientUpdatesFeed)
			r.Get("/public/client-updates/download/{assetName}", h.DownloadClientUpdateAsset)
		}
	})

	r.Group(func(r chi.Router) {
		r.Use(h.RequireAuth)

		r.Post("/auth/logout", h.Logout)
		r.Get("/auth/sessions", h.ListSessions)
		r.Delete("/auth/sessions/{id}", h.RevokeSession)
		r.Post("/auth/totp/enroll", h.EnrollTOTP)
		r.Post("/auth/totp/verify", h.VerifyTOTP)

		r.Get("/users/me", h.Me)

		r.Get("/files", h.ListFiles)
		r.Post("/files", h.UploadFile)
		r.Get("/files/{id}", h.DownloadFile)
		r.Delete("/files/{id}", h.DeleteFile)
		r.Patch("/files/{id}", h.MoveFile)
		r.Post("/files/{id}/restore", h.RestoreFile)
		r.Get("/files/{id}/versions", h.ListFileVersions)
		r.Get("/files/{id}/versions/{versionNum}", h.DownloadFileVersion)
		r.Post("/files/{id}/versions/{versionNum}/restore", h.RestoreFileVersion)
		r.Post("/directories", h.Mkdir)
		r.Delete("/directories/{id}", h.DeleteDirectory)
		r.Patch("/directories/{id}", h.MoveDirectory)
		r.Post("/directories/{id}/restore", h.RestoreDirectory)

		r.Get("/trash", h.ListTrash)

		r.Get("/groups", h.ListGroups)

		r.Post("/shares", h.CreateShare)
		r.Get("/shares", h.ListShares)
		r.Delete("/shares/{id}", h.RevokeShare)
		r.Get("/shared-directories/{id}", h.ListSharedDirectory)

		r.Group(func(r chi.Router) {
			r.Use(h.RequireAdmin)

			r.Get("/users", h.ListUsers)
			r.Post("/users", h.CreateUser)
			r.Patch("/users/{id}", h.PatchUser)
			r.Delete("/users/{id}", h.DeleteUser)

			r.Get("/invitations", h.ListInvitations)
			r.Post("/invitations", h.CreateInvitation)
			r.Delete("/invitations/{id}", h.RevokeInvitation)

			r.Post("/groups", h.CreateGroup)
			r.Post("/groups/{id}/members", h.AddGroupMember)

			r.Get("/audit", h.ListAuditEvents)

			r.Get("/storage/disks", h.ListDisks)
		})
	})

	// Backups entrantes de otro servidor NexusCloud (ADR-029): fuera del
	// árbol de sesión de usuario, protegido solo por requireBackupToken.
	// Con BackupReceiveToken vacío (por defecto), esta capacidad ni se
	// registra -- secure by default, mismo criterio que publicLinksEnabled.
	if h.BackupReceiveToken != "" {
		r.Route("/backups/inbound/{jobID}", func(r chi.Router) {
			r.Use(h.requireBackupToken)
			r.Put("/files/*", h.UploadInboundBackupFile)
			r.Get("/files/*", h.DownloadInboundBackupFile)
			r.Delete("/files/*", h.DeleteInboundBackupFile)
			r.Post("/complete", h.CompleteInboundBackup)
			r.Delete("/", h.DeleteInboundBackupJob)
		})
	}

	return r
}
