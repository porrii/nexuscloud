package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/users"
)

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Gestión de usuarios (§20-21)",
	}
	cmd.AddCommand(newUsersListCmd(), newUsersCreateCmd(), newUsersDisableCmd())
	return cmd
}

func newUsersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista los usuarios existentes",
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

			list, err := userRepo.ListUsers(context.Background())
			if err != nil {
				return err
			}
			for _, u := range list {
				fmt.Fprintf(cmd.OutOrStdout(), "%-36s  %-20s  %-10s  %s\n", u.ID, u.Username, u.Status, u.DisplayName)
			}
			return nil
		},
	}
}

func newUsersCreateCmd() *cobra.Command {
	var (
		username string
		role     string
		password string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Crea un usuario (rol 'user' por defecto)",
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

			u, err := userSvc.CreateUser(context.Background(), users.CreateUserInput{
				Username:     username,
				PasswordHash: hash,
				Role:         role,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Usuario %q creado (id=%s)\n", u.Username, u.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "nombre de usuario (obligatorio)")
	cmd.Flags().StringVar(&role, "role", "", "rol (por defecto: user)")
	cmd.Flags().StringVar(&password, "password", "", "contraseña (si se omite, se solicita de forma interactiva sin eco)")
	return cmd
}

func newUsersDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <username>",
		Short: "Deshabilita un usuario",
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
			userSvc := users.NewService(userRepo)

			u, err := userRepo.GetUserByUsername(context.Background(), args[0])
			if err != nil {
				return err
			}
			if err := userSvc.Disable(context.Background(), u.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Usuario %q deshabilitado\n", u.Username)
			return nil
		},
	}
}
