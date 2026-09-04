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
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
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
	nexuscloudweb "github.com/porrii/nexuscloud/web"
)

// Server agrupa el handler HTTP raíz junto con los recursos que hay que
// cerrar limpiamente al parar (conexión a BD, rate limiters con goroutine
// de limpieza propia).
type Server struct {
	Handler http.Handler
	DB      *sql.DB

	loginLimiter  *security.RateLimiter
	apiLimiter    *security.RateLimiter
	publicLimiter *security.RateLimiter
	stopPurge     chan struct{}
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
	directoryRepo := storage.NewSQLDirectoryRepository(conn)
	versionRepo := storage.NewSQLVersionRepository(conn)
	shareRepo := storage.NewSQLShareRepository(conn)
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
	fileSvc := storage.NewFileService(fileRepo, directoryRepo, versionRepo, shareRepo, poolRepo, provider, hasher,
		cfg.Trash.Enabled, cfg.Versioning.Enabled, cfg.Versioning.MaxVersionsPerFile,
		cfg.Sharing.Enabled, cfg.Sharing.PublicLinksEnabled)
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
	// publicLimiter protege los enlaces públicos de fuerza bruta sobre su
	// contraseña (§37), igual motivo que loginLimiter para /auth/login: es
	// la única superficie de la API que acepta peticiones sin sesión además
	// de login/invitations.
	publicLimiterBurst := cfg.Security.RateLimit.PublicLinkPerMinute
	publicLimiter := security.NewRateLimiter(cfg.Security.RateLimit.PublicLinkPerMinute, publicLimiterBurst)

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
		root.Mount("/api/v1", apiv1.NewRouter(h, loginLimiter, apiLimiter, publicLimiter))
	}
	if cfg.Web.Enabled {
		webHandler, err := newWebUIHandler()
		if err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("preparando interfaz web: %w", err)
		}
		root.Handle("/*", webHandler)
	}

	stopPurge := make(chan struct{})
	if cfg.Trash.Enabled {
		startTrashPurgeLoop(fileSvc, cfg.Trash, logger, stopPurge)
	}

	return &Server{
		Handler: root, DB: sqlDB,
		loginLimiter: loginLimiter, apiLimiter: apiLimiter, publicLimiter: publicLimiter,
		stopPurge: stopPurge,
	}, nil
}

// startTrashPurgeLoop lanza la limpieza automática de la papelera por
// retención (§16 "limpieza automática"). Se ejecuta una vez al arrancar
// (por si el proceso estuvo parado más tiempo del de retención) y luego
// cada hora.
func startTrashPurgeLoop(fileSvc *storage.FileService, cfg config.TrashConfig, logger *slog.Logger, stop <-chan struct{}) {
	retention := time.Duration(cfg.RetentionDays) * 24 * time.Hour

	runOnce := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		files, dirs, err := fileSvc.PurgeExpiredTrash(ctx, retention)
		if err != nil {
			logger.Error("purgando papelera expirada", "error", err)
			return
		}
		if files > 0 || dirs > 0 {
			logger.Info("papelera purgada por retención", "files", files, "directories", dirs, "retention_days", cfg.RetentionDays)
		}
	}

	go func() {
		runOnce()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				runOnce()
			case <-stop:
				return
			}
		}
	}()
}

// Close libera los recursos abiertos por Build: conexión a base de datos y
// las goroutines de limpieza de los rate limiters y de la papelera.
func (s *Server) Close() error {
	s.loginLimiter.Stop()
	s.apiLimiter.Stop()
	s.publicLimiter.Stop()
	if s.stopPurge != nil {
		close(s.stopPurge)
	}
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

// newWebUIHandler sirve los assets embebidos de web/dist (§44, §47). Solo
// se monta cuando web.enabled=true (desactivado por defecto, §3).
func newWebUIHandler() (http.Handler, error) {
	dist, err := fs.Sub(nexuscloudweb.DistFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("preparando assets embebidos: %w", err)
	}
	return spaFileServer(dist), nil
}

// spaFileServer sirve ficheros estáticos y, para cualquier ruta que no
// corresponda a un fichero real embebido, devuelve index.html -- así el
// enrutado del lado cliente (React Router, historial del navegador) se
// resuelve incluso al recargar la página en /account, por ejemplo.
func spaFileServer(distFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(distFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := strings.TrimPrefix(r.URL.Path, "/")
		if upath == "" {
			upath = "."
		}
		if _, err := fs.Stat(distFS, upath); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
