package cli

import (
	"github.com/spf13/cobra"

	"relay/internal/ui"
)

func init() { Register(versionCmd) }

// versionCmd prints the build version. Also available as `relay --version`.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the relay version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ui.Info("relay " + Version)
	},
}
