package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión de NexusCloud",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
			return nil
		},
	}
}
