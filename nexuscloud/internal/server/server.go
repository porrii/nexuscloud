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
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	apiv1 "github.com/porrii/nexuscloud/internal/api/v1"
	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/backup"
	"github.com/porrii/nexuscloud/internal/clientupdates"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
	"github.com/porrii/nexuscloud/internal/webdav"
	nexuscloudweb "github.com/porrii/nexuscloud/web"
)

// Server agrupa el handler HTTP raíz junto con los recursos que hay que
// cerrar limpiamente al parar (conexión a BD, rate limiters con goroutine
// de limpieza propia).
type Server struct {
	Handler http.Handler
	DB      *sql.DB

	loginLimiter           *security.RateLimiter
	apiLimiter             *security.RateLimiter
	publicLimiter          *security.RateLimiter
	anonymousUploadLimiter *security.RateLimiter
	webdavLimiter          *security.RateLimiter // nil si webdav.enabled=false
	stopPurge              chan struct{}
	stopBackup             chan struct{}
}

// Build realiza todo el arranque en frío: abrir BD, migrar, construir
// repositorios/servicios/handlers y montar el router. No arranca ningún
// listener de red (eso es responsabilidad del llamador, típicamente
// internal/cli).
func Build(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.EnsureDataDirs(); err != nil {
		return nil, fmt.Errorf("preparando directorios de datos: %w", err)
	}

	// Layout resuelve las raíces de almacenamiento (absolutas, symlinks
	// resueltos) en un único sitio; es lo que consumen el provider y, en el
	// futuro, los pools adicionales y las miniaturas/versionado.
	layout, err := storage.NewLayout(storage.LayoutParams{
		UserData:   cfg.DefaultStorageDir(),
		Thumbnails: cfg.ThumbnailsDir(),
		Versions:   cfg.VersionsDir(),
		Temp:       cfg.TempDir(),
	})
	if err != nil {
		return nil, fmt.Errorf("resolviendo rutas de almacenamiento: %w", err)
	}
	logger.Info("rutas de almacenamiento",
		"userData", layout.UserData,
		"thumbnails", layout.Thumbnails,
		"versions", layout.Versions,
		"temp", layout.Temp,
		"database", cfg.DatabaseDir())

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
	apiTokenRepo := auth.NewSQLAPITokenRepository(conn)
	invitationRepo := auth.NewSQLInvitationRepository(conn)
	webauthnCredRepo := auth.NewSQLWebAuthnCredentialRepository(conn)
	webauthnCeremonyRepo := auth.NewSQLWebAuthnCeremonyRepository(conn)
	poolRepo := storage.NewSQLPoolRepository(conn)
	fileRepo := storage.NewSQLFileRepository(conn)
	directoryRepo := storage.NewSQLDirectoryRepository(conn)
	versionRepo := storage.NewSQLVersionRepository(conn)
	shareRepo := storage.NewSQLShareRepository(conn)
	auditRepo := audit.NewSQLRepository(conn)

	defaultPool, err := storage.EnsureDefaultPool(context.Background(), poolRepo, layout.UserData)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("preparando storage pool por defecto: %w", err)
	}
	logger.Info("storage pool por defecto", "id", defaultPool.ID, "name", defaultPool.Name, "path", defaultPool.Path)

	// El I/O físico de cada fichero va al Provider de SU pool (files.pool_id),
	// resuelto y cacheado por poolID. Con un solo pool el comportamiento es
	// idéntico al anterior (un único Provider).
	providers := storage.NewPoolProviderResolver(poolRepo)

	hasher := auth.NewHasher(cfg.Security.Argon2)
	userSvc := users.NewService(userRepo, users.WithDefaultQuota(cfg.Storage.DefaultQuotaBytes))
	authenticator := auth.NewAuthenticatorFromConfig(userRepo, sessionRepo, webauthnCredRepo, cfg, logger)
	// APITokens (§78, ADR-037): siempre disponible, sin opción de config que
	// lo desactive -- mismo criterio que las sesiones, no el de WebDAV.
	apiTokenSvc := auth.NewAPITokenService(apiTokenRepo, userRepo, logger)
	invitationSvc := auth.NewInvitationService(invitationRepo, userSvc, hasher)
	fileSvc := storage.NewFileService(fileRepo, directoryRepo, versionRepo, shareRepo, poolRepo, providers, hasher,
		cfg.Trash.Enabled, cfg.Versioning.Enabled, cfg.Versioning.MaxVersionsPerFile,
		cfg.Versioning.MaxVersionAgeDays, cfg.Versioning.MaxVersionsTotalSizeBytes,
		cfg.Sharing.Enabled, cfg.Sharing.PublicLinksEnabled,
		storage.WithQuotas(userSvc, storage.NewSQLUsageRepository(conn)),
		// Favoritos (§87, ADR-038): siempre disponibles, sin opción de config.
		storage.WithFavorites(storage.NewSQLFavoriteRepository(conn)),
		// Subida anónima (§38, ADR-039): el repositorio se conecta siempre;
		// sharing.anonymousUploadEnabled es el interruptor real, comprobado
		// dentro de FileService en cada operación que lo necesita.
		storage.WithAnonymousUploads(storage.NewSQLAnonymousUploadRepository(conn), cfg.Sharing.AnonymousUploadEnabled))
	auditRecorder := audit.NewRecorder(auditRepo, logger)
	backupRepo := backup.NewSQLRepository(conn)
	// backupManager lo consume el bucle automático de más abajo (ADR-016);
	// backupRepo también lo usan los endpoints HTTP de backups entrantes
	// (ADR-029, ver apiv1.Handlers.BackupRepo más abajo) -- backup manual
	// desde CLI sigue sin endpoint HTTP/UI propio, igual que en el Slice 1.
	backupManager := backup.NewManager(poolRepo, fileRepo, providers, backupRepo, fileSvc, cfg.Security.Argon2)

	// clientUpdatesProxy (ADR-032): solo se construye si clientUpdates.enabled
	// = true. El token de GitHub nunca vive en config.yaml, mismo criterio
	// exacto que NEXUSCLOUD_BACKUP_RECEIVE_TOKEN un poco más abajo -- con
	// clientUpdates.enabled=false (por defecto), queda en nil y NewRouter ni
	// registra esas rutas.
	var clientUpdatesProxy *clientupdates.Proxy
	if cfg.ClientUpdates.Enabled {
		clientUpdatesProxy = clientupdates.NewProxy(
			cfg.ClientUpdates.GithubRepo,
			cfg.ClientUpdates.Channel,
			os.Getenv("NEXUSCLOUD_CLIENT_UPDATES_GITHUB_TOKEN"),
		)
	}

	// webauthnSvc (§25, ADR-033): solo se construye si
	// security.webAuthn.enabled = true -- config.Validate ya exige RPID/
	// RPOrigin no vacíos en ese caso, así que webauthn.New (dentro de
	// NewWebAuthnService) nunca falla aquí por config incompleta. Con
	// enabled=false (por defecto), queda en nil y NewRouter ni registra
	// esas rutas, mismo criterio exacto que clientUpdatesProxy.
	var webauthnSvc *auth.WebAuthnService
	if cfg.Security.WebAuthn.Enabled {
		webauthnSvc, err = auth.NewWebAuthnService(cfg.Security.WebAuthn, webauthnCredRepo, webauthnCeremonyRepo, userRepo)
		if err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("configurando WebAuthn: %w", err)
		}
	}

	// webdavTokens/webdavHandler (§43, ADR-034): solo existen si
	// webdav.enabled = true. Con false (por defecto) no se construye nada ni
	// se monta ninguna ruta -- mismo criterio que webauthnSvc/clientUpdatesProxy.
	var (
		webdavTokens  *webdav.TokenService
		webdavHandler http.Handler
		webdavLimiter *security.RateLimiter
	)
	if cfg.WebDAV.Enabled {
		webdavTokens = webdav.NewTokenService(webdav.NewSQLTokenRepository(conn), userRepo, logger)
		webdavHandler = webdav.NewHandler(fileSvc, webdavTokens, auditRecorder, webdav.Options{
			Prefix:             cfg.WebDAV.Path,
			ReadOnly:           cfg.WebDAV.ReadOnly,
			MaxUploadSizeBytes: cfg.WebDAV.MaxUploadSizeBytes,
			TrustedProxies:     cfg.Server.TrustedProxies,
		}, logger)
		webdavLimiter = security.NewRateLimiter(cfg.Security.RateLimit.WebDAVPerMinute, cfg.Security.RateLimit.WebDAVPerMinute)
	}

	h := &apiv1.Handlers{
		Auth:           authenticator,
		Hasher:         hasher,
		Invitations:    invitationSvc,
		InvitationRepo: invitationRepo,
		SessionRepo:    sessionRepo,
		APITokens:      apiTokenSvc,
		UserSvc:        userSvc,
		UserRepo:       userRepo,
		Files:          fileSvc,
		AuditLog:       auditRecorder,
		AuditRepo:      auditRepo,
		Logger:         logger,
		TrustedProxies: cfg.Server.TrustedProxies,
		// BackupsDir/BackupRepo/BackupReceiveToken (ADR-029): habilitan
		// que ESTA instancia reciba backups de otro servidor NexusCloud.
		// El token nunca vive en config.yaml (mismo criterio que
		// NEXUSCLOUD_BACKUP_PASSPHRASE, ADR-028) -- con la variable vacía
		// (por defecto), NewRouter ni registra esas rutas.
		BackupsDir:         cfg.BackupsDir(),
		BackupRepo:         backupRepo,
		BackupReceiveToken: os.Getenv("NEXUSCLOUD_BACKUP_RECEIVE_TOKEN"),
		ClientUpdatesProxy: clientUpdatesProxy,
		WebAuthn:           webauthnSvc,
		WebDAVTokens:       webdavTokens,
		WebDAVPath:         cfg.WebDAV.Path,
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
	// anonymousUploadLimiter (§38, ADR-039): cubo propio, más estricto que
	// publicLimiter -- aquí cualquiera con el enlace escribe sin que el
	// creador haya podido vetar a nadie de antemano.
	anonymousUploadBurst := cfg.Security.RateLimit.AnonymousUploadPerMinute
	anonymousUploadLimiter := security.NewRateLimiter(cfg.Security.RateLimit.AnonymousUploadPerMinute, anonymousUploadBurst)

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
		root.Mount("/api/v1", apiv1.NewRouter(h, loginLimiter, apiLimiter, publicLimiter, anonymousUploadLimiter))
	}
	if webdavHandler != nil {
		// Su propio rate limit por IP, más holgado que el de la API: un
		// cliente WebDAV dispara decenas de PROPFIND por segundo (§43).
		webdavKey := func(r *http.Request) string { return security.ClientIP(r, cfg.Server.TrustedProxies) }
		root.Mount(cfg.WebDAV.Path, webdavLimiter.Middleware(webdavKey)(webdavHandler))
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

	stopBackup := make(chan struct{})
	if cfg.Backup.Enabled {
		// La passphrase (ADR-028) y el token remoto (ADR-029) se leen UNA
		// VEZ aquí, al arrancar el servidor -- el bucle automático no
		// tiene terminal para pedirlos interactivamente, así que la única
		// vía es la variable de entorno, leída antes de que el proceso
		// pueda haber perdido de vista el entorno con el que arrancó.
		startBackupScheduleLoop(backupManager, cfg.Backup, cfg.BackupsDir(),
			os.Getenv("NEXUSCLOUD_BACKUP_PASSPHRASE"), os.Getenv("NEXUSCLOUD_BACKUP_REMOTE_TOKEN"), logger, stopBackup)
	}

	return &Server{
		Handler: root, DB: sqlDB,
		loginLimiter: loginLimiter, apiLimiter: apiLimiter, publicLimiter: publicLimiter,
		anonymousUploadLimiter: anonymousUploadLimiter, webdavLimiter: webdavLimiter,
		stopPurge: stopPurge, stopBackup: stopBackup,
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
		files, dirs, err := fileSvc.PurgeExpiredTrash(ctx, retention, cfg.MaxTotalSizeBytes)
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

// startBackupScheduleLoop lanza el backup automático (§18 "programación",
// ADR-016). A diferencia de startTrashPurgeLoop, NO se ejecuta una vez al
// arrancar: purgar la papelera es barato e idempotente, pero lanzar un
// backup completo en cada arranque del servidor podría ser costoso y
// sorprendente si el proceso se reinicia con frecuencia (p.ej. durante
// actualizaciones) -- el primer backup automático llega tras el primer
// intervalo completo; quien quiera uno inmediato ya tiene "nexuscloud backup
// run" a mano.
func startBackupScheduleLoop(manager *backup.Manager, cfg config.BackupConfig, destDir, passphrase, remoteToken string, logger *slog.Logger, stop <-chan struct{}) {
	interval := time.Duration(cfg.IntervalMinutes) * time.Minute
	// RemoteDestination (ADR-029) vacío = comportamiento de siempre
	// (destDir local); no vacío sustituye el destino por otro servidor
	// NexusCloud, mismo criterio que "--dest https://..." en el CLI.
	if cfg.RemoteDestination != "" {
		destDir = cfg.RemoteDestination
	}

	runOnce := func() {
		// 30 minutos, no el time.Minute de la purga: un backup completo de un
		// árbol grande puede tardar bastante más que purgar filas expiradas.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		job, err := manager.Run(ctx, backup.RunOptions{
			DestinationPath: destDir, RetentionCount: cfg.RetentionCount, RetentionDays: cfg.RetentionDays,
			Incremental: cfg.Incremental, Encrypt: cfg.Encrypt, Passphrase: passphrase, RemoteToken: remoteToken,
		})
		if err != nil {
			logger.Error("backup automático falló", "error", err)
			return
		}
		logger.Info("backup automático completado", "job_id", job.ID, "files", job.FileCount, "bytes", job.TotalBytes)
	}

	go func() {
		ticker := time.NewTicker(interval)
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
// las goroutines de limpieza de los rate limiters, la papelera y el backup
// automático.
func (s *Server) Close() error {
	s.loginLimiter.Stop()
	s.apiLimiter.Stop()
	s.publicLimiter.Stop()
	s.anonymousUploadLimiter.Stop()
	if s.webdavLimiter != nil {
		s.webdavLimiter.Stop()
	}
	if s.stopPurge != nil {
		close(s.stopPurge)
	}
	if s.stopBackup != nil {
		close(s.stopBackup)
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
