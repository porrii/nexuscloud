package apiv1

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/users"
)

type webauthnCredentialResponse struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func toWebAuthnCredentialResponse(c *auth.WebAuthnCredential) webauthnCredentialResponse {
	return webauthnCredentialResponse{ID: c.ID, Label: c.Label, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt}
}

type webauthnCeremonyResponse struct {
	CeremonyID string `json:"ceremony_id"`
	*protocol.CredentialCreation
}

type webauthnLoginCeremonyResponse struct {
	CeremonyID string `json:"ceremony_id"`
	*protocol.CredentialAssertion
}

// BeginWebAuthnRegistration inicia el alta de un passkey nuevo para el
// usuario ya autenticado (§198: la sesión de la petición ya garantiza
// quién es).
func (h *Handlers) BeginWebAuthnRegistration(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	creation, ceremonyID, err := h.WebAuthn.BeginRegistration(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("iniciando registro WebAuthn", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo iniciar el registro del passkey.")
		return
	}
	writeJSON(w, http.StatusOK, webauthnCeremonyResponse{CeremonyID: ceremonyID, CredentialCreation: creation})
}

// FinishWebAuthnRegistration completa el alta. ceremony_id y label van en
// la query, igual que path/name en la subida de ficheros (files_handlers.go):
// el cuerpo es exclusivamente la respuesta cruda que da
// navigator.credentials.create(), tal cual la exige protocol.ParseCredentialCreationResponseBody.
func (h *Handlers) FinishWebAuthnRegistration(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	ceremonyID := r.URL.Query().Get("ceremony_id")
	label := r.URL.Query().Get("label")
	if ceremonyID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ceremony_id es obligatorio.")
		return
	}
	if label == "" {
		label = "Passkey"
	}

	cred, err := h.WebAuthn.FinishRegistration(r.Context(), u.ID, ceremonyID, label, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "webauthn_registration_failed", "No se pudo completar el registro del passkey.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventWebAuthnCredentialRegistered, u.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies), map[string]any{"label": cred.Label})
	writeJSON(w, http.StatusCreated, toWebAuthnCredentialResponse(cred))
}

type webauthnLoginBeginRequest struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// BeginWebAuthnLogin cubre los dos modos a la vez (§25, ADR-033): con
// username+password -- ya verificados como en /auth/login, pero sin
// completar sesión todavía -- es el segundo factor de esa cuenta; sin
// ellos es un login passwordless discoverable, el navegador decide qué
// passkey usar. Público, bajo publicLimiter (mismo objetivo de fuerza
// bruta que /auth/login, ver router.go).
func (h *Handlers) BeginWebAuthnLogin(w http.ResponseWriter, r *http.Request) {
	var req webauthnLoginBeginRequest
	_ = readJSON(r, &req) // cuerpo vacío = login discoverable, no es un error

	ip := security.ClientIP(r, h.TrustedProxies)

	if req.Username != "" {
		u, err := h.Auth.VerifyPassword(r.Context(), req.Username, req.Password)
		if err != nil {
			h.AuditLog.Record(r.Context(), audit.EventLoginFailed, "", "user", req.Username, ip, map[string]any{"reason": err.Error()})
			writeError(w, http.StatusUnauthorized, "unauthorized", "Usuario o contraseña incorrectos.")
			return
		}
		assertion, ceremonyID, err := h.WebAuthn.BeginLogin(r.Context(), u.ID)
		if err != nil {
			h.Logger.Error("iniciando login WebAuthn (2FA)", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo iniciar la comprobación del passkey.")
			return
		}
		writeJSON(w, http.StatusOK, webauthnLoginCeremonyResponse{CeremonyID: ceremonyID, CredentialAssertion: assertion})
		return
	}

	assertion, ceremonyID, err := h.WebAuthn.BeginDiscoverableLogin(r.Context())
	if err != nil {
		h.Logger.Error("iniciando login WebAuthn (passwordless)", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo iniciar el login con passkey.")
		return
	}
	writeJSON(w, http.StatusOK, webauthnLoginCeremonyResponse{CeremonyID: ceremonyID, CredentialAssertion: assertion})
}

// FinishWebAuthnLogin completa cualquiera de los dos modos de
// BeginWebAuthnLogin: username en la query = segundo factor (el cliente
// ya lo conoce, lo mandó en el begin); sin username = passwordless, el
// propio passkey resuelve quién es. El cuerpo es exclusivamente la
// respuesta cruda de navigator.credentials.get(), igual criterio que
// FinishWebAuthnRegistration.
func (h *Handlers) FinishWebAuthnLogin(w http.ResponseWriter, r *http.Request) {
	ceremonyID := r.URL.Query().Get("ceremony_id")
	username := r.URL.Query().Get("username")
	if ceremonyID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ceremony_id es obligatorio.")
		return
	}
	ip := security.ClientIP(r, h.TrustedProxies)

	var (
		resultUser *users.User
		err        error
	)
	if username != "" {
		u, lookupErr := h.UserRepo.GetUserByUsername(r.Context(), username)
		if lookupErr != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "No se pudo completar el login con passkey.")
			return
		}
		if _, err = h.WebAuthn.FinishLogin(r.Context(), u.ID, ceremonyID, r.Body); err == nil {
			resultUser = u
		}
	} else {
		resultUser, _, err = h.WebAuthn.FinishDiscoverableLogin(r.Context(), ceremonyID, r.Body)
	}
	if err != nil {
		h.AuditLog.Record(r.Context(), audit.EventLoginFailed, "", "user", username, ip, map[string]any{"reason": err.Error()})
		writeError(w, http.StatusUnauthorized, "webauthn_login_failed", "No se pudo completar el login con passkey.")
		return
	}

	res, err := h.Auth.CompleteWebAuthnLogin(r.Context(), resultUser, r.UserAgent(), ip)
	if err != nil {
		h.Logger.Error("completando sesión tras login WebAuthn", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar el login.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventLogin, res.User.ID, "user", res.User.ID, ip, map[string]any{"method": "webauthn"})
	setSessionCookie(w, r, res.Token, res.Session.ExpiresAt)
	writeJSON(w, http.StatusOK, loginResponse{
		Token:   res.Token,
		User:    toUserResponse(res.User),
		Session: toSessionResponse(res.Session),
	})
}

// ListWebAuthnCredentials devuelve los passkeys del propio usuario, para
// que los gestione desde su cuenta.
func (h *Handlers) ListWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	creds, err := h.WebAuthn.ListCredentials(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("listando credenciales WebAuthn", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudieron listar los passkeys.")
		return
	}
	out := make([]webauthnCredentialResponse, 0, len(creds))
	for _, c := range creds {
		out = append(out, toWebAuthnCredentialResponse(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeWebAuthnCredential exige, vía WebAuthnCredentialRepository.DeleteCredential,
// que la credencial pertenezca al usuario autenticado (§198 IDOR).
func (h *Handlers) RevokeWebAuthnCredential(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.WebAuthn.RevokeCredential(r.Context(), id, u.ID); err != nil {
		if errors.Is(err, auth.ErrWebAuthnCredentialNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Passkey no encontrado.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo revocar el passkey.")
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventWebAuthnCredentialRevoked, u.ID, "user", u.ID, security.ClientIP(r, h.TrustedProxies), map[string]any{"credential_id": id})
	w.WriteHeader(http.StatusNoContent)
}
