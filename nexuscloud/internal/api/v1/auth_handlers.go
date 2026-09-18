package apiv1

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/security"
)

const sessionCookieName = "nexuscloud_session"

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

type loginResponse struct {
	Token   string          `json:"token"`
	User    userResponse    `json:"user"`
	Session sessionResponse `json:"session"`
}

// Login nunca distingue en su respuesta entre "usuario inexistente" y
// "contraseña incorrecta" (§27, evita enumeración de usuarios).
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "username y password son obligatorios.")
		return
	}

	ip := security.ClientIP(r, h.TrustedProxies)
	res, err := h.Auth.Login(r.Context(), req.Username, req.Password, req.TOTPCode, r.UserAgent(), ip)
	if err != nil {
		h.AuditLog.Record(r.Context(), audit.EventLoginFailed, "", "user", req.Username, ip, map[string]any{"reason": err.Error()})

		status, code, msg := http.StatusUnauthorized, "unauthorized", "Usuario o contraseña incorrectos."
		switch {
		case errors.Is(err, auth.ErrWebAuthnRequired):
			// La contraseña ya era correcta: el cliente debe repetir la
			// verificación contra /auth/webauthn/login/begin (con el mismo
			// username+password) para completar el segundo factor -- ver el
			// comentario de Authenticator.Login sobre por qué WebAuthn no
			// puede resolverse en esta misma llamada como TOTP.
			code, msg = "webauthn_required", "Se requiere un passkey (WebAuthn)."
		case errors.Is(err, auth.ErrTOTPRequired):
			code, msg = "totp_required", "Se requiere código de doble factor."
		case errors.Is(err, auth.ErrTOTPInvalid):
			code, msg = "totp_invalid", "Código de doble factor incorrecto."
		case errors.Is(err, auth.ErrUserDisabled):
			code, msg = "user_disabled", "Este usuario está deshabilitado."
		}
		writeError(w, status, code, msg)
		return
	}

	h.AuditLog.Record(r.Context(), audit.EventLogin, res.User.ID, "user", res.User.ID, ip, nil)
	setSessionCookie(w, r, res.Token, res.Session.ExpiresAt)
	writeJSON(w, http.StatusOK, loginResponse{
		Token:   res.Token,
		User:    toUserResponse(res.User),
		Session: toSessionResponse(res.Session),
	})
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	sess, _ := SessionFromContext(r.Context())

	if err := h.Auth.Logout(r.Context(), sess.ID, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo cerrar la sesión.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventLogout, u.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies), nil)
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	sessions, err := h.SessionRepo.ListSessionsForUser(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("listando sesiones", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar las sesiones.")
		return
	}
	out := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, toSessionResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeSession exige, vía SessionRepository.RevokeSession, que la sesión
// pertenezca al usuario autenticado (§198 IDOR).
func (h *Handlers) RevokeSession(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.SessionRepo.RevokeSession(r.Context(), id, u.ID); err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Sesión no encontrada.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo revocar la sesión.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) EnrollTOTP(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	secret, url, err := auth.GenerateTOTP("NexusCloud", u.Username)
	if err != nil {
		h.Logger.Error("generando secreto TOTP", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo generar el secreto TOTP.")
		return
	}
	// El secreto no se persiste hasta que /totp/verify confirme un código
	// válido: evita bloquear al usuario con un secreto que nunca llegó a
	// registrar en su app autenticadora.
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_url": url})
}

type verifyTOTPRequest struct {
	Secret string `json:"secret"`
	Code   string `json:"code"`
}

func (h *Handlers) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	var req verifyTOTPRequest
	if err := readJSON(r, &req); err != nil || req.Secret == "" || req.Code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "secret y code son obligatorios.")
		return
	}
	if !auth.ValidateTOTP(req.Code, req.Secret) {
		writeError(w, http.StatusBadRequest, "totp_invalid", "El código no coincide con el secreto.")
		return
	}

	u.TOTPSecret = req.Secret
	u.UpdatedAt = time.Now().UTC()
	if err := h.UserRepo.UpdateUser(r.Context(), u); err != nil {
		h.Logger.Error("activando TOTP", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo activar 2FA.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, Expires: expires,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}
