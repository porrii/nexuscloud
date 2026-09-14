package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// notImplementedYet crea un subcomando "reservado": existe en la forma del
// árbol de CLI descrito en §98 desde la Fase 1, pero su funcionalidad real
// (gestión avanzada de Storage/RAID, políticas de seguridad) llega en fases
// posteriores (§163 Fase 6). Mantener el hueco ahora evita romper la forma
// del CLI cuando se implemente. El Backup Manager (Fase 5) ya tiene
// implementación real -- ver backup_cmd.go, ADR-015.
func notImplementedYet(use, short, phase string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%q todavía no está implementado (planificado para %s); ver docs/architecture.md", use, phase)
		},
	}
}

func newSecurityCmd() *cobra.Command {
	cmd := notImplementedYet("security", "Políticas de seguridad avanzadas (2FA/Passkeys/WebDAV, Fase 6)", "Fase 6")
	cmd.AddCommand(
		notImplementedYet("checkup", "Security Checkup (§141)", "Fase 6"),
	)
	return cmd
}
