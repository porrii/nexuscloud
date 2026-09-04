// Package security implementa controles transversales de la cadena HTTP:
// cabeceras defensivas, CORS estricto, rate limiting y resolución de IP de
// cliente consciente de proxies de confianza (§27, §50, §69, §118, §191).
package security

import "net/http"

// Headers añade cabeceras de seguridad estándar a toda respuesta (§191).
// HSTS solo se añade cuando la petición ya llegó por TLS, para no romper
// instalaciones LAN sin HTTPS (§66).
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
