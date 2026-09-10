// Package cli implementa el árbol de subcomandos del binario nexuscloud
// (§98) usando cobra. Cada subcomando carga su propia configuración de
// forma independiente en vez de compartir estado global mutable.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/config"
)

var cfgFile string

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "nexuscloud",
		Short:         "NexusCloud — plataforma de almacenamiento self-hosted",
		Long:          "NexusCloud es una plataforma de almacenamiento en nube privada, autoalojada, modular, multiplataforma y segura.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&cfgFile, "config", "", "ruta al fichero config.yaml (por defecto: ./config.yaml si existe)")

	root.AddCommand(
		newVersionCmd(),
		newConfigCmd(),
		newDoctorCmd(),
		newStartCmd(),
		newServiceCmd(),
		newMigrateCmd(),
		newAdminCmd(),
		newUsersCmd(),
		newStorageCmd(),
		newBackupCmd(),
		newSecurityCmd(),
	)
	return root
}

// Execute es el punto de entrada invocado desde cmd/nexuscloud/main.go.
func Execute() error {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return err
	}
	return nil
}

// loadConfig carga la configuración usando --config si se dio, o
// "./config.yaml" si existe. Que no exista fichero no es un error: Load cae
// a defaults + variables de entorno.
func loadConfig() (*config.Config, error) {
	path := cfgFile
	if path == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			path = "config.yaml"
		}
	}
	return config.Load(path)
}
