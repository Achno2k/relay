package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/ui"
	"github.com/spf13/cobra"
)

func init() { Register(attachCmd) }

// attachCmd marks a session as attached on the box, then drops the user into
// herdr over ssh so the terminal mirrors the agent's pane.
var attachCmd = &cobra.Command{
	Use:   "attach <session-id|agent-name>",
	Short: "Attach your terminal to an agent session on the box",
	Args:  cobra.ExactArgs(1),
	RunE:  runAttach,
}

func runAttach(cmd *cobra.Command, args []string) error {
	ref := args[0]
	if !validRef(ref) {
		ui.Fail("invalid session id or agent name: " + ref)
		return errors.New("invalid session reference")
	}

	cfg, err := config.Load()
	if err != nil {
		ui.Fail(err.Error())
		return err
	}
	if cfg.Box.Host == "" {
		ui.Fail("no box host in config (herdr --remote needs a directly reachable host); run `agents init`")
		return errors.New("config has no box host")
	}
	if cfg.Box.Transport == "ssm" {
		ui.Warn("SSM transport configured; herdr --remote connects with plain ssh and may not reach the box")
	}

	by, err := whoami()
	if err != nil {
		ui.Fail("could not determine local user: " + err.Error())
		return err
	}

	r := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
	script := fmt.Sprintf("agents sessions mark-attached %s --by %s", ref, by)

	var out bytes.Buffer
	if err := ui.Spinner(cmd.Context(), "Marking session attached", func(ctx context.Context) error {
		out.Reset()
		return r.Run(ctx, script, &out, &out)
	}); err != nil {
		if msg := strings.TrimSpace(out.String()); msg != "" {
			ui.Muted(msg)
		}
		ui.Fail("could not mark session as attached on the box")
		return fmt.Errorf("mark-attached: %w", err)
	}
	if msg := strings.TrimSpace(out.String()); msg != "" {
		ui.Muted(msg)
	}

	return execHerdrRemote(cfg)
}

// validRef reports whether s is a safe session id or agent name to embed in a
// remote shell command. Allows the shapes we generate: ids, repo names, and
// "<repo>-<id>" agent names.
func validRef(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '/', r == '@':
		default:
			return false
		}
	}
	return true
}

// whoami returns "<user>@<hostname>" for the attached_by field.
func whoami() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	h, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return u.Username + "@" + h, nil
}

// herdrRemoteArgs builds the argv for the local herdr attach. The herdr
// session is the bot's default session unless AGENTS_HERDR_SESSION names one.
func herdrRemoteArgs(cfg config.Config) []string {
	target := cfg.Box.Host
	if cfg.Box.User != "" {
		target = cfg.Box.User + "@" + cfg.Box.Host
	}
	argv := []string{"--remote", target}
	if s := os.Getenv("AGENTS_HERDR_SESSION"); s != "" {
		argv = append(argv, "--session", s)
	}
	return argv
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
