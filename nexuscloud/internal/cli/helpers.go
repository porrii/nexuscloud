package cli

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/storage"
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

// openFileService abre la base de datos configurada y construye un
// storage.FileService completo -- extraído de la construcción que ya
// hacía openBackupManager (ADR-025) para RestoreToPool, reutilizado
// aquí para "nexuscloud files ..." (Tarea de auditoría "todo por
// comandos", 2026-09-15) sin duplicar la misma lista de piezas dos
// veces. Devuelve también users.Repository porque los comandos de
// files necesitan resolver --username a un ownerID antes de llamar a
// cualquier método del servicio (es una herramienta de administrador
// local que opera SOBRE los archivos de un usuario, no un login de
// usuario -- mismo criterio que "admin create-user --username").
func openFileService(cfg *config.Config, migrate bool) (*sql.DB, *storage.FileService, users.Repository, error) {
	sqlDB, conn, pools, err := openPoolRepo(cfg, migrate)
	if err != nil {
		return nil, nil, nil, err
	}
	resolver := storage.NewPoolProviderResolver(pools)
	files := storage.NewSQLFileRepository(conn)
	directories := storage.NewSQLDirectoryRepository(conn)
	versions := storage.NewSQLVersionRepository(conn)
	shares := storage.NewSQLShareRepository(conn)
	hasher := auth.NewHasher(cfg.Security.Argon2)
	fileSvc := storage.NewFileService(files, directories, versions, shares, pools, resolver, hasher,
		cfg.Trash.Enabled, cfg.Versioning.Enabled, cfg.Versioning.MaxVersionsPerFile,
		cfg.Versioning.MaxVersionAgeDays, cfg.Versioning.MaxVersionsTotalSizeBytes,
		cfg.Sharing.Enabled, cfg.Sharing.PublicLinksEnabled)
	return sqlDB, fileSvc, users.NewSQLRepository(conn), nil
}

// resolveOwnerID traduce --username a un ID de usuario real, con un
// mensaje de error claro si no existe -- mismo patrón que
// newUsersDisableCmd (GetUserByUsername) pero factorizado porque
// files_cmd.go lo necesita en cada uno de sus 6 subcomandos.
func resolveOwnerID(ctx context.Context, userRepo users.Repository, username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("--username es obligatorio")
	}
	u, err := userRepo.GetUserByUsername(ctx, username)
	if err != nil {
		return "", fmt.Errorf("usuario %q no encontrado: %w", username, err)
	}
	return u.ID, nil
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
