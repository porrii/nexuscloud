package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/users"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Operaciones de administrador",
	}
	cmd.AddCommand(newAdminCreateUserCmd())
	return cmd
}

func newAdminCreateUserCmd() *cobra.Command {
	var (
		username string
		role     string
		password string
	)
	cmd := &cobra.Command{
		Use:   "create-user",
		Short: "Crea un usuario administrador (§140: nunca admin/admin)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" {
				return fmt.Errorf("--username es obligatorio")
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()
			userSvc := users.NewService(userRepo)

			ctx := context.Background()
			if role == "" {
				isFirst, err := userSvc.IsFirstUser(ctx)
				if err != nil {
					return err
				}
				if isFirst {
					role = users.RoleSuperAdmin
				} else {
					role = users.RoleAdministrator
				}
			}

			if password == "" {
				password, err = promptPassword(cmd)
				if err != nil {
					return err
				}
			}
			if len(password) < 8 {
				return fmt.Errorf("la contraseña debe tener al menos 8 caracteres")
			}

			hasher := auth.NewHasher(cfg.Security.Argon2)
			hash, err := hasher.Hash(password)
			if err != nil {
				return err
			}

			u, err := userSvc.CreateUser(ctx, users.CreateUserInput{
				Username:     username,
				PasswordHash: hash,
				Role:         role,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Usuario %q creado con rol %q (id=%s)\n", u.Username, role, u.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "nombre de usuario (obligatorio)")
	cmd.Flags().StringVar(&role, "role", "", "rol a asignar (por defecto: super_admin si es el primer usuario, si no administrator)")
	cmd.Flags().StringVar(&password, "password", "", "contraseña (si se omite, se solicita de forma interactiva sin eco)")
	return cmd
}
