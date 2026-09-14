package security

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter mantiene un token bucket por clave (normalmente IP, o
// IP+username en login) con expiración de entradas inactivas para no
// crecer sin límite en un proceso de larga duración (§27, §77).
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*visitor
	r        rate.Limit
	burst    int
	ttl      time.Duration
	stop     chan struct{}
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewRateLimiter(perMinute, burst int) *RateLimiter {
	if burst < 1 {
		burst = 1
	}
	rl := &RateLimiter{
		limiters: make(map[string]*visitor),
		r:        rate.Limit(float64(perMinute) / 60.0),
		burst:    burst,
		ttl:      10 * time.Minute,
		stop:     make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Stop detiene la goroutine de limpieza en segundo plano. Los tests deben
// llamarla en un defer para no dejar goroutines colgadas.
func (rl *RateLimiter) Stop() {
	close(rl.stop)
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	v, ok := rl.limiters[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.r, rl.burst)}
		rl.limiters[key] = v
	}
	v.lastSeen = time.Now()
	return v.limiter.Allow()
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			for k, v := range rl.limiters {
				if time.Since(v.lastSeen) > rl.ttl {
					delete(rl.limiters, k)
				}
			}
			rl.mu.Unlock()
		case <-rl.stop:
			return
		}
	}
}

// Middleware aplica el rate limit derivando la clave de cada petición con
// keyFunc (normalmente ClientIP).
func (rl *RateLimiter) Middleware(keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.Allow(keyFunc(r)) {
				writeRateLimitError(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeRateLimitError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    "rate_limited",
			"message": "Demasiadas peticiones, inténtalo de nuevo más tarde.",
		},
	})
}
