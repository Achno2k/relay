package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"relay/internal/setup"
)

func init() {
	var port int
	cmd := &cobra.Command{
		Use:   "install-systemd",
		Short: "Write the relay.service system unit for `relay serve` (uses sudo; does not enable it)",
		Long: "Writes /etc/systemd/system/relay.service, running `relay serve` as you.\n" +
			"`relay pair` does this and more (herdr, Tailscale, enable, health check).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInstallSystemd(cmd, port)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7878, "port to serve on")
	Register(cmd)
}

func runInstallSystemd(cmd *cobra.Command, port int) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	path, err := setup.InstallRelayUnit(cmd.Context(), exe, port)
	if err != nil {
		return err
	}
	fmt.Println("wrote " + path)
	fmt.Printf("enable it with:\n  sudo systemctl enable --now %s\n", setup.RelayUnitName)
	fmt.Printf("disable with:\n  sudo systemctl disable --now %s\n", setup.RelayUnitName)
	return nil
}
