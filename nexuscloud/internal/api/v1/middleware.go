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

// RequireAuth exige un token válido -- de sesión (cookie o Bearer) o de API
// (§78, ADR-037, siempre Bearer, reconocible por su prefijo) -- y añade el
// usuario al contexto de la petición. Un token de API no deja `ctxSession`
// poblado (no hay ninguna sesión real detrás): es alcance todo-o-nada, así
// que ningún handler necesita distinguir cómo se autenticó la petición.
func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Autenticación requerida.")
			return
		}

		if strings.HasPrefix(token, auth.APITokenPrefix) {
			u, _, err := h.APITokens.Authenticate(r.Context(), token)
			if err != nil {
				if !errors.Is(err, auth.ErrInvalidAPICredentials) {
					h.Logger.Warn("error validando token de API", "error", err)
				}
				writeError(w, http.StatusUnauthorized, "unauthorized", "Token inválido, expirado o revocado.")
				return
			}
			ctx := context.WithValue(r.Context(), ctxUser, u)
			next.ServeHTTP(w, r.WithContext(ctx))
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

// isSafeMethod: los métodos que no cambian nada (RFC 9110 §9.2.1).
func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// RequireWritable (ADR-044) debe montarse después de RequireAuth. Deniega
// por MÉTODO, no por ruta: cualquier petición que no sea GET, HEAD u OPTIONS
// de un usuario read_only recibe 403, así que una ruta nueva que escriba
// queda bloqueada sin tener que acordarse de nada. Las pocas escrituras de
// autoservicio que read_only sí puede hacer se registran fuera de este
// grupo (ver NewRouter). Las lecturas no hacen ninguna consulta extra; el
// rol se comprueba en cada petición contra la BD, nunca cacheado (§168).
// Los tokens de API quedan cubiertos igual: RequireAuth deja en el contexto
// al usuario dueño del token.
func (h *Handlers) RequireWritable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		u, ok := UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Autenticación requerida.")
			return
		}
		readOnly, err := h.UserSvc.IsReadOnly(r.Context(), u.ID)
		if err != nil {
			// Fallar cerrado: sin saber el rol, no se escribe nada.
			h.Logger.Error("comprobando el rol de solo lectura", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la operación.")
			return
		}
		if readOnly {
			writeError(w, http.StatusForbidden, "read_only_account", "Tu cuenta es de solo lectura.")
			return
		}
		next.ServeHTTP(w, r)
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
