package cli

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"relay/internal/config"
	"relay/internal/ui"
)

func init() { Register(attachCmd) }

// attachCmd drops the user into herdr on the box over ssh.
var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach your terminal to herdr on the box",
	Args:  cobra.NoArgs,
	RunE:  runAttach,
}

func runAttach(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		ui.Fail(err.Error())
		return err
	}
	if cfg.Box.Host == "" {
		ui.Fail("no box host in config (herdr --remote needs a directly reachable host); run `relay init`")
		return errors.New("config has no box host")
	}
	if cfg.Box.Transport == "ssm" {
		ui.Warn("SSM transport configured; herdr --remote connects with plain ssh and may not reach the box")
	}
	return execHerdrRemote(cfg)
}

// herdrRemoteArgs builds the argv for the local herdr attach.
func herdrRemoteArgs(cfg config.Config) []string {
	target := cfg.Box.Host
	if cfg.Box.User != "" {
		target = cfg.Box.User + "@" + cfg.Box.Host
	}
	return []string{"--remote", target}
}

// execHerdrRemote runs local `herdr --remote <user>@<host>` with the tty
// attached and propagates herdr's exit status.
func execHerdrRemote(cfg config.Config) error {
	bin, err := exec.LookPath("herdr")
	if err != nil {
		ui.Fail("herdr not found on PATH; install it first (see https://herdr.dev)")
		return err
	}
	argv := herdrRemoteArgs(cfg)
	ui.Muted(bin + " " + strings.Join(argv, " "))

	c := exec.Command(bin, argv...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		ui.Fail("herdr: " + err.Error())
		return err
	}
	return nil
}
