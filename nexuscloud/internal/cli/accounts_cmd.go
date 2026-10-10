package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/accountadmin"
	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/users"
	"github.com/porrii/nexuscloud/internal/webdav"
)

// Administración de cuentas desde la CLI (ADR-042, B1). La CLI es local y
// no está sujeta a las reglas entre administradores (actor vacío), pero usa
// los MISMOS servicios que la API (Decisión 4): users.Service para roles y
// grupos, y accountadmin.Service para contraseña, 2FA, passkeys y tokens.

// openAccountAdmin construye accountadmin.Service sobre la conexión de
// openUsersRepo.
func openAccountAdmin(cfg *config.Config, sqlDB *sql.DB, userRepo users.Repository) *accountadmin.Service {
	conn := db.Wrap(cfg.Database.Driver, sqlDB)
	return accountadmin.New(
		users.NewService(userRepo), userRepo, auth.NewHasher(cfg.Security.Argon2),
		auth.NewSQLSessionRepository(conn), auth.NewSQLAPITokenRepository(conn),
		webdav.NewSQLTokenRepository(conn), auth.NewSQLWebAuthnCredentialRepository(conn),
	)
}

func newUsersSetRoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-role <usuario> <rol>",
		Short: "Cambia el rol de un usuario (super_admin, administrator, user o read_only)",
		Long: "Deja al usuario con un único rol (ADR-042). Nunca deja la instancia sin ningún\n" +
			"superadministrador activo, y no pasa una cuenta a read_only mientras tenga enlaces\n" +
			"públicos, de subida anónima o comparticiones con subida vigentes.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// FileService hace falta para contar lo publicado antes de pasar
			// una cuenta a read_only.
			sqlDB, fileSvc, userRepo, err := openFileService(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ctx := context.Background()
			u, err := userRepo.GetUserByUsername(ctx, args[0])
			if err != nil {
				return fmt.Errorf("usuario %q no encontrado: %w", args[0], err)
			}
			svc := users.NewService(userRepo, users.WithPublicationCounter(fileSvc))
			previous, err := svc.SetRole(ctx, "", u.ID, args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rol de %q: %s → %s.\n", args[0], previous, args[1])
			return nil
		},
	}
}

func newUsersResetPasswordCmd() *cobra.Command {
	var password string
	cmd := &cobra.Command{
		Use:   "reset-password <usuario>",
		Short: "Fija una contraseña nueva y revoca todas las sesiones y tokens del usuario",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ctx := context.Background()
			u, err := userRepo.GetUserByUsername(ctx, args[0])
			if err != nil {
				return fmt.Errorf("usuario %q no encontrado: %w", args[0], err)
			}
			if password == "" {
				password, err = promptPassword(cmd)
				if err != nil {
					return err
				}
			}
			revoked, err := openAccountAdmin(cfg, sqlDB, userRepo).ResetPassword(ctx, "", u.ID, password)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Contraseña de %q cambiada. Sesiones cerradas; tokens revocados: %d de API y %d WebDAV.\n",
				args[0], revoked.APITokens, revoked.WebDAVTokens)
			return nil
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "contraseña nueva (si se omite, se solicita de forma interactiva sin eco)")
	return cmd
}

// --- Grupos (ADR-042 Decisión 11) ------------------------------------------

func newUsersGroupMembersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "members <grupo>",
		Short: "Lista los miembros de un grupo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ctx := context.Background()
			g, err := userRepo.GetGroupByName(ctx, args[0])
			if err != nil {
				return fmt.Errorf("grupo %q no encontrado: %w", args[0], err)
			}
			members, err := users.NewService(userRepo).ListGroupMembers(ctx, g.ID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(members) == 0 {
				fmt.Fprintf(out, "El grupo %q no tiene miembros.\n", args[0])
				return nil
			}
			for _, m := range members {
				fmt.Fprintf(out, "%-32s  %s\n", m.Username, m.DisplayName)
			}
			return nil
		},
	}
}

func newUsersGroupRemoveMemberCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-member <usuario> <grupo>",
		Short: "Saca a un usuario de un grupo",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ctx := context.Background()
			u, err := userRepo.GetUserByUsername(ctx, args[0])
			if err != nil {
				return fmt.Errorf("usuario %q no encontrado: %w", args[0], err)
			}
			g, err := userRepo.GetGroupByName(ctx, args[1])
			if err != nil {
				return fmt.Errorf("grupo %q no encontrado: %w", args[1], err)
			}
			if err := users.NewService(userRepo).RemoveGroupMember(ctx, g.ID, u.ID); err != nil {
				if errors.Is(err, users.ErrNotFound) {
					return fmt.Errorf("%q no es miembro de %q", args[0], args[1])
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%q ya no es miembro de %q.\n", args[0], args[1])
			return nil
		},
	}
}

func newUsersGroupRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <grupo> <nombre-nuevo>",
		Short: "Cambia el nombre de un grupo",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ctx := context.Background()
			g, err := userRepo.GetGroupByName(ctx, args[0])
			if err != nil {
				return fmt.Errorf("grupo %q no encontrado: %w", args[0], err)
			}
			if err := users.NewService(userRepo).RenameGroup(ctx, g.ID, args[1]); err != nil {
				if errors.Is(err, users.ErrAlreadyExists) {
					return fmt.Errorf("ya existe un grupo llamado %q", args[1])
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Grupo %q renombrado a %q.\n", args[0], args[1])
			return nil
		},
	}
}

func newUsersGroupDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <grupo>",
		Short: "Borra un grupo, sus membresías y las comparticiones dirigidas a él",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, fileSvc, userRepo, err := openFileService(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ctx := context.Background()
			g, err := userRepo.GetGroupByName(ctx, args[0])
			if err != nil {
				return fmt.Errorf("grupo %q no encontrado: %w", args[0], err)
			}
			shares, err := fileSvc.CountActiveSharesForGroup(ctx, g.ID)
			if err != nil {
				return err
			}
			if !yes {
				return fmt.Errorf("borrar %q se llevará %d comparticiones dirigidas a él; repite con --yes para confirmarlo", args[0], shares)
			}
			if err := users.NewService(userRepo).DeleteGroup(ctx, g.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Grupo %q borrado (%d comparticiones eliminadas).\n", args[0], shares)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirma el borrado")
	return cmd
}
