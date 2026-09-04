package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/logging"
	"github.com/porrii/nexuscloud/internal/server"
	"github.com/porrii/nexuscloud/internal/version"
)

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Arranca el servidor NexusCloud",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			logger, closer, err := logging.New(cfg.Logging)
			if err != nil {
				return err
			}
			defer closer.Close()

			logger.Info("arrancando NexusCloud", "version", version.Version)

			srv, err := server.Build(cfg, logger)
			if err != nil {
				return fmt.Errorf("construyendo el servidor: %w", err)
			}
			defer srv.Close()

			addr := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
			httpServer := &http.Server{
				Addr:              addr,
				Handler:           srv.Handler,
				ReadHeaderTimeout: 10 * time.Second,
			}

			errCh := make(chan error, 1)
			go func() {
				logger.Info("escuchando", "addr", addr, "tls", cfg.Server.TLSCertFile != "")
				var serveErr error
				if cfg.Server.TLSCertFile != "" {
					serveErr = httpServer.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
				} else {
					serveErr = httpServer.ListenAndServe()
				}
				if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					errCh <- serveErr
				}
			}()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			select {
			case err := <-errCh:
				return fmt.Errorf("error del servidor HTTP: %w", err)
			case <-ctx.Done():
				logger.Info("señal de apagado recibida, cerrando de forma ordenada")
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("apagando el servidor HTTP: %w", err)
			}
			logger.Info("NexusCloud detenido correctamente")
			return nil
		},
	}
}
