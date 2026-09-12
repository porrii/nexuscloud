package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/backup"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/storage"
)

// newBackupCmd implementa el Backup Manager real (§18, ADR-015),
// reemplazando el stub reservado desde la Fase 1. Alcance de este slice:
// backup manual y completo a una carpeta local, con verificación de
// integridad y restauración a una carpeta elegida -- ver ADR-015 para lo
// que queda fuera (incremental, programación, cifrado, RAID, snapshots).
func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup Manager: backup manual, listar y restaurar (§18)",
	}
	cmd.AddCommand(newBackupRunCmd(), newBackupListCmd(), newBackupRestoreCmd())
	return cmd
}

// openBackupManager reutiliza openPoolRepo (misma BD, mismo pool por
// defecto ya garantizado) y construye un backup.Manager real encima. pools
// se devuelve también para poder resolver --pool (id o nombre) sin abrir
// una segunda conexión.
func openBackupManager(cfg *config.Config, migrate bool) (*sql.DB, *storage.SQLPoolRepository, *backup.Manager, error) {
	sqlDB, conn, pools, err := openPoolRepo(cfg, migrate)
	if err != nil {
		return nil, nil, nil, err
	}
	resolver := storage.NewPoolProviderResolver(pools)
	files := storage.NewSQLFileRepository(conn)
	manager := backup.NewManager(pools, files, resolver, backup.NewSQLRepository(conn))
	return sqlDB, pools, manager, nil
}

// ensureWritableDir crea dest si no existe y comprueba que se puede
// escribir en ella -- mismo patrón que newStoragePoolAddCmd.
func ensureWritableDir(dest string) (string, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("resolviendo --dest: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return "", fmt.Errorf("creando %s: %w", abs, err)
	}
	testFile := filepath.Join(abs, ".nexuscloud-backup-write-test")
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return "", fmt.Errorf("la ruta %s no es escribible: %w", abs, err)
	}
	_ = os.Remove(testFile)
	return abs, nil
}

func newBackupRunCmd() *cobra.Command {
	var dest string
	var poolRefs []string
	var keepLast, keepDays int
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Ejecuta un backup manual y completo",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if dest == "" {
				dest = cfg.BackupsDir()
			}
			absDest, err := ensureWritableDir(dest)
			if err != nil {
				return err
			}

			sqlDB, pools, manager, err := openBackupManager(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			// El Backup Manager solo trabaja con ids ya resueltos (ver
			// backup.RunOptions.PoolIDs) -- traducir --pool (id o nombre) es
			// responsabilidad de este comando, con el mismo resolvePoolRef
			// que ya usan "storage pool <verbo> <id>".
			poolIDs := make([]string, 0, len(poolRefs))
			for _, ref := range poolRefs {
				id, err := resolvePoolRef(cmd.Context(), pools, ref)
				if err != nil {
					return err
				}
				poolIDs = append(poolIDs, id)
			}

			job, err := manager.Run(cmd.Context(), backup.RunOptions{
				PoolIDs: poolIDs, DestinationPath: absDest, RetentionCount: keepLast, RetentionDays: keepDays,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup %s completado: %d archivos, %s, en %s\n",
				job.ID, job.FileCount, humanBytes(uint64(job.TotalBytes)), filepath.Join(absDest, job.ID))
			return nil
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "carpeta de destino (por defecto: la carpeta de backups de esta instancia)")
	cmd.Flags().StringArrayVar(&poolRefs, "pool", nil, "id o nombre de un storage pool a respaldar (repetible; por defecto, todos los elegibles)")
	cmd.Flags().IntVar(&keepLast, "keep-last", 0, "conserva solo los N backups completados más recientes en --dest tras este run (0 = sin límite)")
	cmd.Flags().IntVar(&keepDays, "keep-days", 0, "conserva solo los backups completados de los últimos N días en --dest tras este run (0 = sin límite)")
	return cmd
}

func newBackupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista los backups realizados",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, _, manager, err := openBackupManager(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			jobs, err := manager.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(jobs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Todavía no se ha realizado ningún backup.")
				return nil
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-36s  %-10s %-20s %8s %10s  %s\n",
				"ID", "ESTADO", "FECHA", "ARCHIVOS", "TAMAÑO", "DESTINO")
			for _, j := range jobs {
				fmt.Fprintf(out, "%-36s  %-10s %-20s %8d %10s  %s\n",
					j.ID, j.Status, j.StartedAt.Local().Format("2006-01-02 15:04:05"),
					j.FileCount, humanBytes(uint64(j.TotalBytes)), j.DestinationPath)
				if j.Status == backup.StatusFailed && j.ErrorMessage != "" {
					fmt.Fprintf(out, "    error: %s\n", j.ErrorMessage)
				}
			}
			return nil
		},
	}
}

func newBackupRestoreCmd() *cobra.Command {
	var dest string
	cmd := &cobra.Command{
		Use:   "restore <job-id>",
		Short: "Restaura un backup a una carpeta elegida (no reinserta en un pool activo)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dest == "" {
				return errors.New("--dest es obligatorio: indica la carpeta donde extraer el backup")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			absDest, err := ensureWritableDir(dest)
			if err != nil {
				return err
			}
			sqlDB, _, manager, err := openBackupManager(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			if err := manager.Restore(cmd.Context(), backup.RestoreOptions{JobID: args[0], DestinationPath: absDest}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup %s restaurado en %s\n", args[0], absDest)
			return nil
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "carpeta donde extraer el backup (obligatorio)")
	return cmd
}
