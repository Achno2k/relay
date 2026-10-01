// Package cli wires the cobra commands. Each subcommand lives in its own file
// in this package and registers itself via Register in an init().
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"relay/internal/api"
)

// Version is the build version, stamped with
// -ldflags "-X relay/internal/api.Version=…". /health reports the same value.
var Version = api.Version

var registry []*cobra.Command

// Register adds a subcommand to the root. Call from init() in your own file.
func Register(cmd *cobra.Command) { registry = append(registry, cmd) }

// exitCode lets a command pick the process exit status without printing an error.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

// Execute runs the command line and exits non-zero on failure.
func Execute() {
	root := &cobra.Command{
		Use:   "relay",
		Short: "Drive the coding agents in a herdr session from the Relay iOS app",
		Long: "relay runs the bridge between herdr and the Relay iOS app (serve, pair, token,\n" +
			"install-launchd, install-systemd) and sets up an EC2 box for it from your\n" +
			"laptop (init, doctor, ssh, attach, deploy, reset, env).",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	for _, c := range registry {
		root.AddCommand(c)
	}
	err := root.Execute()
	if err == nil {
		return
	}
	var code exitCode
	if errors.As(err, &code) {
		os.Exit(int(code))
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
