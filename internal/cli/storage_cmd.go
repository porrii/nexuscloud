package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/storage/diskinfo"
)

func newStorageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Gestión del almacenamiento: discos y Storage Pools",
	}
	cmd.AddCommand(
		newStorageDisksCmd(),
		notImplementedYet("list", "Lista Storage Pools", "Fase D del plan de almacenamiento"),
		notImplementedYet("add", "Añade un Storage Pool", "Fase D del plan de almacenamiento"),
	)
	return cmd
}

// newStorageDisksCmd enumera, en solo lectura, las unidades de
// almacenamiento que ve el sistema operativo (§11 / requisito 3). No
// necesita base de datos ni configuración: es un diagnóstico del host.
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
