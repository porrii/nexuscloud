package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/logging"
)

// serviceProgram implementa service.Interface: el gestor de servicios del
// SO (systemd, Windows SCM, launchd) llama a Start/Stop. Start debe volver
// enseguida; el trabajo real bloquea en run().
type serviceProgram struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	runErr error
}

func (p *serviceProgram) Start(_ service.Service) error {
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.done = make(chan struct{})
	go p.run()
	return nil
}

func (p *serviceProgram) run() {
	defer close(p.done)
	cfg, err := loadConfig()
	if err != nil {
		p.runErr = err
		return
	}
	logger, closer, err := logging.New(cfg.Logging)
	if err != nil {
		p.runErr = err
		return
	}
	defer closer.Close()
	p.runErr = RunServer(p.ctx, cfg, logger)
}

func (p *serviceProgram) Stop(_ service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
	return p.runErr
}

// buildService construye el objeto service.Service. Para `install` hace
// falta la ruta ABSOLUTA del config.yaml (se hornea en la definición del
// servicio); para el resto de acciones no se usa.
func buildService(configPathForInstall string) (service.Service, *serviceProgram, error) {
	args := []string{"service", "run"}
	if configPathForInstall != "" {
		args = append(args, "--config", configPathForInstall)
	}
	cfg := &service.Config{
		Name:        "nexuscloud",
		DisplayName: "NexusCloud",
		Description: "Plataforma de almacenamiento privada self-hosted (NexusCloud).",
		Arguments:   args,
	}
	prg := &serviceProgram{}
	s, err := service.New(prg, cfg)
	return s, prg, err
}

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Instala y controla NexusCloud como servicio del sistema (systemd / Windows / launchd)",
		Long: "Gestiona NexusCloud como servicio nativo del sistema operativo, sin depender de Docker.\n" +
			"install/uninstall requieren privilegios de administrador. En Linux con systemd ya\n" +
			"existe además una unit de ejemplo en deploy/systemd/nexuscloud.service.",
	}
	cmd.AddCommand(
		newServiceInstallCmd(),
		newServiceControlCmd("uninstall", "Desinstala el servicio", (service.Service).Uninstall),
		newServiceControlCmd("start", "Arranca el servicio", (service.Service).Start),
		newServiceControlCmd("stop", "Detiene el servicio", (service.Service).Stop),
		newServiceControlCmd("restart", "Reinicia el servicio", (service.Service).Restart),
		newServiceStatusCmd(),
		newServiceRunCmd(),
	)
	return cmd
}

func newServiceInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Instala el servicio (requiere administrador)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := cfgFile
			if path == "" {
				return errors.New(
					"especifica --config con la ruta ABSOLUTA del config.yaml que usará el servicio\n" +
						"(p.ej. nexuscloud service install --config /etc/nexuscloud/config.yaml)")
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			s, _, err := buildService(abs)
			if err != nil {
				return err
			}
			if err := s.Install(); err != nil {
				return fmt.Errorf("instalando el servicio: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Servicio 'nexuscloud' instalado (config: %s). Arráncalo con: nexuscloud service start\n", abs)
			return nil
		},
	}
}

func newServiceControlCmd(use, short string, action func(service.Service) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, _, err := buildService("")
			if err != nil {
				return err
			}
			if err := action(s); err != nil {
				return fmt.Errorf("%s del servicio: %w", use, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Servicio 'nexuscloud': %s OK\n", use)
			return nil
		},
	}
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Muestra el estado del servicio",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, _, err := buildService("")
			if err != nil {
				return err
			}
			st, err := s.Status()
			if err != nil {
				if errors.Is(err, service.ErrNotInstalled) {
					fmt.Fprintln(cmd.OutOrStdout(), "no instalado")
					return nil
				}
				return fmt.Errorf(
					"no se pudo consultar el estado del servicio (¿hay un gestor de servicios como systemd disponible?): %w", err)
			}
			switch st {
			case service.StatusRunning:
				fmt.Fprintln(cmd.OutOrStdout(), "en ejecución")
			case service.StatusStopped:
				fmt.Fprintln(cmd.OutOrStdout(), "parado")
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "desconocido")
			}
			return nil
		},
	}
}

// newServiceRunCmd es lo que invoca el gestor de servicios del SO; no está
// pensado para lanzarse a mano (para eso está `nexuscloud start`).
func newServiceRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "run",
		Short:  "Ejecuta NexusCloud bajo el control del gestor de servicios (uso interno)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, _, err := buildService("")
			if err != nil {
				return err
			}
			return s.Run()
		},
	}
}
