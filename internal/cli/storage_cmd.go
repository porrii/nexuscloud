package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/storage/diskinfo"
)

func newStorageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Gestión del almacenamiento: discos y Storage Pools",
	}
	cmd.AddCommand(
		newStorageDisksCmd(),
		newStoragePoolListCmd(),
		newStoragePoolAddCmd(),
		newStoragePoolStatusCmd("enable", "active", "Activa un Storage Pool"),
		newStoragePoolStatusCmd("disable", "disabled", "Desactiva un Storage Pool"),
		newStoragePoolSetPolicyCmd(),
		newStoragePoolRemoveCmd(),
	)
	return cmd
}

// openPoolRepo abre la BD configurada (migrando si migrate=true), garantiza
// que exista el pool por defecto (igual que hace el servidor al arrancar,
// para que los comandos funcionen también en una instancia recién migrada
// que aún no se ha arrancado nunca) y devuelve la conexión cruda (para
// cerrarla) y un repositorio de pools.
func openPoolRepo(cfg *config.Config, migrate bool) (*sql.DB, *db.Conn, *storage.SQLPoolRepository, error) {
	sqlDB, err := db.Open(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if migrate {
		if err := db.Migrate(cfg, sqlDB); err != nil {
			sqlDB.Close()
			return nil, nil, nil, err
		}
	}
	conn := db.Wrap(cfg.Database.Driver, sqlDB)
	pools := storage.NewSQLPoolRepository(conn)
	if _, err := storage.EnsureDefaultPool(context.Background(), pools, cfg.DefaultStorageDir()); err != nil {
		sqlDB.Close()
		return nil, nil, nil, err
	}
	return sqlDB, conn, pools, nil
}

// --- disks (Fase C) -------------------------------------------------------

func newStorageDisksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disks",
		Short: "Lista las unidades de almacenamiento detectadas en este equipo",
		RunE: func(cmd *cobra.Command, args []string) error {
			disks, err := diskinfo.Enumerate(cmd.Context())
			if errors.Is(err, diskinfo.ErrUnsupported) {
				fmt.Fprintln(cmd.OutOrStdout(),
					"La enumeración de discos no está soportada en este sistema operativo todavía.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("enumerando discos: %w", err)
			}
			if len(disks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No se detectó ninguna unidad de almacenamiento.")
				return nil
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-24s %-8s %10s %10s %10s  %s\n",
				"PUNTO DE MONTAJE", "FS", "TOTAL", "USADO", "LIBRE", "ESTADO")
			for _, d := range disks {
				estado := "rw"
				if d.ReadOnly {
					estado = "solo lectura"
				}
				fs := d.Filesystem
				if fs == "" {
					fs = "-"
				}
				fmt.Fprintf(out, "%-24s %-8s %10s %10s %10s  %s\n",
					truncate(d.MountPoint, 24), truncate(fs, 8),
					humanBytes(d.TotalBytes), humanBytes(d.UsedBytes), humanBytes(d.FreeBytes),
					estado)
			}
			return nil
		},
	}
}

// --- pools --------------------------------------------------------------

func newStoragePoolListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista los Storage Pools configurados",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, conn, pools, err := openPoolRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			list, err := pools.ListPools(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-36s  %-14s %-8s %-11s %5s %8s  %s\n",
				"ID", "NOMBRE", "ESTADO", "UTILIZACIÓN", "PRIO", "FICHEROS", "RUTA")
			for _, p := range list {
				var n int
				_ = conn.QueryRowContext(cmd.Context(),
					"SELECT COUNT(*) FROM files WHERE pool_id = ?", p.ID).Scan(&n)
				fmt.Fprintf(out, "%-36s  %-14s %-8s %-11s %5d %8d  %s\n",
					p.ID, truncate(p.Name, 14), p.Status,
					p.UtilizationPolicy, p.Priority, n, p.Path)
			}
			return nil
		},
	}
}

func newStoragePoolAddCmd() *cobra.Command {
	var name, path string
	var priority int
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Añade un Storage Pool local",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" || path == "" {
				return errors.New("--name y --path son obligatorios")
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolviendo --path: %w", err)
			}
			if err := os.MkdirAll(abs, 0o750); err != nil {
				return fmt.Errorf("creando %s: %w", abs, err)
			}
			testFile := filepath.Join(abs, ".nexuscloud-add-write-test")
			if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
				return fmt.Errorf("la ruta %s no es escribible: %w", abs, err)
			}
			_ = os.Remove(testFile)

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, _, pools, err := openPoolRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			p := &storage.Pool{
				ID:        idgen.New(),
				Name:      name,
				Type:      "local",
				Path:      abs,
				Priority:  priority,
				Status:    "active",
				CreatedAt: time.Now().UTC(),
			}
			if err := pools.CreatePool(cmd.Context(), p); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Storage Pool creado: %s (%s) en %s\n", p.Name, p.ID, p.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "nombre del pool")
	cmd.Flags().StringVar(&path, "path", "", "ruta raíz del pool (se crea si no existe)")
	cmd.Flags().IntVar(&priority, "priority", 100, "prioridad (menor = se usa antes); el pool 'default' es 0")
	return cmd
}

func newStoragePoolStatusCmd(use, status, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, _, pools, err := openPoolRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			id, err := resolvePoolRef(cmd.Context(), pools, args[0])
			if err != nil {
				return err
			}
			if status == "disabled" {
				if err := ensureNotLastActivePool(cmd.Context(), pools, id); err != nil {
					return err
				}
			}
			if err := pools.SetPoolStatus(cmd.Context(), id, status); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pool %s -> %s\n", id, status)
			return nil
		},
	}
}

// resolvePoolRef acepta el id exacto de un pool o su nombre (que es UNIQUE
// en el esquema) y devuelve el id real. Hace los comandos de gestión
// usables sin copiar UUIDs.
func resolvePoolRef(ctx context.Context, pools storage.PoolRepository, ref string) (string, error) {
	if _, err := pools.GetPoolByID(ctx, ref); err == nil {
		return ref, nil
	}
	list, err := pools.ListPools(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range list {
		if p.Name == ref {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("no hay ningún storage pool con id o nombre %q", ref)
}

func ensureNotLastActivePool(ctx context.Context, pools storage.PoolRepository, id string) error {
	list, err := pools.ListPools(ctx)
	if err != nil {
		return err
	}
	active := 0
	for _, p := range list {
		if p.Status == "active" {
			active++
		}
	}
	target, err := pools.GetPoolByID(ctx, id)
	if err != nil {
		return err
	}
	if target.Status == "active" && active <= 1 {
		return storage.ErrLastActivePool
	}
	return nil
}

func newStoragePoolSetPolicyCmd() *cobra.Command {
	var utilization, backup, versioning, snapshot string
	cmd := &cobra.Command{
		Use:   "set-policy <id>",
		Short: "Cambia las políticas de un Storage Pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, _, pools, err := openPoolRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			id, err := resolvePoolRef(cmd.Context(), pools, args[0])
			if err != nil {
				return err
			}
			p, err := pools.GetPoolByID(cmd.Context(), id)
			if err != nil {
				return err
			}
			if utilization != "" {
				p.UtilizationPolicy = utilization
			}
			if backup != "" {
				p.BackupPolicy = backup
			}
			if versioning != "" {
				p.VersioningPolicy = versioning
			}
			if snapshot != "" {
				p.SnapshotPolicy = snapshot
			}
			if err := pools.UpdatePool(cmd.Context(), p); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Pool %s: utilization=%s backup=%s versioning=%s snapshot=%s\n",
				p.ID, p.UtilizationPolicy, p.BackupPolicy, p.VersioningPolicy, p.SnapshotPolicy)
			return nil
		},
	}
	cmd.Flags().StringVar(&utilization, "utilization", "", "fill | round-robin | manual")
	cmd.Flags().StringVar(&backup, "backup", "", "inherit | on | off")
	cmd.Flags().StringVar(&versioning, "versioning", "", "inherit | on | off")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "none")
	return cmd
}

func newStoragePoolRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Borra un Storage Pool (falla si tiene archivos o carpetas)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, _, pools, err := openPoolRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			id, err := resolvePoolRef(cmd.Context(), pools, args[0])
			if err != nil {
				return err
			}
			if err := pools.DeletePool(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Storage Pool %s borrado\n", id)
			return nil
		},
	}
}

// --- helpers de formato ------------------------------------------------

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
