package security

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP resuelve la IP real del cliente. Solo confía en
// X-Forwarded-For/X-Real-IP cuando la conexión TCP inmediata proviene de un
// proxy listado en trustedProxies; en cualquier otro caso usa la IP de la
// conexión TCP directamente, sin fiarse ciegamente de cabeceras que
// cualquier cliente externo podría falsificar (§50, §118, §168 Zero Trust).
func ClientIP(r *http.Request, trustedProxies []string) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}
	if !isTrustedProxy(remoteIP, trustedProxies) {
		return remoteIP
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if first := strings.TrimSpace(parts[0]); first != "" {
			return first
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return remoteIP
}

func isTrustedProxy(ip string, trusted []string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	for _, cidrOrIP := range trusted {
		if cidrOrIP == ip {
			return true
		}
		if _, ipnet, err := net.ParseCIDR(cidrOrIP); err == nil && ipnet.Contains(parsedIP) {
			return true
		}
	}
	return false
}
