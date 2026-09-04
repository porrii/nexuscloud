package cli

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/users"
)

// openUsersRepo abre la base de datos configurada (aplicando migraciones si
// migrate=true) y devuelve tanto la conexión cruda (para poder cerrarla)
// como un repositorio de usuarios listo para usar. Común a admin/users/doctor.
func openUsersRepo(cfg *config.Config, migrate bool) (*sql.DB, users.Repository, error) {
	sqlDB, err := db.Open(cfg)
	if err != nil {
		return nil, nil, err
	}
	if migrate {
		if err := db.Migrate(cfg, sqlDB); err != nil {
			sqlDB.Close()
			return nil, nil, err
		}
	}
	return sqlDB, users.NewSQLRepository(db.Wrap(cfg.Database.Driver, sqlDB)), nil
}

// promptPassword solicita una contraseña sin eco cuando stdin es una
// terminal real; si es un pipe (p.ej. Docker/scripts) lee una línea. Nunca
// se acepta una contraseña vacía (§140: nunca admin/admin).
func promptPassword(cmd *cobra.Command) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), "Contraseña: ")

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(cmd.OutOrStdout())
		if err != nil {
			return "", fmt.Errorf("leyendo contraseña: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("leyendo contraseña de stdin: %w", err)
	}
	return strings.TrimSpace(line), nil
}
