package apiv1

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/security"
)

// NewRouter construye el árbol de rutas /api/v1 (§42). loginLimiter aplica
// rate limiting específico al endpoint de login (§27, más estricto);
// apiLimiter cubre el resto de la API.
func NewRouter(h *Handlers, loginLimiter, apiLimiter *security.RateLimiter) http.Handler {
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
		r.Post("/files/{id}/restore", h.RestoreFile)
		r.Post("/directories", h.Mkdir)
		r.Delete("/directories/{id}", h.DeleteDirectory)
		r.Post("/directories/{id}/restore", h.RestoreDirectory)

		r.Get("/trash", h.ListTrash)

		r.Group(func(r chi.Router) {
			r.Use(h.RequireAdmin)

			r.Get("/users", h.ListUsers)
			r.Post("/users", h.CreateUser)
			r.Patch("/users/{id}", h.PatchUser)
			r.Delete("/users/{id}", h.DeleteUser)

			r.Get("/invitations", h.ListInvitations)
			r.Post("/invitations", h.CreateInvitation)
			r.Delete("/invitations/{id}", h.RevokeInvitation)

			r.Get("/audit", h.ListAuditEvents)
		})
	})

	return r
}
