package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/storage"
)

// newSharesCmd cierra el último hueco de la auditoría "todo por comandos"
// (2ª tanda, 2026-09-15): crear/listar/revocar comparticiones (§37) solo
// existía por web o por curl a mano. storage.FileService ya expone todo
// lo necesario (openFileService ya inyecta el ShareRepository) -- todas
// las validaciones (tipo válido, al menos un permiso, --can-upload exige
// carpeta, fechas futuras, límites positivos, compartición/enlaces
// desactivados por config) viven dentro de CreateShare, este comando no
// las duplica.
func newSharesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shares",
		Short: "Compartición de archivos y carpetas (§37)",
	}
	cmd.AddCommand(newSharesCreateCmd(), newSharesListCmd(), newSharesRevokeCmd())
	return cmd
}

func newSharesCreateCmd() *cobra.Command {
	var username, shareType, targetUsername, targetGroup, label, password, expiresAt string
	var canUpload, noDownload bool
	var maxDownloads int
	var maxUploadSizeBytes int64
	cmd := &cobra.Command{
		Use:   "create --username <owner> <ruta> --share-type user|group|link",
		Short: "Comparte un archivo o carpeta con un usuario, un grupo, o crea un enlace",
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

			in := storage.CreateShareInput{
				ResourceIsDirectory: entry.dir != nil,
				Label:               label,
				CanDownload:         !noDownload,
				CanUpload:           canUpload,
				Password:            password,
			}
			if entry.dir != nil {
				in.ResourceID = entry.dir.ID
			} else {
				in.ResourceID = entry.file.ID
			}

			switch shareType {
			case "user":
				in.Type = storage.ShareTypeUser
				targetID, err := resolveOwnerID(cmd.Context(), userRepo, targetUsername)
				if err != nil {
					return fmt.Errorf("--target-username: %w", err)
				}
				in.TargetUserID = targetID
			case "group":
				in.Type = storage.ShareTypeGroup
				if targetGroup == "" {
					return fmt.Errorf("--target-group es obligatorio con --share-type group")
				}
				g, err := userRepo.GetGroupByName(cmd.Context(), targetGroup)
				if err != nil {
					return fmt.Errorf("grupo %q no encontrado: %w", targetGroup, err)
				}
				in.TargetGroupID = g.ID
			case "link":
				in.Type = storage.ShareTypeLink
			default:
				return fmt.Errorf("--share-type debe ser user, group o link")
			}

			if maxDownloads > 0 {
				md := maxDownloads
				in.MaxDownloads = &md
			}
			if maxUploadSizeBytes > 0 {
				mb := maxUploadSizeBytes
				in.MaxUploadSizeBytes = &mb
			}
			if expiresAt != "" {
				t, err := time.Parse(time.RFC3339, expiresAt)
				if err != nil {
					return fmt.Errorf("--expires-at debe estar en formato RFC3339 (p.ej. 2026-12-31T23:59:59Z): %w", err)
				}
				in.ExpiresAt = &t
			}

			share, token, err := svc.CreateShare(cmd.Context(), ownerID, in)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Share creado (id=%s)\n", share.ID)
			if token != "" {
				fmt.Fprintf(out, "Token del enlace (se muestra una sola vez, dáselo a quien vaya a acceder): %s\n", token)
			}
			return nil
		},
	}
	usernameFlag(cmd, &username)
	cmd.Flags().StringVar(&shareType, "share-type", "", "user | group | link (obligatorio)")
	cmd.Flags().StringVar(&targetUsername, "target-username", "", "usuario destino (obligatorio con --share-type user)")
	cmd.Flags().StringVar(&targetGroup, "target-group", "", "grupo destino (obligatorio con --share-type group)")
	cmd.Flags().StringVar(&label, "label", "", "nombre personalizado del enlace (cosmético)")
	cmd.Flags().BoolVar(&canUpload, "can-upload", false, "permite subir (solo enlaces de carpeta)")
	cmd.Flags().BoolVar(&noDownload, "no-download", false, "no permitir descarga (por defecto sí se permite)")
	cmd.Flags().StringVar(&password, "password", "", "contraseña del enlace (solo enlaces; vacío = sin contraseña)")
	cmd.Flags().StringVar(&expiresAt, "expires-at", "", "fecha de caducidad, RFC3339 (p.ej. 2026-12-31T23:59:59Z)")
	cmd.Flags().IntVar(&maxDownloads, "max-downloads", 0, "límite de descargas (0 = sin límite)")
	cmd.Flags().Int64Var(&maxUploadSizeBytes, "max-upload-size-bytes", 0, "límite de tamaño de subida en bytes (0 = sin límite)")
	return cmd
}

func newSharesListCmd() *cobra.Command {
	var username string
	var withMe bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista comparticiones (por defecto, las que YO he creado; --with-me para lo compartido conmigo)",
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

			var shares []*storage.Share
			if withMe {
				shares, err = svc.ListSharesWithMe(cmd.Context(), ownerID)
			} else {
				shares, err = svc.ListSharesByMe(cmd.Context(), ownerID)
			}
			if err != nil {
				return err
			}
			if len(shares) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(sin comparticiones)")
				return nil
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-36s  %-6s  %-20s  %-36s  %-20s  %s\n", "ID", "TIPO", "RECURSO", "DESTINO", "EXPIRA", "ESTADO")
			now := time.Now()
			for _, sh := range shares {
				resourceName := "?"
				if info, err := svc.ResourceInfoForShare(cmd.Context(), sh); err == nil {
					resourceName = info.Name
				}
				target := "(enlace)"
				switch sh.Type {
				case storage.ShareTypeUser:
					target = sh.TargetUserID
				case storage.ShareTypeGroup:
					target = sh.TargetGroupID
				}
				expires := "-"
				if sh.ExpiresAt != nil {
					expires = sh.ExpiresAt.Local().Format("2006-01-02 15:04:05")
				}
				status := "activo"
				switch {
				case sh.IsRevoked():
					status = "revocado"
				case sh.IsExpired(now):
					status = "caducado"
				case sh.IsExhausted():
					status = "agotado"
				}
				fmt.Fprintf(out, "%-36s  %-6s  %-20s  %-36s  %-20s  %s\n", sh.ID, sh.Type, resourceName, target, expires, status)
			}
			return nil
		},
	}
	usernameFlag(cmd, &username)
	cmd.Flags().BoolVar(&withMe, "with-me", false, "muestra lo compartido CONMIGO en vez de lo que yo he compartido")
	return cmd
}

func newSharesRevokeCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "revoke <share-id>",
		Short: "Revoca una compartición (el id lo da \"shares list\")",
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
			if err := svc.RevokeShare(cmd.Context(), ownerID, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Share %s revocado.\n", args[0])
			return nil
		},
	}
	usernameFlag(cmd, &username)
	return cmd
}
