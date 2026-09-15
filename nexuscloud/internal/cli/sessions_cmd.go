package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// newSessionsCmd expone por CLI la gestión de sesiones activas de OTRO
// usuario (§26) -- hasta ahora solo existía "MIS sesiones" vía sesión
// HTTP (/auth/sessions), sin ninguna vía de que un administrador local
// gestionara las de otra persona. ListSessionsForUser/RevokeSession ya
// aceptan el userID como parámetro explícito (nunca lo sacan de una
// sesión HTTP), así que --username + resolveOwnerID basta -- backlog
// "todo por comandos", 2ª tanda (2026-09-15).
func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Sesiones activas de un usuario (§26)",
	}
	cmd.AddCommand(newSessionsListCmd(), newSessionsRevokeCmd())
	return cmd
}

func newSessionsListCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista las sesiones de un usuario (activas, caducadas y revocadas)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, sessRepo, userRepo, err := openSessionRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			sessions, err := sessRepo.ListSessionsForUser(cmd.Context(), ownerID)
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(sin sesiones)")
				return nil
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-36s  %-20s  %-15s  %-20s  %-20s  %s\n",
				"ID", "DISPOSITIVO", "IP", "CREADA", "ÚLTIMA ACTIVIDAD", "ESTADO")
			now := time.Now()
			for _, s := range sessions {
				status := "activa"
				switch {
				case s.RevokedAt != nil:
					status = "revocada"
				case !s.IsValid(now):
					status = "caducada"
				}
				fmt.Fprintf(out, "%-36s  %-20s  %-15s  %-20s  %-20s  %s\n",
					s.ID, s.Device, s.IP, s.CreatedAt.Local().Format("2006-01-02 15:04:05"),
					s.LastSeenAt.Local().Format("2006-01-02 15:04:05"), status)
			}
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newSessionsRevokeCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "revoke <session-id>",
		Short: "Revoca una sesión de un usuario (el id lo da \"sessions list\")",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, sessRepo, userRepo, err := openSessionRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			if err := sessRepo.RevokeSession(cmd.Context(), args[0], ownerID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Sesión %s revocada.\n", args[0])
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}
