package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/porrii/nexuscloud/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Gestiona la configuración de esta instancia",
	}
	cmd.AddCommand(newConfigInitCmd(), newConfigValidateCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Genera un config.yaml con los valores seguros por defecto (§100, §140)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := os.Stat(out); err == nil {
				return fmt.Errorf("%s ya existe; bórralo o usa --out para otra ruta si quieres regenerarlo", out)
			}

			data, err := yaml.Marshal(config.Defaults())
			if err != nil {
				return fmt.Errorf("serializando configuración por defecto: %w", err)
			}
			header := "# Configuración de NexusCloud generada por 'nexuscloud config init'.\n" +
				"# Ver config.example.yaml en la raíz del repositorio para una versión comentada campo a campo.\n" +
				"# Revisa docs/security.md antes de exponer esta instancia a Internet.\n\n"

			if err := os.WriteFile(out, append([]byte(header), data...), 0o640); err != nil {
				return fmt.Errorf("escribiendo %s: %w", out, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configuración creada en %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "config.yaml", "ruta de salida")
	return cmd
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Valida el fichero de configuración actual (§152)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK: configuración válida (configVersion=%d, db=%s, puerto=%d)\n",
				cfg.ConfigVersion, cfg.Database.Driver, cfg.Server.Port)
			return nil
		},
	}
}
