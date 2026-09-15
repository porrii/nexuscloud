package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// newAuditCmd expone por CLI el registro de auditoría (§31), hasta ahora
// solo accesible por API (GET /api/v1/audit, admin-only) -- backlog "todo
// por comandos", 2ª tanda (2026-09-15). Sin lógica de negocio que
// replicar: ListEvents ya clampa limit/offset fuera de rango en vez de
// dar error, así que el comando no valida nada por su cuenta.
func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Registro de auditoría (§31)",
	}
	cmd.AddCommand(newAuditListCmd())
	return cmd
}

func newAuditListCmd() *cobra.Command {
	var limit, offset int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista eventos de auditoría (login, altas/bajas, compartición, ...)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, repo, err := openAuditRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			events, err := repo.ListEvents(context.Background(), limit, offset)
			if err != nil {
				return err
			}
			if len(events) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(sin eventos)")
				return nil
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-20s  %-22s  %-36s  %-10s %-36s  %-15s  %s\n",
				"FECHA", "TIPO", "ACTOR", "OBJETIVO", "ID OBJETIVO", "IP", "DETALLE")
			for _, e := range events {
				fmt.Fprintf(out, "%-20s  %-22s  %-36s  %-10s %-36s  %-15s  %v\n",
					e.OccurredAt.Local().Format("2006-01-02 15:04:05"), e.EventType, e.ActorUserID,
					e.TargetType, e.TargetID, e.IP, e.Metadata)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 100, "máximo de eventos a devolver (se ajusta automáticamente si es <= 0 o > 500)")
	cmd.Flags().IntVar(&offset, "offset", 0, "cuántos eventos más recientes saltar (paginación)")
	return cmd
}
