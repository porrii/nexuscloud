package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/storage"
)

// newFilesCmd cubre el gap de mayor impacto encontrado en la auditoría
// "todo por comandos" (2026-09-15): no había ninguna forma de subir, bajar,
// listar, borrar, crear carpetas o mover archivos de un usuario sin pasar
// por la web o por curl a mano contra la API. Mismo patrón que admin/users/
// storage: acceso DIRECTO a storage.FileService desde este mismo proceso,
// nunca un cliente HTTP de sí mismo.
func newFilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files",
		Short: "Operaciones de archivo de un usuario: listar, subir, bajar, crear carpetas, borrar, mover",
	}
	cmd.AddCommand(
		newFilesListCmd(),
		newFilesUploadCmd(),
		newFilesDownloadCmd(),
		newFilesMkdirCmd(),
		newFilesRmCmd(),
		newFilesMvCmd(),
		newFilesTrashCmd(),
		newFilesVersionsCmd(),
	)
	return cmd
}

// usernameFlag registra --username, obligatorio en los 6 subcomandos de
// "files" -- es una herramienta de administrador local que opera SOBRE los
// archivos de un usuario dado, no un login de usuario (mismo criterio que
// "admin create-user --username"). La comprobación de que no esté vacío la
// hace resolveOwnerID (internal/cli/helpers.go), no cada subcomando.
func usernameFlag(cmd *cobra.Command, username *string) {
	cmd.Flags().StringVar(username, "username", "", "usuario propietario de los archivos (obligatorio)")
}

// splitRemotePath separa una ruta remota lógica ("/Documentos/informe.pdf")
// en su carpeta contenedora y el nombre final, con la misma normalización
// que ya aplica storage.FileService (path.Clean con "/" al principio) --
// así "informe.pdf" (sin carpeta) y "/informe.pdf" se comportan igual.
func splitRemotePath(remotePath string) (parent, name string) {
	return path.Split(path.Clean("/" + remotePath))
}

// resolvedEntry identifica qué es una ruta remota ya existente: exactamente
// uno de los dos campos queda no-nil cuando no hay error.
type resolvedEntry struct {
	file *storage.FileMeta
	dir  *storage.Directory
}

// resolveRemotePath busca una ruta remota dentro del listado de su carpeta
// contenedora -- FileService no tiene un "get por ruta completa" (las rutas
// solo se resuelven relativas a un listado de su padre), así que se hace
// aquí lo mismo que haría un explorador de archivos: listar el padre y
// buscar el nombre exacto entre carpetas y archivos. Usado por download/rm/mv,
// que necesitan el ID real (y saber si es archivo o carpeta) antes de operar.
func resolveRemotePath(ctx context.Context, svc *storage.FileService, ownerID, remotePath string) (*resolvedEntry, error) {
	parent, name := splitRemotePath(remotePath)
	if name == "" {
		return nil, fmt.Errorf("ruta inválida: %q", remotePath)
	}
	result, err := svc.List(ctx, ownerID, parent)
	if err != nil {
		return nil, err
	}
	for _, d := range result.Directories {
		if d.Name == name {
			return &resolvedEntry{dir: d}, nil
		}
	}
	for _, f := range result.Files {
		if f.Name == name {
			return &resolvedEntry{file: f}, nil
		}
	}
	return nil, fmt.Errorf("no existe %q", remotePath)
}

func newFilesListCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "list [ruta]",
		Short: "Lista archivos y carpetas de una ruta (por defecto, la raíz)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parent := "/"
			if len(args) == 1 {
				parent = args[0]
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			result, err := svc.List(cmd.Context(), ownerID, parent)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, d := range result.Directories {
				fmt.Fprintf(out, "%-6s %10s  %s\n", "DIR", "-", d.Name)
			}
			for _, f := range result.Files {
				fmt.Fprintf(out, "%-6s %10s  %s\n", "FILE", humanBytes(uint64(f.SizeBytes)), f.Name)
			}
			if len(result.Directories) == 0 && len(result.Files) == 0 {
				fmt.Fprintln(out, "(vacío)")
			}
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newFilesUploadCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "upload --username <u> <local> <ruta-remota>",
		Short: "Sube un fichero local a una ruta remota",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			localPath, remotePath := args[0], args[1]
			f, err := os.Open(localPath)
			if err != nil {
				return fmt.Errorf("abriendo %s: %w", localPath, err)
			}
			defer f.Close()

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			parent, name := splitRemotePath(remotePath)
			if name == "" {
				return fmt.Errorf("ruta remota inválida: %q", remotePath)
			}
			meta, err := svc.Upload(cmd.Context(), storage.UploadInput{
				OwnerID: ownerID, ParentPath: parent, Name: name, Content: f,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Subido: %s (%s)\n", remotePath, humanBytes(uint64(meta.SizeBytes)))
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newFilesDownloadCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "download --username <u> <ruta-remota> <local>",
		Short: "Descarga un fichero remoto a una ruta local",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			remotePath, localPath := args[0], args[1]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			entry, err := resolveRemotePath(cmd.Context(), svc, ownerID, remotePath)
			if err != nil {
				return err
			}
			if entry.file == nil {
				return fmt.Errorf("%q es una carpeta, no un archivo", remotePath)
			}
			_, rc, err := svc.Download(cmd.Context(), ownerID, entry.file.ID)
			if err != nil {
				return err
			}
			defer rc.Close()

			out, err := os.Create(localPath)
			if err != nil {
				return fmt.Errorf("creando %s: %w", localPath, err)
			}
			defer out.Close()
			written, err := io.Copy(out, rc)
			if err != nil {
				return fmt.Errorf("escribiendo %s: %w", localPath, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Descargado: %s -> %s (%s)\n", remotePath, localPath, humanBytes(uint64(written)))
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newFilesMkdirCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "mkdir --username <u> <ruta>",
		Short: "Crea una carpeta remota",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			parent, name := splitRemotePath(args[0])
			if name == "" {
				return fmt.Errorf("ruta inválida: %q", args[0])
			}
			if _, err := svc.Mkdir(cmd.Context(), ownerID, parent, name, ""); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Carpeta creada: %s\n", args[0])
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newFilesRmCmd() *cobra.Command {
	var username string
	var permanent bool
	cmd := &cobra.Command{
		Use:   "rm --username <u> <ruta>",
		Short: "Borra un archivo o carpeta remota (a la papelera, salvo --permanent)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			entry, err := resolveRemotePath(cmd.Context(), svc, ownerID, args[0])
			if err != nil {
				return err
			}
			switch {
			case entry.file != nil && permanent:
				err = svc.PermanentlyDeleteFile(cmd.Context(), ownerID, entry.file.ID)
			case entry.file != nil:
				err = svc.Delete(cmd.Context(), ownerID, entry.file.ID)
			case permanent:
				err = svc.PermanentlyDeleteDirectory(cmd.Context(), ownerID, entry.dir.ID)
			default:
				err = svc.DeleteDirectory(cmd.Context(), ownerID, entry.dir.ID)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Borrado: %s\n", args[0])
			return nil
		},
	}
	usernameFlag(cmd, &username)
	cmd.Flags().BoolVar(&permanent, "permanent", false, "borra permanentemente en vez de mover a la papelera")
	return cmd
}

func newFilesMvCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "mv --username <u> <origen> <destino>",
		Short: "Mueve o renombra un archivo o carpeta remota",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			entry, err := resolveRemotePath(cmd.Context(), svc, ownerID, args[0])
			if err != nil {
				return err
			}
			newParent, newName := splitRemotePath(args[1])
			if newName == "" {
				return fmt.Errorf("ruta destino inválida: %q", args[1])
			}
			if entry.file != nil {
				_, err = svc.MoveFile(cmd.Context(), ownerID, entry.file.ID, &newParent, &newName)
			} else {
				_, err = svc.MoveDirectory(cmd.Context(), ownerID, entry.dir.ID, &newParent, &newName)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Movido: %s -> %s\n", args[0], args[1])
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

// newFilesTrashCmd y newFilesVersionsCmd cierran 2 de los huecos restantes
// de la auditoría "todo por comandos" (2ª tanda, 2026-09-15): no había
// forma de que un administrador gestionara la papelera o el historial de
// versiones de OTRO usuario -- ListTrash/ListVersions/DownloadVersion/
// RestoreVersion ya toman el owner/requester como parámetro explícito
// (nunca lo sacan de una sesión HTTP), así que --username + resolveOwnerID
// ya es la comprobación correcta, sin tocar internal/storage para nada.
func newFilesTrashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "Papelera de un usuario (§16)",
	}
	cmd.AddCommand(newFilesTrashListCmd(), newFilesTrashRestoreCmd())
	return cmd
}

func newFilesTrashListCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista la papelera de un usuario (archivos y carpetas borrados, vista plana)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			result, err := svc.ListTrash(cmd.Context(), ownerID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(result.Directories) == 0 && len(result.Files) == 0 {
				fmt.Fprintln(out, "(papelera vacía)")
				return nil
			}
			fmt.Fprintf(out, "%-36s  %-6s %10s  %-40s  %s\n", "ID", "TIPO", "TAMAÑO", "RUTA ORIGINAL", "BORRADO")
			for _, d := range result.Directories {
				fmt.Fprintf(out, "%-36s  %-6s %10s  %-40s  %s\n",
					d.ID, "DIR", "-", path.Join(d.ParentPath, d.Name), d.DeletedAt.Local().Format("2006-01-02 15:04:05"))
			}
			for _, f := range result.Files {
				fmt.Fprintf(out, "%-36s  %-6s %10s  %-40s  %s\n",
					f.ID, "FILE", humanBytes(uint64(f.SizeBytes)), path.Join(f.ParentPath, f.Name), f.DeletedAt.Local().Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newFilesTrashRestoreCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "restore <id>",
		Short: "Restaura un archivo o carpeta de la papelera (el id lo da \"files trash list\")",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			// La papelera no distingue tipo por adelantado (vista plana) --
			// se intenta primero como archivo, y solo si específicamente no
			// se encuentra como tal se intenta como carpeta. Cualquier otro
			// error (p.ej. ErrForbidden, nombre ya ocupado) se propaga tal
			// cual, nunca se enmascara con el mensaje genérico de "no
			// encontrado en ningún sitio".
			err = svc.RestoreFile(cmd.Context(), ownerID, args[0])
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Archivo %s restaurado.\n", args[0])
				return nil
			}
			if !errors.Is(err, storage.ErrFileNotFound) {
				return err
			}
			if err := svc.RestoreDirectory(cmd.Context(), ownerID, args[0]); err != nil {
				if errors.Is(err, storage.ErrDirectoryNotFound) {
					return fmt.Errorf("no encuentro %q en la papelera (ni como archivo ni como carpeta)", args[0])
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Carpeta %s restaurada.\n", args[0])
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newFilesVersionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions",
		Short: "Historial de versiones de un archivo de un usuario (§15)",
	}
	cmd.AddCommand(newFilesVersionsListCmd(), newFilesVersionsDownloadCmd(), newFilesVersionsRestoreCmd())
	return cmd
}

func newFilesVersionsListCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "list --username <u> <ruta>",
		Short: "Lista el historial de versiones de un archivo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			entry, err := resolveRemotePath(cmd.Context(), svc, ownerID, args[0])
			if err != nil {
				return err
			}
			if entry.file == nil {
				return fmt.Errorf("%q es una carpeta, no un archivo -- las carpetas no tienen versiones", args[0])
			}
			versions, err := svc.ListVersions(cmd.Context(), ownerID, entry.file.ID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(versions) == 0 {
				fmt.Fprintln(out, "(sin versiones anteriores)")
				return nil
			}
			fmt.Fprintf(out, "%-8s %10s  %s\n", "VERSIÓN", "TAMAÑO", "FECHA")
			for _, v := range versions {
				fmt.Fprintf(out, "%-8d %10s  %s\n", v.VersionNum, humanBytes(uint64(v.SizeBytes)), v.CreatedAt.Local().Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newFilesVersionsDownloadCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "download --username <u> <ruta> <num-version> <local>",
		Short: "Descarga una versión concreta (no la actual) de un archivo",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			versionNum, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("número de versión inválido: %q", args[1])
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			entry, err := resolveRemotePath(cmd.Context(), svc, ownerID, args[0])
			if err != nil {
				return err
			}
			if entry.file == nil {
				return fmt.Errorf("%q es una carpeta, no un archivo -- las carpetas no tienen versiones", args[0])
			}
			_, rc, err := svc.DownloadVersion(cmd.Context(), ownerID, entry.file.ID, versionNum)
			if err != nil {
				return err
			}
			defer rc.Close()

			localOut, err := os.Create(args[2])
			if err != nil {
				return fmt.Errorf("creando %s: %w", args[2], err)
			}
			defer localOut.Close()
			written, err := io.Copy(localOut, rc)
			if err != nil {
				return fmt.Errorf("escribiendo %s: %w", args[2], err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Descargado: %s (versión %d) -> %s (%s)\n",
				args[0], versionNum, args[2], humanBytes(uint64(written)))
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}

func newFilesVersionsRestoreCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "restore --username <u> <ruta> <num-version>",
		Short: "Restaura una versión antigua como el contenido vigente (la actual pasa al historial, nunca se pierde)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			versionNum, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("número de versión inválido: %q", args[1])
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			sqlDB, svc, userRepo, err := openFileService(cfg, true)
			if err != nil {
				return err
			}
			defer sqlDB.Close()

			ownerID, err := resolveOwnerID(cmd.Context(), userRepo, username)
			if err != nil {
				return err
			}
			entry, err := resolveRemotePath(cmd.Context(), svc, ownerID, args[0])
			if err != nil {
				return err
			}
			if entry.file == nil {
				return fmt.Errorf("%q es una carpeta, no un archivo -- las carpetas no tienen versiones", args[0])
			}
			if _, err := svc.RestoreVersion(cmd.Context(), ownerID, entry.file.ID, versionNum); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s restaurado a la versión %d.\n", args[0], versionNum)
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}
