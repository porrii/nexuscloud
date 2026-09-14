package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/auth"
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
	cmd.AddCommand(newBackupRunCmd(), newBackupListCmd(), newBackupRestoreCmd(), newBackupRestoreToPoolCmd(), newBackupVerifyCmd())
	return cmd
}

// openBackupManager reutiliza openPoolRepo (misma BD, mismo pool por
// defecto ya garantizado) y construye un backup.Manager real encima. pools
// se devuelve también para poder resolver --pool (id o nombre) sin abrir
// una segunda conexión.
//
// Construye un FileService completo (no solo files/resolver como antes de
// ADR-025) porque RestoreToPool reinserta ficheros pasando por
// FileService.Upload/Mkdir -- necesita las mismas piezas que ya monta
// internal/server.Build para la API HTTP, con los mismos flags de
// trash/versioning/sharing ya cargados desde cfg, para que una restauración
// a pool se comporte exactamente igual (versionado incluido) que subir un
// archivo con el servidor corriendo.
func openBackupManager(cfg *config.Config, migrate bool) (*sql.DB, *storage.SQLPoolRepository, *backup.Manager, error) {
	sqlDB, conn, pools, err := openPoolRepo(cfg, migrate)
	if err != nil {
		return nil, nil, nil, err
	}
	resolver := storage.NewPoolProviderResolver(pools)
	files := storage.NewSQLFileRepository(conn)
	directories := storage.NewSQLDirectoryRepository(conn)
	versions := storage.NewSQLVersionRepository(conn)
	shares := storage.NewSQLShareRepository(conn)
	hasher := auth.NewHasher(cfg.Security.Argon2)
	fileSvc := storage.NewFileService(files, directories, versions, shares, pools, resolver, hasher,
		cfg.Trash.Enabled, cfg.Versioning.Enabled, cfg.Versioning.MaxVersionsPerFile,
		cfg.Versioning.MaxVersionAgeDays, cfg.Versioning.MaxVersionsTotalSizeBytes,
		cfg.Sharing.Enabled, cfg.Sharing.PublicLinksEnabled)
	manager := backup.NewManager(pools, files, resolver, backup.NewSQLRepository(conn), fileSvc, cfg.Security.Argon2)
	return sqlDB, pools, manager, nil
}

// backupPassphraseFromEnv (ADR-028): única vía para suministrar la
// passphrase de backup, tanto en estos comandos manuales como en el modo
// automático (internal/server.Build) -- nunca por flag (quedaría en el
// historial de shell y en `ps aux`), nunca por prompt interactivo (el modo
// automático no tiene terminal, y una sola vía que sirva para los dos
// casos es más simple que dos).
func backupPassphraseFromEnv() string {
	return os.Getenv("NEXUSCLOUD_BACKUP_PASSPHRASE")
}

// backupRemoteTokenFromEnv (ADR-029): mismo criterio exacto que
// backupPassphraseFromEnv -- nunca por flag, un secreto no debe pasar por
// argv ni quedar en el historial de shell. Se lee sin más si el backup no
// es remoto (RunOptions.RemoteToken/RestoreOptions.RemoteToken/etc. lo
// ignoran sin más cuando el destino es local).
func backupRemoteTokenFromEnv() string {
	return os.Getenv("NEXUSCLOUD_BACKUP_REMOTE_TOKEN")
}

// isRemoteDestination (ADR-029): mismo criterio que
// backup.resolveDestination (no exportado, así que se repite aquí la
// única línea que hace falta) -- "http://"/"https://" es un destino
// remoto, cualquier otra cosa es una carpeta local.
func isRemoteDestination(dest string) bool {
	return strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://")
}

// ensureWritableDir crea dest si no existe y comprueba que se puede
// escribir en ella -- mismo patrón que newStoragePoolAddCmd. Nunca se
// llama con un destino remoto (ver newBackupRunCmd).
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
	var incremental, encrypt bool
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
			// Un destino remoto (ADR-029) no es una carpeta de ESTA
			// máquina -- ensureWritableDir (filepath.Abs + MkdirAll +
			// prueba de escritura local) no tiene sentido para una URL,
			// así que se salta por completo: absDest queda tal cual la
			// URL dada.
			absDest := dest
			if !isRemoteDestination(dest) {
				absDest, err = ensureWritableDir(dest)
				if err != nil {
					return err
				}
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
				Incremental: incremental, Encrypt: encrypt, Passphrase: backupPassphraseFromEnv(),
				RemoteToken: backupRemoteTokenFromEnv(),
			})
			if err != nil {
				return err
			}
			location := filepath.Join(absDest, job.ID)
			if isRemoteDestination(absDest) {
				location = absDest + "/" + job.ID
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup %s completado: %d archivos, %s, en %s\n",
				job.ID, job.FileCount, humanBytes(uint64(job.TotalBytes)), location)
			return nil
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "carpeta de destino, o https://otro-servidor:puerto para respaldar a otro NexusCloud (ADR-029, exige NEXUSCLOUD_BACKUP_REMOTE_TOKEN); por defecto, la carpeta de backups de esta instancia")
	cmd.Flags().StringArrayVar(&poolRefs, "pool", nil, "id o nombre de un storage pool a respaldar (repetible; por defecto, todos los elegibles)")
	cmd.Flags().IntVar(&keepLast, "keep-last", 0, "conserva solo los N backups completados más recientes en --dest tras este run (0 = sin límite)")
	cmd.Flags().IntVar(&keepDays, "keep-days", 0, "conserva solo los backups completados de los últimos N días en --dest tras este run (0 = sin límite)")
	cmd.Flags().BoolVar(&incremental, "incremental", false, "enlaza (hardlink) al backup anterior en --dest cualquier fichero sin cambios en vez de recopiarlo (ADR-026)")
	cmd.Flags().BoolVar(&encrypt, "encrypt", false, "cifra cada fichero con AES-256-CTR; exige NEXUSCLOUD_BACKUP_PASSPHRASE en el entorno (ADR-028)")
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

			if err := manager.Restore(cmd.Context(), backup.RestoreOptions{
				JobID: args[0], DestinationPath: absDest,
				Passphrase: backupPassphraseFromEnv(), RemoteToken: backupRemoteTokenFromEnv(),
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup %s restaurado en %s\n", args[0], absDest)
			return nil
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "carpeta donde extraer el backup (obligatorio)")
	return cmd
}

func newBackupRestoreToPoolCmd() *cobra.Command {
	var poolRef string
	cmd := &cobra.Command{
		Use:   "restore-to-pool <job-id>",
		Short: "Reinserta un backup como archivos activos en un pool (ADR-025)",
		Long: "A diferencia de \"restore\" (extrae a una carpeta, sin tocar la base de datos), reinserta\n" +
			"cada archivo del backup como un archivo activo normal -- navegable, descargable,\n" +
			"versionable -- en el pool indicado. El pool destino es siempre explícito: no tiene por\n" +
			"qué ser el pool original del backup, precisamente para poder recuperar datos cuando\n" +
			"ese pool original ya no existe o está inactivo.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if poolRef == "" {
				return errors.New("--pool es obligatorio: indica el pool activo (id o nombre) donde reinsertar el backup")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, pools, manager, err := openBackupManager(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			poolID, err := resolvePoolRef(cmd.Context(), pools, poolRef)
			if err != nil {
				return err
			}
			result, err := manager.RestoreToPool(cmd.Context(), backup.RestoreToPoolOptions{
				JobID: args[0], PoolID: poolID,
				Passphrase: backupPassphraseFromEnv(), RemoteToken: backupRemoteTokenFromEnv(),
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, e := range result.Errors {
				fmt.Fprintf(out, "MAL: %s\n", e)
			}
			fmt.Fprintf(out, "Restaurados: %d\n", result.Restored)
			if len(result.Errors) > 0 {
				return fmt.Errorf("%d archivo(s) no se pudieron restaurar", len(result.Errors))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&poolRef, "pool", "", "pool activo destino, id o nombre (obligatorio)")
	return cmd
}

func newBackupVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <job-id>",
		Short: "Recalcula el SHA-256 de cada fichero de un backup ya hecho, sin restaurarlo",
		Args:  cobra.ExactArgs(1),
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

			result, err := manager.Verify(cmd.Context(), backup.VerifyOptions{
				JobID: args[0], Passphrase: backupPassphraseFromEnv(), RemoteToken: backupRemoteTokenFromEnv(),
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, fr := range result.Files {
				status := "OK"
				if !fr.OK {
					status = "MAL: " + fr.Error
				}
				fmt.Fprintf(out, "%-8s %q%s\n", status, fr.PoolName, filepath.Join(fr.ParentPath, fr.Name))
			}
			if !result.OK {
				return fmt.Errorf("backup %s: al menos un fichero no verificó correctamente", args[0])
			}
			fmt.Fprintf(out, "Backup %s: %d archivos verificados correctamente\n", args[0], len(result.Files))
			return nil
		},
	}
}
