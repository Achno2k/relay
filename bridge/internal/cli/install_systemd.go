package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"relay/internal/config"
)

func init() {
	var port int
	cmd := &cobra.Command{
		Use:   "install-systemd",
		Short: "Write a systemd user unit for `relay serve` (does not enable it)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInstallSystemd(port)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7878, "port to serve on")
	Register(cmd)
}

func runInstallSystemd(port int) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if err := config.EnsureHome(); err != nil { // the unit appends to the log here
		return err
	}
	if err := writeUnit(config.SystemdUnitPath(), config.SystemdUnit(exe, port)); err != nil {
		return err
	}
	fmt.Printf("enable it with:\n  systemctl --user daemon-reload && systemctl --user enable --now %s\n", config.SystemdUnitName)
	fmt.Println("keep it running while logged out with:\n  loginctl enable-linger $USER")
	fmt.Printf("disable with:\n  systemctl --user disable --now %s\n", config.SystemdUnitName)
	return nil
}
