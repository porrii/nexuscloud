package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

// Cuotas de almacenamiento por CLI (§24, ADR-036): el valor de --quota, el
// informe `users quota` y `users group edit`.

var (
	errQuotaEmpty = errors.New("--quota está vacío: indica un tamaño (100GB), unlimited o inherit")
	sizePattern   = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([a-zA-Z]*)$`)
)

// sizeUnits son las unidades de tamaño aceptadas. Son binarias, como todo lo
// que muestra la CLI (humanBytes): 1 GB = 1 GiB = 1024³ bytes.
var sizeUnits = map[string]int64{
	"": 1, "b": 1,
	"kb": 1 << 10, "kib": 1 << 10,
	"mb": 1 << 20, "mib": 1 << 20,
	"gb": 1 << 30, "gib": 1 << 30,
	"tb": 1 << 40, "tib": 1 << 40,
	"pb": 1 << 50, "pib": 1 << 50,
}

// parseQuotaFlag interpreta el valor de --quota: un tamaño (100GB, 1.5TB,
// 500MB, 2048 bytes), unlimited/ilimitada (0: sin límite aunque el grupo o la
// global lo tengan) o inherit/heredar (nil: sin cuota propia, hereda).
func parseQuotaFlag(s string) (*int64, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return nil, errQuotaEmpty
	}
	switch strings.ToLower(v) {
	case "inherit", "heredar", "none", "ninguna":
		return nil, nil
	case "unlimited", "ilimitada", "ilimitado":
		zero := int64(0)
		return &zero, nil
	}
	n, err := parseByteSize(v)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// parseByteSize convierte «100GB», «1.5 TiB» o «2048» en bytes. Los decimales
// solo se admiten con unidad (no hay medio byte).
func parseByteSize(s string) (int64, error) {
	invalid := fmt.Errorf("%q no es un tamaño válido (ejemplos: 100GB, 1.5TB, 500MB, 2048, unlimited, inherit)", s)
	m := sizePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, invalid
	}
	unit, ok := sizeUnits[strings.ToLower(m[2])]
	if !ok {
		return 0, invalid
	}
	tooBig := fmt.Errorf("%q es demasiado grande", s)

	if !strings.Contains(m[1], ".") {
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || n > math.MaxInt64/unit {
			return 0, tooBig
		}
		return n * unit, nil
	}
	if m[2] == "" {
		return 0, invalid
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, invalid
	}
	bytes := f * float64(unit)
	if bytes >= float64(math.MaxInt64) {
		return 0, tooBig
	}
	return int64(math.Round(bytes)), nil
}

// formatQuota es cómo se muestra una cuota PROPIA: nil = hereda; 0 = ilimitada.
func formatQuota(q *int64) string {
	switch {
	case q == nil:
		return "heredada"
	case *q == 0:
		return "ilimitada"
	default:
		return humanBytes(uint64(*q))
	}
}

// newUsersGroupEditCmd fija la cuota por miembro de un grupo. De momento es lo
// único editable de un grupo, así que --quota es obligatoria.
func newUsersGroupEditCmd() *cobra.Command {
	var quota string
	cmd := &cobra.Command{
		Use:   "edit <grupo>",
		Short: "Cambia la cuota por miembro de un grupo (§24)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("quota") {
				return fmt.Errorf("indica --quota (p. ej. --quota 500GB, --quota unlimited o --quota inherit)")
			}
			value, err := parseQuotaFlag(quota)
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

			group, err := userRepo.GetGroupByName(context.Background(), args[0])
			if err != nil {
				return fmt.Errorf("grupo %q no encontrado: %w", args[0], err)
			}
			if err := users.NewService(userRepo).SetGroupQuota(context.Background(), group.ID, value); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cuota del grupo %q: %s (por miembro)\n", group.Name, formatQuota(value))
			return nil
		},
	}
	cmd.Flags().StringVar(&quota, "quota", "", "cuota por miembro: un tamaño (500GB), unlimited o inherit")
	return cmd
}

// newUsersQuotaCmd es el informe de uso y cuota efectiva de los usuarios.
func newUsersQuotaCmd() *cobra.Command {
	var detail bool
	cmd := &cobra.Command{
		Use:   "quota [usuario]",
		Short: "Muestra el uso y la cuota efectiva de los usuarios (§24)",
		Long: "Muestra, por usuario, lo que ocupa (archivos + papelera + versiones anteriores), su cuota efectiva\n" +
			"y de dónde sale: cuota propia del usuario, de uno de sus grupos o la global (storage.defaultQuotaBytes).",
		Args: cobra.MaximumNArgs(1),
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

			var list []*users.User
			if len(args) == 1 {
				u, err := userRepo.GetUserByUsername(ctx, args[0])
				if err != nil {
					return fmt.Errorf("usuario %q no encontrado: %w", args[0], err)
				}
				list = []*users.User{u}
			} else if list, err = userRepo.ListUsers(ctx); err != nil {
				return err
			}

			svc := users.NewService(userRepo, users.WithDefaultQuota(cfg.Storage.DefaultQuotaBytes))
			usage, err := storage.NewSQLUsageRepository(db.Wrap(cfg.Database.Driver, sqlDB)).AllOwnersUsage(ctx)
			if err != nil {
				return err
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			if detail {
				fmt.Fprintln(tw, "USUARIO\tUSADO\tARCHIVOS\tPAPELERA\tVERSIONES\tCUOTA\t%\tORIGEN")
			} else {
				fmt.Fprintln(tw, "USUARIO\tUSADO\tCUOTA\t%\tORIGEN")
			}
			for _, u := range list {
				eq, err := svc.EffectiveQuota(ctx, u.ID)
				if err != nil {
					return err
				}
				used := usage[u.ID]
				limit, percent := "ilimitada", "-"
				if !eq.Unlimited() {
					limit = humanBytes(uint64(eq.LimitBytes))
					percent = fmt.Sprintf("%.1f%%", float64(used.Total())/float64(eq.LimitBytes)*100)
				}
				if detail {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", u.Username, humanBytes(uint64(used.Total())),
						humanBytes(uint64(used.FilesBytes)), humanBytes(uint64(used.TrashBytes)), humanBytes(uint64(used.VersionsBytes)),
						limit, percent, quotaSourceLabel(eq))
				} else {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", u.Username, humanBytes(uint64(used.Total())), limit, percent, quotaSourceLabel(eq))
				}
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&detail, "detail", false, "desglosa el uso en archivos, papelera y versiones")
	return cmd
}

// quotaSourceLabel dice de dónde sale el límite de un usuario.
func quotaSourceLabel(q users.EffectiveQuota) string {
	switch q.Source {
	case users.QuotaSourceUser:
		return "usuario"
	case users.QuotaSourceGroup:
		return "grupo:" + q.GroupName
	case users.QuotaSourceGlobal:
		return "global"
	default:
		return "-"
	}
}
