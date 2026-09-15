package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

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
