// Package server ensambla la aplicación completa: abre la base de datos,
// aplica migraciones, construye repositorios/servicios y expone un único
// http.Handler raíz (§4). Es el único paquete que conoce todos los demás a
// la vez; el resto se comunica solo a través de interfaces.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	apiv1 "github.com/porrii/nexuscloud/internal/api/v1"
	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

// Server agrupa el handler HTTP raíz junto con los recursos que hay que
// cerrar limpiamente al parar (conexión a BD, rate limiters con goroutine
// de limpieza propia).
type Server struct {
	Handler http.Handler
	DB      *sql.DB

	loginLimiter *security.RateLimiter
	apiLimiter   *security.RateLimiter
}

// Build realiza todo el arranque en frío: abrir BD, migrar, construir
// repositorios/servicios/handlers y montar el router. No arranca ningún
// listener de red (eso es responsabilidad del llamador, típicamente
// internal/cli).
func Build(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.EnsureDataDirs(); err != nil {
		return nil, fmt.Errorf("preparando directorios de datos: %w", err)
	}

	sqlDB, err := db.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("abriendo base de datos: %w", err)
	}
	if err := db.Migrate(cfg, sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("aplicando migraciones: %w", err)
	}
	conn := db.Wrap(cfg.Database.Driver, sqlDB)

	userRepo := users.NewSQLRepository(conn)
	sessionRepo := auth.NewSQLSessionRepository(conn)
	invitationRepo := auth.NewSQLInvitationRepository(conn)
	poolRepo := storage.NewSQLPoolRepository(conn)
	fileRepo := storage.NewSQLFileRepository(conn)
	auditRepo := audit.NewSQLRepository(conn)

	pool, err := storage.EnsureDefaultPool(context.Background(), poolRepo, cfg.DefaultStorageDir())
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("preparando storage pool por defecto: %w", err)
	}
	provider, err := storage.NewLocalFilesystemProvider(pool.Path)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("preparando proveedor de almacenamiento: %w", err)
	}

	hasher := auth.NewHasher(cfg.Security.Argon2)
	userSvc := users.NewService(userRepo)
	authenticator := auth.NewAuthenticatorFromConfig(userRepo, sessionRepo, cfg, logger)
	invitationSvc := auth.NewInvitationService(invitationRepo, userSvc, hasher)
	fileSvc := storage.NewFileService(fileRepo, poolRepo, provider)
	auditRecorder := audit.NewRecorder(auditRepo, logger)

	h := &apiv1.Handlers{
		Auth:           authenticator,
		Hasher:         hasher,
		Invitations:    invitationSvc,
		InvitationRepo: invitationRepo,
		SessionRepo:    sessionRepo,
		UserSvc:        userSvc,
		UserRepo:       userRepo,
		Files:          fileSvc,
		AuditLog:       auditRecorder,
		AuditRepo:      auditRepo,
		Logger:         logger,
		TrustedProxies: cfg.Server.TrustedProxies,
	}

	loginBurst := cfg.Security.RateLimit.LoginPerMinute
	loginLimiter := security.NewRateLimiter(cfg.Security.RateLimit.LoginPerMinute, loginBurst)
	apiLimiter := security.NewRateLimiter(cfg.Security.RateLimit.APIPerMinute, cfg.Security.RateLimit.APIPerMinute)

	root := chi.NewRouter()
	root.Use(security.Headers)
	if len(cfg.Security.CORSAllowedOrigins) > 0 {
		root.Use(security.CORS(cfg.Security.CORSAllowedOrigins))
	}

	root.Get("/health", healthHandler())
	root.Get("/ready", readyHandler(sqlDB))

	// El flag web.enabled ya gobierna el montaje del router aunque todavía
	// no haya assets que servir (Fase 2): evita retrofitting del patrón de
	// activación/desactivación más adelante (§3, §47).
	if cfg.API.Enabled {
		root.Mount("/api/v1", apiv1.NewRouter(h, loginLimiter, apiLimiter))
	}
	if cfg.Web.Enabled {
		logger.Warn("web.enabled=true pero la interfaz web todavía no existe en esta fase (Fase 2); no se monta ninguna ruta")
	}

	return &Server{Handler: root, DB: sqlDB, loginLimiter: loginLimiter, apiLimiter: apiLimiter}, nil
}

// Close libera los recursos abiertos por Build: conexión a base de datos y
// las goroutines de limpieza de los rate limiters.
func (s *Server) Close() error {
	s.loginLimiter.Stop()
	s.apiLimiter.Stop()
	return s.DB.Close()
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeHealthJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

// readyHandler comprueba dependencias reales (§111) sin revelar detalles
// internos: solo un estado agregado por dependencia.
func readyHandler(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(ctx); err != nil {
			writeHealthJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "database": "unreachable"})
			return
		}
		writeHealthJSON(w, http.StatusOK, map[string]any{"status": "ready", "database": "ok"})
	}
}

func writeHealthJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
