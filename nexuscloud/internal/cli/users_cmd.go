package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Gestión de usuarios (§20-21)",
	}
	cmd.AddCommand(newUsersListCmd(), newUsersCreateCmd(), newUsersDisableCmd(), newUsersGroupCmd(), newUsersTotpCmd())
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

// newUsersGroupCmd arregla el hueco encontrado en la auditoría "todo por
// comandos" (2026-09-15): CreateGroup/AddUserToGroup existían en el
// repositorio desde antes, pero ni la web ni el CLI los exponían -- la
// única vía era un INSERT SQL a mano. Mismo patrón que el resto de "users".
func newUsersGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Gestión de grupos (§22)",
	}
	cmd.AddCommand(newUsersGroupCreateCmd(), newUsersGroupAddMemberCmd())
	return cmd
}

func newUsersGroupCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <nombre>",
		Short: "Crea un grupo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			g := &users.Group{ID: idgen.New(), Name: args[0], CreatedAt: time.Now().UTC()}
			if err := userRepo.CreateGroup(context.Background(), g); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Grupo %q creado (id=%s)\n", g.Name, g.ID)
			return nil
		},
	}
}

func newUsersGroupAddMemberCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-member <usuario> <grupo>",
		Short: "Añade un usuario a un grupo",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			userID, err := resolveOwnerID(context.Background(), userRepo, args[0])
			if err != nil {
				return err
			}
			group, err := userRepo.GetGroupByName(context.Background(), args[1])
			if err != nil {
				return fmt.Errorf("grupo %q no encontrado: %w", args[1], err)
			}
			if err := userRepo.AddUserToGroup(context.Background(), userID, group.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Usuario %q añadido al grupo %q\n", args[0], args[1])
			return nil
		},
	}
}

// newUsersTotpCmd expone por CLI el doble factor (§25), implementado en la
// API (auth.GenerateTOTP/ValidateTOTP, EnrollTOTP/VerifyTOTP en
// auth_handlers.go) desde antes pero sin ninguna vía de administrarlo fuera
// de la web -- en un servidor headless no había manera de activarlo.
//
// El flujo se divide en enroll/verify (no en un solo paso) por la misma
// razón que ya documenta EnrollTOTP: el secreto NO se persiste hasta que se
// confirma con un código real generado por la app autenticadora, para no
// poder dejar a un usuario bloqueado con un secreto que nunca llegó a
// registrar. Como no hay sesión entre dos invocaciones separadas del
// binario, verify recibe el secreto por --secret (el mismo que imprimió
// enroll) en vez de asumir que quedó guardado en algún sitio intermedio.
//
// disable es la vía de recuperación si un usuario queda bloqueado (secreto
// mal copiado, app autenticadora perdida, etc.) -- sin él, activar 2FA por
// primera vez en una instalación de un solo admin sin interfaz gráfica no
// tendría vuelta atrás si algo sale mal.
func newUsersTotpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "totp",
		Short: "Doble factor (TOTP, §25)",
	}
	cmd.AddCommand(newUsersTotpEnrollCmd(), newUsersTotpVerifyCmd(), newUsersTotpDisableCmd())
	return cmd
}

func newUsersTotpEnrollCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Genera un secreto TOTP nuevo (no lo activa todavía)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" {
				return fmt.Errorf("--username es obligatorio")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			u, err := userRepo.GetUserByUsername(context.Background(), username)
			if err != nil {
				return fmt.Errorf("usuario %q no encontrado: %w", username, err)
			}

			secret, url, err := auth.GenerateTOTP("NexusCloud", u.Username)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Secreto: %s\n", secret)
			fmt.Fprintf(out, "URL (pégala en tu app autenticadora, o genera un QR con ella): %s\n", url)
			fmt.Fprintln(out, "\nTodavía NO está activo. Genera un código con tu app y confirma con:")
			fmt.Fprintf(out, "  nexuscloud users totp verify --username %s --secret %s <código>\n", username, secret)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
	return cmd
}

func newUsersTotpVerifyCmd() *cobra.Command {
	var username, secret string
	cmd := &cobra.Command{
		Use:   "verify <código>",
		Short: "Confirma un secreto generado con \"totp enroll\" y activa 2FA",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" {
				return fmt.Errorf("--username es obligatorio")
			}
			if secret == "" {
				return fmt.Errorf("--secret es obligatorio (el mismo que imprimió \"totp enroll\")")
			}
			if !auth.ValidateTOTP(args[0], secret) {
				return fmt.Errorf("el código no coincide con el secreto")
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			u, err := userRepo.GetUserByUsername(context.Background(), username)
			if err != nil {
				return fmt.Errorf("usuario %q no encontrado: %w", username, err)
			}
			u.TOTPSecret = secret
			u.UpdatedAt = time.Now().UTC()
			if err := userRepo.UpdateUser(context.Background(), u); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "2FA activado para %q: a partir de ahora el login exigirá también el código TOTP.\n", username)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
	cmd.Flags().StringVar(&secret, "secret", "", "el mismo secreto que imprimió \"totp enroll\" (obligatorio)")
	return cmd
}

func newUsersTotpDisableCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Desactiva 2FA de un usuario (recupera el acceso si se ha quedado bloqueado)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" {
				return fmt.Errorf("--username es obligatorio")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, userRepo, err := openUsersRepo(cfg, false)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			u, err := userRepo.GetUserByUsername(context.Background(), username)
			if err != nil {
				return fmt.Errorf("usuario %q no encontrado: %w", username, err)
			}
			u.TOTPSecret = ""
			u.UpdatedAt = time.Now().UTC()
			if err := userRepo.UpdateUser(context.Background(), u); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "2FA desactivado para %q.\n", username)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
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
