// Command nexuscloud es el punto de entrada único del servidor y del CLI
// de administración de NexusCloud (§98).
package main

import (
	"os"

	"github.com/porrii/nexuscloud/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
