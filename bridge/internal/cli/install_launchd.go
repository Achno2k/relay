package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"relay/internal/config"
)

func init() {
	var port int
	cmd := &cobra.Command{
		Use:   "install-launchd",
		Short: "Write a LaunchAgent plist for `relay serve` (does not load it)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInstallLaunchd(port)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7878, "port to serve on")
	Register(cmd)
}

func runInstallLaunchd(port int) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	path := config.LaunchAgentPath()
	if err := writeUnit(path, config.LaunchAgentPlist(exe, port)); err != nil {
		return err
	}
	fmt.Printf("load it with:\n  launchctl bootstrap gui/$(id -u) %s\n", path)
	fmt.Printf("unload with:\n  launchctl bootout gui/$(id -u)/%s\n", config.LaunchAgentLabel)
	return nil
}

// executable is this binary's resolved path, for the service files.
func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func writeUnit(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Println("wrote " + path)
	return nil
}
