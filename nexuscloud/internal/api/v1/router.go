package apiv1

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/porrii/nexuscloud/internal/security"
)

// NewRouter construye el árbol de rutas /api/v1 (§42). loginLimiter aplica
// rate limiting específico al endpoint de login (§27, más estricto);
// apiLimiter cubre el resto de la API; publicLimiter cubre los enlaces
// públicos de compartición (§37); anonymousUploadLimiter cubre los enlaces
// de subida anónima (§38, ADR-039) -- superficie propia, más estricta que
// publicLimiter (ver security.rateLimit.anonymousUploadPerMinute).
func NewRouter(h *Handlers, loginLimiter, apiLimiter, publicLimiter, anonymousUploadLimiter *security.RateLimiter) http.Handler {
	r := chi.NewRouter()
	keyFunc := func(req *http.Request) string { return security.ClientIP(req, h.TrustedProxies) }

	// middleware.Recoverer (§34, ADR-041 Decisión 6, hallazgo CRÍTICO del
	// pase de security-reviewer sobre el diseño de miniaturas): hasta esta
	// fase ningún panic de Go se recuperaba en todo el proyecto -- nunca
	// había código de parseo real expuesto a bytes adversariales. Un panic
	// de decodificación (índice fuera de rango, división por cero...) en
	// CUALQUIER handler tumbaría el proceso entero para todos los
	// inquilinos. Va primero, antes que cualquier otro middleware, para
	// cubrir también un panic dentro de ellos.
	r.Use(middleware.Recoverer)
	r.Use(apiLimiter.Middleware(keyFunc))

	// Rutas públicas: login e invitations/redeem son los dos únicos puntos
	// de entrada sin sesión previa (§21: no hay registro público).
	r.Group(func(r chi.Router) {
		r.Use(loginLimiter.Middleware(keyFunc))
		r.Post("/auth/login", h.Login)

		// Login con passkey (§25, ADR-033): mismo rate limit que
		// /auth/login, mismo motivo -- objetivo obvio de fuerza bruta.
		// Cubre tanto el segundo factor (con username+password) como el
		// login passwordless discoverable (sin ellos); ver
		// BeginWebAuthnLogin. Con h.WebAuthn == nil
		// (security.webAuthn.enabled=false, por defecto), estas rutas ni
		// se registran -- mismo criterio que ClientUpdatesProxy.
		if h.WebAuthn != nil {
			r.Post("/auth/webauthn/login/begin", h.BeginWebAuthnLogin)
			r.Post("/auth/webauthn/login/finish", h.FinishWebAuthnLogin)
		}
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

	// Subida anónima (§38, ADR-039): sin sesión, sin contraseña, y sin
	// NINGÚN endpoint de navegación -- el token en la URL es la única
	// autorización posible. Grupo propio (no publicLimiter compartido):
	// cualquiera con el enlace escribe sin que el creador haya podido vetar
	// a nadie, así que el límite por defecto es más estricto que el de
	// shares.
	r.Group(func(r chi.Router) {
		r.Use(anonymousUploadLimiter.Middleware(keyFunc))
		r.Get("/public/anonymous-uploads/{token}", h.GetAnonymousUploadLink)
		r.Post("/public/anonymous-uploads/{token}/upload", h.UploadAnonymousUploadLink)
	})

	r.Group(func(r chi.Router) {
		r.Use(h.RequireAuth)

		r.Post("/auth/logout", h.Logout)
		// Modo «sudo» (§126, ADR-042 Decisión 2): mismo rate limit que el
		// login, porque también comprueba una contraseña. Autoservicio: una
		// cuenta read_only también puede (no escribe datos).
		r.With(loginLimiter.Middleware(keyFunc)).Post("/auth/reauthenticate", h.Reauthenticate)
		r.Get("/auth/sessions", h.ListSessions)
		r.Delete("/auth/sessions/{id}", h.RevokeSession)
		r.Post("/auth/totp/enroll", h.EnrollTOTP)
		r.Post("/auth/totp/verify", h.VerifyTOTP)

		// Tokens de API (§78, ADR-037): acceso todo-o-nada a la API REST
		// completa, sin la opción de config que sí tiene WebDAV -- siempre
		// disponibles, como las sesiones.
		r.Post("/auth/api-tokens", h.CreateAPIToken)
		r.Get("/auth/api-tokens", h.ListAPITokens)
		r.Delete("/auth/api-tokens/{id}", h.RevokeAPIToken)

		if h.WebAuthn != nil {
			r.Post("/auth/webauthn/register/begin", h.BeginWebAuthnRegistration)
			r.Post("/auth/webauthn/register/finish", h.FinishWebAuthnRegistration)
			r.Get("/auth/webauthn/credentials", h.ListWebAuthnCredentials)
			r.Delete("/auth/webauthn/credentials/{id}", h.RevokeWebAuthnCredential)
		}

		// Tokens de acceso WebDAV (§43, ADR-034): la contraseña que usan los
		// clientes WebDAV por HTTP Basic. Solo con webdav.enabled=true.
		if h.WebDAVTokens != nil {
			r.Post("/auth/webdav/tokens", h.CreateWebDAVToken)
			r.Get("/auth/webdav/tokens", h.ListWebDAVTokens)
			r.Delete("/auth/webdav/tokens/{id}", h.RevokeWebDAVToken)
		}

		r.Get("/users/me", h.Me)
		r.Get("/users/me/quota", h.MyQuota)

		// Favoritos (§87, ADR-038): solo sobre el árbol propio del usuario.
		r.Post("/favorites", h.CreateFavorite)
		r.Get("/favorites", h.ListFavorites)
		r.Delete("/favorites/{id}", h.DeleteFavorite)

		// Revocar lo publicado (ADR-044 Decisión 1): reducir lo expuesto
		// nunca es una escritura peligrosa, así que también read_only puede.
		r.Delete("/shares/{id}", h.RevokeShare)
		r.Delete("/anonymous-uploads/{id}", h.RevokeAnonymousUploadLink)

		// Rol read_only (ADR-044): TODO lo que se registre a partir de aquí
		// pasa por RequireWritable, que deniega cualquier método que no sea
		// GET/HEAD/OPTIONS a un usuario read_only. Encima de este punto solo
		// va el autoservicio de la propia cuenta (sesiones, 2FA, passkeys,
		// tokens, favoritos y revocar lo propio): una ruta nueva va aquí
		// debajo salvo decisión expresa, y el test de integración
		// TestReadOnlyAccountCannotWriteThroughAnyRoute lo vigila.
		r.Group(func(r chi.Router) {
			r.Use(h.RequireWritable)

			// Actividad reciente (§88, ADR-038): feed de "Recientes" del dashboard.
			r.Get("/activity", h.ListActivity)

			// Búsqueda (§33): recursiva sobre TODO el árbol propio, a diferencia
			// de GET /files (una carpeta concreta). Con search.enabled=false,
			// esta ruta ni se registra -- mismo criterio que ClientUpdatesProxy.
			if h.SearchEnabled {
				r.Get("/search", h.Search)
			}

			r.Get("/files", h.ListFiles)
			r.Post("/files", h.UploadFile)
			r.Get("/files/{id}", h.DownloadFile)
			r.Delete("/files/{id}", h.DeleteFile)
			r.Patch("/files/{id}", h.MoveFile)
			r.Post("/files/{id}/restore", h.RestoreFile)
			// Miniaturas (§34, ADR-041): sin flag propio de activación de ruta
			// (a diferencia de /search) -- thumbnails.enabled se comprueba
			// dentro de FileService, mismo criterio que
			// sharing.anonymousUploadEnabled; con la función desactivada,
			// responde 404 en vez de dejar de existir la ruta.
			r.Get("/files/{id}/thumbnail", h.GetFileThumbnail)
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
			r.Get("/shared-directories/{id}", h.ListSharedDirectory)
			r.Post("/shared-directories/{id}/files", h.UploadToSharedDirectory)

			// Subida anónima (§38, ADR-039): autoservicio -- cada usuario crea,
			// lista y revoca sus propios enlaces, siempre sobre una carpeta suya
			// (revocar está registrado arriba, fuera de RequireWritable).
			r.Post("/anonymous-uploads", h.CreateAnonymousUploadLink)
			r.Get("/anonymous-uploads", h.ListAnonymousUploadLinks)

			r.Group(func(r chi.Router) {
				r.Use(h.RequireAdmin)

				r.Get("/users", h.ListUsers)
				r.Post("/users", h.CreateUser)
				r.Patch("/users/{id}", h.PatchUser)

				// Administración de cuentas (ADR-042, B1). El cambio de rol
				// exige reautenticación solo hacia o desde administrador
				// (lo decide el handler); el resto de lo destructivo, siempre.
				r.Put("/users/{id}/role", h.SetUserRole)
				r.Get("/users/{id}/passkeys", h.ListUserPasskeys)
				r.Get("/users/{id}/sessions", h.ListUserSessions)
				r.Delete("/users/{id}/sessions", h.RevokeAllUserSessions)
				r.Delete("/users/{id}/sessions/{sessionId}", h.RevokeUserSession)
				r.Get("/users/{id}/api-tokens", h.ListUserAPITokens)
				r.Delete("/users/{id}/api-tokens/{tokenId}", h.RevokeUserAPIToken)
				r.Get("/users/{id}/webdav-tokens", h.ListUserWebDAVTokens)
				r.Delete("/users/{id}/webdav-tokens/{tokenId}", h.RevokeUserWebDAVToken)
				r.Get("/users/{id}/shares", h.ListUserShares)
				r.Delete("/users/{id}/shares/{shareId}", h.RevokeUserShare)
				r.Get("/users/{id}/anonymous-uploads", h.ListUserAnonymousUploads)
				r.Delete("/users/{id}/anonymous-uploads/{linkId}", h.RevokeUserAnonymousUpload)

				r.Group(func(r chi.Router) {
					r.Use(h.RequireRecentReauth)
					r.Delete("/users/{id}", h.DeleteUser)
					r.Post("/users/{id}/password", h.ResetUserPassword)
					r.Delete("/users/{id}/totp", h.RemoveUserTOTP)
					r.Delete("/users/{id}/passkeys/{credentialId}", h.RemoveUserPasskey)
					r.Delete("/groups/{id}", h.DeleteGroup)
				})

				r.Get("/invitations", h.ListInvitations)
				r.Post("/invitations", h.CreateInvitation)
				r.Delete("/invitations/{id}", h.RevokeInvitation)

				r.Post("/groups", h.CreateGroup)
				r.Patch("/groups/{id}", h.PatchGroup)
				r.Get("/groups/{id}/members", h.ListGroupMembers)
				r.Post("/groups/{id}/members", h.AddGroupMember)
				r.Delete("/groups/{id}/members/{userId}", h.RemoveGroupMember)

				r.Get("/audit", h.ListAuditEvents)

				r.Get("/storage/disks", h.ListDisks)

				// Miniaturas (§34, ADR-041 Decisión 1): visibilidad
				// administrativa sobre la cola persistente -- qué está pendiente
				// o qué se dio por fallido tras agotar reintentos. Sin flag de
				// activación propio, igual que la ruta bajo demanda de arriba.
				r.Get("/admin/thumbnail-jobs", h.ListThumbnailJobs)

				// Búsqueda cruzando usuarios (§33 "Usuario" como criterio):
				// mismo interruptor que la búsqueda personal.
				if h.SearchEnabled {
					r.Get("/admin/search", h.AdminSearch)
				}
			})
		}) // fin de RequireWritable
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
