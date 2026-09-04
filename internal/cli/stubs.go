package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// notImplementedYet crea un subcomando "reservado": existe en la forma del
// árbol de CLI descrito en §98 desde la Fase 1, pero su funcionalidad real
// (Backup Manager, gestión avanzada de Storage/RAID, políticas de
// seguridad) llega en fases posteriores (§163 Fase 5-6). Mantener el hueco
// ahora evita romper la forma del CLI cuando se implemente.
func notImplementedYet(use, short, phase string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%q todavía no está implementado (planificado para %s); ver docs/architecture.md", use, phase)
		},
	}
}

func newStorageCmd() *cobra.Command {
	cmd := notImplementedYet("storage", "Gestión de Storage Pools y discos (Fase 5)", "Fase 5")
	cmd.AddCommand(
		notImplementedYet("list", "Lista Storage Pools", "Fase 5"),
		notImplementedYet("add", "Añade un Storage Pool", "Fase 5"),
	)
	return cmd
}

func newBackupCmd() *cobra.Command {
	cmd := notImplementedYet("backup", "Backup Manager (§18, Fase 5)", "Fase 5")
	cmd.AddCommand(
		notImplementedYet("run", "Ejecuta un backup manual", "Fase 5"),
		notImplementedYet("list", "Lista backups disponibles", "Fase 5"),
		notImplementedYet("restore", "Restaura un backup", "Fase 5"),
	)
	return cmd
}

func newSecurityCmd() *cobra.Command {
	cmd := notImplementedYet("security", "Políticas de seguridad avanzadas (2FA/Passkeys/WebDAV, Fase 6)", "Fase 6")
	cmd.AddCommand(
		notImplementedYet("checkup", "Security Checkup (§141)", "Fase 6"),
	)
	return cmd
}
