package cli

import (
	"github.com/Achno2k/agents-cli/internal/ui"
	"github.com/spf13/cobra"
)

func init() { Register(versionCmd) }

// versionCmd prints the build version. Also available as `agents --version`.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the agents version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ui.Info("agents " + Version)
	},
}
