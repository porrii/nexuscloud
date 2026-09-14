package apiv1

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/porrii/nexuscloud/internal/auth"
)

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("nexuscloud_session"); err == nil {
		return c.Value
	}
	return ""
}

// RequireAuth exige un token de sesión válido (cookie o Bearer) y añade el
// usuario/sesión al contexto de la petición.
func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Autenticación requerida.")
			return
		}
		u, sess, err := h.Auth.ValidateToken(r.Context(), token)
		if err != nil {
			if !errors.Is(err, auth.ErrSessionNotFound) && !errors.Is(err, auth.ErrUserDisabled) {
				h.Logger.Warn("error validando token de sesión", "error", err)
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "Sesión inválida o expirada.")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, u)
		ctx = context.WithValue(ctx, ctxSession, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin debe montarse después de RequireAuth. Comprueba el rol en
// cada petición (nunca confía en un claim cacheado del cliente, §168).
func (h *Handlers) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Autenticación requerida.")
			return
		}
		isAdmin, err := h.UserSvc.IsAdmin(r.Context(), u.ID)
		if err != nil {
			h.Logger.Error("comprobando privilegios de administrador", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la operación.")
			return
		}
		if !isAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Esta operación requiere privilegios de administrador.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
