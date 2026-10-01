// Package cli wires cobra commands. Each subcommand lives in its own file in
// this package and registers itself via Register in an init(). root.go is
// owned by the lead session; do not edit it, add a new file instead.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var Version = "dev"

var registry []*cobra.Command

// Register adds a subcommand to the root. Call from init() in your own file.
func Register(cmd *cobra.Command) { registry = append(registry, cmd) }

func Execute() error {
	root := &cobra.Command{
		Use:           "agents",
		Short:         "Run coding agents on your own EC2 box, drive them from Slack or your terminal",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	for _, c := range registry {
		root.AddCommand(c)
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
