package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
	"github.com/porrii/nexuscloud/internal/webdav"
)

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Gestión de usuarios (§20-21)",
	}
	cmd.AddCommand(newUsersListCmd(), newUsersCreateCmd(), newUsersDisableCmd(), newUsersEnableCmd(),
		newUsersEditCmd(), newUsersDeleteCmd(), newUsersGroupCmd(), newUsersQuotaCmd(), newUsersTotpCmd(), newUsersWebauthnCmd(),
		newUsersWebdavTokenCmd(), newUsersAPITokenCmd(), newUsersInvitationCmd())
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
		quota    string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Crea un usuario (rol 'user' por defecto)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" {
				return fmt.Errorf("--username es obligatorio")
			}
			var quotaBytes *int64
			if cmd.Flags().Changed("quota") {
				var err error
				if quotaBytes, err = parseQuotaFlag(quota); err != nil {
					return err
				}
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
				QuotaBytes:   quotaBytes,
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
	cmd.Flags().StringVar(&quota, "quota", "", "cuota de almacenamiento: un tamaño (100GB), unlimited o inherit (por defecto hereda la del grupo o la global)")
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
	cmd.AddCommand(newUsersGroupCreateCmd(), newUsersGroupEditCmd(), newUsersGroupAddMemberCmd())
	return cmd
}

func newUsersGroupCreateCmd() *cobra.Command {
	var quota string
	cmd := &cobra.Command{
		Use:   "create <nombre>",
		Short: "Crea un grupo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var quotaBytes *int64
			if cmd.Flags().Changed("quota") {
				var err error
				if quotaBytes, err = parseQuotaFlag(quota); err != nil {
					return err
				}
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

			g := &users.Group{ID: idgen.New(), Name: args[0], QuotaBytes: quotaBytes, CreatedAt: time.Now().UTC()}
			if err := userRepo.CreateGroup(context.Background(), g); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Grupo %q creado (id=%s)\n", g.Name, g.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&quota, "quota", "", "cuota por miembro: un tamaño (500GB), unlimited o inherit (por defecto el grupo no aporta cuota)")
	return cmd
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

// openWebAuthnCredRepo reutiliza la misma conexión que openUsersRepo en
// vez de abrir la base de datos dos veces -- db.Wrap solo añade la
// reescritura de placeholders (Rebind), no abre nada nuevo.
func openWebAuthnCredRepo(cfg *config.Config, sqlDB *sql.DB) auth.WebAuthnCredentialRepository {
	return auth.NewSQLWebAuthnCredentialRepository(db.Wrap(cfg.Database.Driver, sqlDB))
}

func newUsersWebauthnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webauthn",
		Short: "Passkeys / WebAuthn (§25, ADR-033)",
	}
	cmd.AddCommand(newUsersWebauthnListCmd(), newUsersWebauthnRevokeCmd())
	return cmd
}

func newUsersWebauthnListCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los passkeys registrados de un usuario",
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
			creds, err := openWebAuthnCredRepo(cfg, sqlDB).ListCredentialsForUser(context.Background(), u.ID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(creds) == 0 {
				fmt.Fprintf(out, "%q no tiene passkeys registrados.\n", username)
				return nil
			}
			for _, c := range creds {
				lastUsed := "nunca"
				if c.LastUsedAt != nil {
					lastUsed = c.LastUsedAt.Format(time.RFC3339)
				}
				fmt.Fprintf(out, "%-36s  %-20s  creado %s  último uso %s\n", c.ID, c.Label, c.CreatedAt.Format(time.RFC3339), lastUsed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
	return cmd
}

func newUsersWebauthnRevokeCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "revoke <credential-id>",
		Short: "Revoca un passkey (recupera el acceso si se ha quedado bloqueado)",
		Args:  cobra.ExactArgs(1),
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
			// DeleteCredential exige coincidencia de userID (§198 IDOR):
			// --username no es solo cosmético, evita revocar por error el
			// passkey de otro usuario aunque se acierte el ID.
			if err := openWebAuthnCredRepo(cfg, sqlDB).DeleteCredential(context.Background(), args[0], u.ID); err != nil {
				if errors.Is(err, auth.ErrWebAuthnCredentialNotFound) {
					return fmt.Errorf("passkey %q no encontrado para %q", args[0], username)
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Passkey %q revocado para %q.\n", args[0], username)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
	return cmd
}

// openWebDAVTokenService reutiliza la conexión de openUsersRepo, igual que
// openWebAuthnCredRepo.
func openWebDAVTokenService(cfg *config.Config, sqlDB *sql.DB, userRepo users.Repository) *webdav.TokenService {
	return webdav.NewTokenService(webdav.NewSQLTokenRepository(db.Wrap(cfg.Database.Driver, sqlDB)), userRepo, nil)
}

// newUsersWebdavTokenCmd gestiona los tokens de acceso WebDAV (§43, ADR-034):
// la "contraseña" que usan los clientes WebDAV por HTTP Basic. A diferencia
// de los passkeys, un token no necesita navegador, así que aquí SÍ se puede
// crear (un servidor sin interfaz web, la instalación por defecto, no tendría
// otra forma de dar acceso).
func newUsersWebdavTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webdav-token",
		Short: "Tokens de acceso WebDAV (§43, ADR-034)",
	}
	cmd.AddCommand(newUsersWebdavTokenCreateCmd(), newUsersWebdavTokenListCmd(), newUsersWebdavTokenRevokeCmd())
	return cmd
}

func newUsersWebdavTokenCreateCmd() *cobra.Command {
	var username, label string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Crea un token de acceso WebDAV (se muestra una sola vez)",
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
			tok, plain, err := openWebDAVTokenService(cfg, sqlDB, userRepo).Create(context.Background(), u.ID, label)
			if err != nil {
				if errors.Is(err, webdav.ErrInvalidLabel) {
					return fmt.Errorf("--label es demasiado largo (máximo 100 caracteres)")
				}
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Token de acceso WebDAV %q creado para %q.\n\n", tok.Label, username)
			fmt.Fprintln(out, "Cópialo ahora: NO se vuelve a mostrar (solo se guarda su huella).")
			fmt.Fprintf(out, "\n  %s\n\n", plain)
			fmt.Fprintf(out, "En tu cliente WebDAV usa %q como usuario y este token como CONTRASEÑA, contra https://<tu-servidor>%s/\n", username, cfg.WebDAV.Path)
			if !cfg.WebDAV.Enabled {
				fmt.Fprintln(out, "\nAviso: webdav.enabled=false en esta configuración -- el token no servirá hasta que actives WebDAV (ver docs/webdav.md).")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
	cmd.Flags().StringVar(&label, "label", "", `nombre para reconocer el token, p.ej. "portátil de casa" (por defecto "WebDAV")`)
	return cmd
}

func newUsersWebdavTokenListCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los tokens de acceso WebDAV de un usuario",
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
			tokens, err := openWebDAVTokenService(cfg, sqlDB, userRepo).List(context.Background(), u.ID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(tokens) == 0 {
				fmt.Fprintf(out, "%q no tiene tokens de acceso WebDAV.\n", username)
				return nil
			}
			for _, t := range tokens {
				lastUsed := "nunca"
				if t.LastUsedAt != nil {
					lastUsed = t.LastUsedAt.Format(time.RFC3339)
				}
				fmt.Fprintf(out, "%-36s  %-20s  creado %s  último uso %s\n", t.ID, t.Label, t.CreatedAt.Format(time.RFC3339), lastUsed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
	return cmd
}

func newUsersWebdavTokenRevokeCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "revoke <token-id>",
		Short: "Revoca un token de acceso WebDAV (el cliente que lo usa deja de tener acceso)",
		Args:  cobra.ExactArgs(1),
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
			// Revoke exige coincidencia de userID (§198 IDOR): --username no
			// es solo cosmético, evita revocar el token de otro usuario aunque
			// se acierte el ID.
			if err := openWebDAVTokenService(cfg, sqlDB, userRepo).Revoke(context.Background(), args[0], u.ID); err != nil {
				if errors.Is(err, webdav.ErrTokenNotFound) {
					return fmt.Errorf("token %q no encontrado para %q", args[0], username)
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Token %q revocado para %q.\n", args[0], username)
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

// newUsersEnableCmd/newUsersEditCmd/newUsersDeleteCmd cierran el hueco
// encontrado en la auditoría "todo por comandos" (2ª tanda, 2026-09-15):
// solo existía "disable" -- no había forma de reactivar, editar campos
// básicos o borrar un usuario por CLI, aunque la API HTTP (PATCH/DELETE
// /users/{id}) ya lo soporta desde antes. No existe users.Service.Enable
// (solo Disable) -- enable/edit replican a mano la misma lógica que
// PatchUser (internal/api/v1/users_handlers.go): leer el *User completo,
// mutar solo el campo deseado, escribir el struct completo -- nunca un
// users.User{} parcial, o se perdería PasswordHash/TOTPSecret al guardar.
func newUsersEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <username>",
		Short: "Reactiva un usuario deshabilitado",
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

			u, err := userRepo.GetUserByUsername(context.Background(), args[0])
			if err != nil {
				return err
			}
			u.Status = users.StatusActive
			u.UpdatedAt = time.Now().UTC()
			if err := userRepo.UpdateUser(context.Background(), u); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Usuario %q reactivado\n", u.Username)
			return nil
		},
	}
}

func newUsersEditCmd() *cobra.Command {
	var displayName, email, quota string
	cmd := &cobra.Command{
		Use:   "edit <username>",
		Short: "Edita el nombre visible, el email o la cuota de un usuario",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("display-name") && !cmd.Flags().Changed("email") && !cmd.Flags().Changed("quota") {
				return fmt.Errorf("indica al menos --display-name, --email o --quota")
			}
			var quotaBytes *int64
			if cmd.Flags().Changed("quota") {
				var err error
				if quotaBytes, err = parseQuotaFlag(quota); err != nil {
					return err
				}
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

			u, err := userRepo.GetUserByUsername(context.Background(), args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("display-name") {
				u.DisplayName = displayName
			}
			if cmd.Flags().Changed("email") {
				u.Email = email
			}
			if cmd.Flags().Changed("quota") {
				u.QuotaBytes = quotaBytes
			}
			u.UpdatedAt = time.Now().UTC()
			if err := userRepo.UpdateUser(context.Background(), u); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Usuario %q actualizado\n", u.Username)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "nuevo nombre visible")
	cmd.Flags().StringVar(&email, "email", "", "nuevo email")
	cmd.Flags().StringVar(&quota, "quota", "", "cuota de almacenamiento: un tamaño (100GB), unlimited (sin límite) o inherit (hereda la del grupo o la global)")
	return cmd
}

func newUsersDeleteCmd() *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <username>",
		Short: "Borra un usuario PARA SIEMPRE -- irreversible, exige --confirm",
		Long: "Borra el usuario y, en cascada, sus sesiones, pertenencia a grupos,\n" +
			"invitaciones creadas por él y sus comparticiones (shares) -- y los\n" +
			"METADATOS de todos sus archivos y carpetas. El contenido físico de esos\n" +
			"archivos en el Storage Pool NO se borra solo: queda huérfano en disco\n" +
			"(limitación ya existente en la API HTTP, no de este comando). No hay\n" +
			"\"deshacer\" -- si tienes dudas, usa \"users disable\" en su lugar.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("esto borra al usuario PARA SIEMPRE, en cascada (sesiones, grupos, invitaciones, shares, metadatos de archivos) -- repite el comando con --confirm si estás seguro")
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

			u, err := userRepo.GetUserByUsername(context.Background(), args[0])
			if err != nil {
				return err
			}
			if err := userRepo.DeleteUser(context.Background(), u.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Usuario %q borrado.\n", u.Username)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "confirma el borrado irreversible (obligatorio)")
	return cmd
}

// newUsersInvitationCmd expone por CLI la creación/listado/revocación de
// invitaciones de alta (§21) -- hasta ahora solo por API, admin-only.
// El canje en sí (/invitations/redeem) sigue siendo solo web/API a
// propósito: es autoalta pública, no una operación de administrador.
// Backlog "todo por comandos", 2ª tanda (2026-09-15).
func newUsersInvitationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invitation",
		Short: "Invitaciones de alta (§21) -- crear, listar, revocar",
	}
	cmd.AddCommand(newUsersInvitationCreateCmd(), newUsersInvitationListCmd(), newUsersInvitationRevokeCmd())
	return cmd
}

func newUsersInvitationCreateCmd() *cobra.Command {
	var createdBy, role string
	var maxUses, ttlHours int
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Crea una invitación de alta",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, _, userRepo, err := openInvitationService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			// invitations.created_by es NOT NULL -- no hay actor de sesión
			// del que sacarlo en un CLI, así que --created-by hace de "quién
			// soy" (mismo criterio que --username en el resto del CLI).
			creatorID, err := resolveOwnerID(cmd.Context(), userRepo, createdBy)
			if err != nil {
				return fmt.Errorf("--created-by: %w", err)
			}

			inv, token, err := svc.Create(cmd.Context(), auth.CreateInvitationInput{
				CreatedBy: creatorID, RoleID: role, MaxUses: maxUses, TTL: time.Duration(ttlHours) * time.Hour,
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Invitación creada (id=%s)\n", inv.ID)
			fmt.Fprintf(out, "Token (dáselo a la persona que se va a dar de alta; se muestra una sola vez): %s\n", token)
			fmt.Fprintf(out, "Expira: %s -- usos máximos: %d\n", inv.ExpiresAt.Local().Format("2006-01-02 15:04:05"), inv.MaxUses)
			fmt.Fprintln(out, "Para canjearla: POST /api/v1/invitations/redeem (web o API) con ese token -- el canje en sí no tiene comando CLI, es autoalta pública.")
			return nil
		},
	}
	cmd.Flags().StringVar(&createdBy, "created-by", "", "usuario administrador que crea la invitación (obligatorio; si se borra después, la invitación se borra en cascada)")
	cmd.Flags().StringVar(&role, "role", "", "rol que recibirá quien la canjee (vacío = user por defecto)")
	cmd.Flags().IntVar(&maxUses, "max-uses", 1, "usos máximos")
	cmd.Flags().IntVar(&ttlHours, "ttl-hours", 168, "horas hasta que expire (por defecto 168 = 7 días)")
	return cmd
}

func newUsersInvitationListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista las invitaciones creadas",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, _, invRepo, _, err := openInvitationService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			invitations, err := invRepo.ListInvitations(cmd.Context())
			if err != nil {
				return err
			}
			if len(invitations) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(sin invitaciones)")
				return nil
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-36s  %-14s %8s  %-20s  %-10s  %s\n", "ID", "ROL", "USOS", "EXPIRA", "ESTADO", "CREADA POR")
			now := time.Now()
			for _, inv := range invitations {
				status := "usable"
				switch {
				case inv.RevokedAt != nil:
					status = "revocada"
				case now.After(inv.ExpiresAt):
					status = "expirada"
				case inv.UseCount >= inv.MaxUses:
					status = "agotada"
				}
				role := inv.RoleID
				if role == "" {
					role = users.RoleUser + " (def.)"
				}
				fmt.Fprintf(out, "%-36s  %-14s %4d/%-3d  %-20s  %-10s  %s\n",
					inv.ID, role, inv.UseCount, inv.MaxUses,
					inv.ExpiresAt.Local().Format("2006-01-02 15:04:05"), status, inv.CreatedBy)
			}
			return nil
		},
	}
}

func newUsersInvitationRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoca una invitación (el id lo da \"users invitation list\")",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, _, invRepo, _, err := openInvitationService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			if err := invRepo.RevokeInvitation(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Invitación %s revocada.\n", args[0])
			return nil
		},
	}
}
