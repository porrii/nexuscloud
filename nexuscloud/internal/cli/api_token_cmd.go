package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/auth"
)

// newUsersAPITokenCmd gestiona los tokens de acceso a la API REST completa
// (§78, ADR-037), de alcance todo-o-nada: el token actúa exactamente como
// el usuario. Mismo criterio que newUsersWebdavTokenCmd -- se puede crear
// sin interfaz web, con --expires-in opcional (webdav-token no lo tiene:
// nunca expira).
func newUsersAPITokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-token",
		Short: "Tokens de acceso a la API (§78, ADR-037)",
	}
	cmd.AddCommand(newUsersAPITokenCreateCmd(), newUsersAPITokenListCmd(), newUsersAPITokenRevokeCmd())
	return cmd
}

func newUsersAPITokenCreateCmd() *cobra.Command {
	var username, label, expiresIn string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Crea un token de acceso a la API (se muestra una sola vez)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" {
				return fmt.Errorf("--username es obligatorio")
			}
			expiresAt, err := parseExpiresIn(expiresIn, time.Now().UTC())
			if err != nil {
				return err
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
			tok, plain, err := openAPITokenService(cfg, sqlDB, userRepo).Create(context.Background(), u.ID, label, expiresAt)
			if err != nil {
				if errors.Is(err, auth.ErrInvalidAPITokenLabel) {
					return fmt.Errorf("--label es demasiado largo (máximo 100 caracteres)")
				}
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Token de API %q creado para %q.\n\n", tok.Label, username)
			fmt.Fprintln(out, "Cópialo ahora: NO se vuelve a mostrar (solo se guarda su huella).")
			fmt.Fprintf(out, "\n  %s\n\n", plain)
			if tok.ExpiresAt != nil {
				fmt.Fprintf(out, "Expira el %s.\n", tok.ExpiresAt.Format(time.RFC3339))
			} else {
				fmt.Fprintln(out, "No expira (revócalo manualmente cuando ya no lo necesites).")
			}
			fmt.Fprintln(out, "Úsalo como cabecera: Authorization: Bearer <token>, contra cualquier endpoint de /api/v1 -- actúa exactamente como este usuario.")
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
	cmd.Flags().StringVar(&label, "label", "", `nombre para reconocer el token, p.ej. "script de backup" (por defecto "Token de API")`)
	cmd.Flags().StringVar(&expiresIn, "expires-in", "", "cuándo caduca: 30d, 90d, 1y, 12h... (por defecto nunca)")
	return cmd
}

func newUsersAPITokenListCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los tokens de acceso a la API de un usuario",
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
			tokens, err := openAPITokenService(cfg, sqlDB, userRepo).List(context.Background(), u.ID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(tokens) == 0 {
				fmt.Fprintf(out, "%q no tiene tokens de acceso a la API.\n", username)
				return nil
			}
			for _, t := range tokens {
				lastUsed := "nunca"
				if t.LastUsedAt != nil {
					lastUsed = t.LastUsedAt.Format(time.RFC3339)
				}
				expires := "nunca"
				if t.ExpiresAt != nil {
					expires = t.ExpiresAt.Format(time.RFC3339)
				}
				fmt.Fprintf(out, "%-36s  %-20s  creado %s  expira %s  último uso %s\n",
					t.ID, t.Label, t.CreatedAt.Format(time.RFC3339), expires, lastUsed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "usuario (obligatorio)")
	return cmd
}

func newUsersAPITokenRevokeCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "revoke <token-id>",
		Short: "Revoca un token de acceso a la API (deja de autenticar peticiones)",
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
			if err := openAPITokenService(cfg, sqlDB, userRepo).Revoke(context.Background(), args[0], u.ID); err != nil {
				if errors.Is(err, auth.ErrAPITokenNotFound) {
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

// parseExpiresIn interpreta --expires-in: vacío/"never"/"nunca" = sin
// expiración; "<N>d"/"<N>y" son días/años (que time.ParseDuration no
// entiende de por sí); cualquier otra cosa se delega en
// time.ParseDuration ("12h", "30m"...). now se recibe como parámetro para
// que el resultado sea determinista en los tests.
func parseExpiresIn(s string, now time.Time) (*time.Time, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" || v == "never" || v == "nunca" {
		return nil, nil
	}
	invalid := fmt.Errorf("%q no es una expiración válida (ejemplos: 30d, 90d, 1y, 12h, never)", s)

	if n, ok := strings.CutSuffix(v, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil || days <= 0 {
			return nil, invalid
		}
		t := now.Add(time.Duration(days) * 24 * time.Hour)
		return &t, nil
	}
	if n, ok := strings.CutSuffix(v, "y"); ok {
		years, err := strconv.Atoi(n)
		if err != nil || years <= 0 {
			return nil, invalid
		}
		t := now.AddDate(years, 0, 0)
		return &t, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return nil, invalid
	}
	t := now.Add(d)
	return &t, nil
}
