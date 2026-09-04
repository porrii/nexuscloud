package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestHeadersSetsDefensiveHeadersButNotHSTSOverPlainHTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	Headers(okHandler()).ServeHTTP(rr, req)

	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("falta X-Content-Type-Options: nosniff")
	}
	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("falta X-Frame-Options: DENY")
	}
	if rr.Header().Get("Strict-Transport-Security") != "" {
		t.Error("no debería enviarse HSTS sobre una petición HTTP sin TLS (§66)")
	}
}

func TestCORSAllowsListedOriginOnly(t *testing.T) {
	mw := CORS([]string{"https://cloud.example.com"})

	reqAllowed := httptest.NewRequest(http.MethodGet, "/", nil)
	reqAllowed.Header.Set("Origin", "https://cloud.example.com")
	rrAllowed := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rrAllowed, reqAllowed)
	if got := rrAllowed.Header().Get("Access-Control-Allow-Origin"); got != "https://cloud.example.com" {
		t.Errorf("origen permitido: Access-Control-Allow-Origin = %q", got)
	}

	reqDenied := httptest.NewRequest(http.MethodGet, "/", nil)
	reqDenied.Header.Set("Origin", "https://evil.example.com")
	rrDenied := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rrDenied, reqDenied)
	if got := rrDenied.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("un origen no listado nunca debería reflejarse: got %q", got)
	}
}

func TestRateLimiterBlocksAfterBurstExhausted(t *testing.T) {
	rl := NewRateLimiter(60, 2) // 2 peticiones de ráfaga, 1/seg de recarga
	defer rl.Stop()

	if !rl.Allow("1.2.3.4") {
		t.Error("primera petición debería permitirse")
	}
	if !rl.Allow("1.2.3.4") {
		t.Error("segunda petición (dentro del burst) debería permitirse")
	}
	if rl.Allow("1.2.3.4") {
		t.Error("tercera petición inmediata debería bloquearse (burst agotado)")
	}
}

func TestRateLimiterTracksKeysIndependently(t *testing.T) {
	rl := NewRateLimiter(60, 1)
	defer rl.Stop()

	if !rl.Allow("ip-a") {
		t.Error("ip-a debería permitirse la primera vez")
	}
	if !rl.Allow("ip-b") {
		t.Error("ip-b no debería verse afectada por el consumo de ip-a")
	}
}

func TestClientIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	got := ClientIP(req, []string{"127.0.0.1"}) // el proxy de confianza no coincide con el peer real
	if got != "203.0.113.9" {
		t.Errorf("ClientIP = %q, esperado la IP TCP real (203.0.113.9) al no confiar en el peer", got)
	}
}

func TestClientIPHonorsForwardedHeaderFromTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")

	got := ClientIP(req, []string{"127.0.0.1"})
	if got != "198.51.100.7" {
		t.Errorf("ClientIP = %q, esperado 198.51.100.7 (primer salto de XFF) desde un proxy de confianza", got)
	}
}
