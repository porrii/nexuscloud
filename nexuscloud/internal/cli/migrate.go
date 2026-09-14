package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/db"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Gestiona migraciones de base de datos (§8)",
	}
	cmd.AddCommand(newMigrateUpCmd(), newMigrateStatusCmd())
	return cmd
}

func newMigrateUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Aplica las migraciones pendientes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, err := db.Open(cfg)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			if err := db.Migrate(cfg, sqlDB); err != nil {
				return err
			}
			version, _, err := db.Status(cfg, sqlDB)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Migraciones aplicadas. Versión de esquema actual: %d\n", version)
			return nil
		},
	}
}

func newMigrateStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Muestra la versión de esquema actualmente aplicada",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, err := db.Open(cfg)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			version, dirty, err := db.Status(cfg, sqlDB)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Versión de esquema: %d (dirty=%v)\n", version, dirty)
			return nil
		},
	}
}
