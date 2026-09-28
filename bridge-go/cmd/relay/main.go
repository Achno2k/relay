// Command relay is the bridge between herdr and the Relay iOS app.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"relay/internal/api"
	"relay/internal/config"
	"relay/internal/herdr"
	"relay/internal/qr"
	"relay/internal/service"
)

const usage = `relay: bridge between herdr and the Relay iOS app.

Usage: relay [subcommand] [flags]

Subcommands:
  serve            Run the HTTP + WebSocket bridge (default).
  pair             Print the pairing QR code and URL.
  token            Print the bearer token.
  install-launchd  Write a LaunchAgent plist for ` + "`relay serve`" + ` (does not load it).
  install-systemd  Write a systemd user unit for ` + "`relay serve`" + ` (does not enable it).

Run "relay <subcommand> -h" for its flags. "relay --version" prints the version.
`

// exitCode lets a subcommand pick the process exit status.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-version":
			fmt.Println(api.Version)
			return
		case "-h", "--help", "help":
			fmt.Print(usage)
			return
		}
		if args[0] != "" && args[0][0] != '-' {
			cmd, args = args[0], args[1:]
		}
	}
	run, ok := map[string]func([]string) error{
		"serve":           serve,
		"pair":            pair,
		"token":           token,
		"install-launchd": installLaunchd,
		"install-systemd": installSystemd,
	}[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "relay: unknown subcommand %q\n\n%s", cmd, usage)
		os.Exit(64)
	}
	if err := run(args); err != nil {
		var code exitCode
		if errors.As(err, &code) {
			os.Exit(int(code))
		}
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func newFlags(name string) *flag.FlagSet {
	return flag.NewFlagSet("relay "+name, flag.ContinueOnError)
}

func serve(args []string) error {
	fs := newFlags("serve")
	port := fs.Int("port", 7878, "Port to listen on.")
	localOnly := fs.Bool("local-only", false, "Bind to 127.0.0.1 only, even if Tailscale is up.")
	requireTailscale := fs.Bool("require-tailscale", false, "Exit instead of falling back to 127.0.0.1 when Tailscale is down (for launchd/systemd).")
	if err := fs.Parse(args); err != nil {
		return err
	}
	socketPath := herdr.DefaultSocketPath()
	if err := herdr.CheckSocketPath(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, "relay: "+err.Error()+"; set HERDR_SOCKET_PATH to a shorter path")
		return exitCode(78) // EX_CONFIG
	}
	tok, err := config.LoadToken()
	if err != nil {
		return err
	}
	if err := config.EnsureHome(); err != nil {
		return err
	}
	hosts := []string{"127.0.0.1"}
	if !*localOnly {
		if ip := config.TailscaleIPv4(); ip != "" {
			hosts = append(hosts, ip)
		} else if *requireTailscale {
			fmt.Println("tailscale ip -4 unavailable; exiting so launchd retries")
			return exitCode(75)
		} else {
			fmt.Println("tailscale ip -4 unavailable; listening on 127.0.0.1 only")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// After the first signal, a second one kills the process the default way.
	context.AfterFunc(ctx, stop)
	return service.Run(ctx, service.Options{
		Port:       *port,
		Hosts:      hosts,
		Token:      tok,
		SocketPath: socketPath,
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
	})
}

func pair(args []string) error {
	fs := newFlags("pair")
	port := fs.Int("port", 7878, "Port the bridge listens on.")
	host := fs.String("host", "", "Host to put in the URL (defaults to the Tailscale IPv4).")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tok, err := config.LoadToken()
	if err != nil {
		return err
	}
	resolved := *host
	if resolved == "" {
		resolved = config.TailscaleIPv4()
	}
	if resolved == "" {
		fmt.Print("warning: no Tailscale IPv4 found; using 127.0.0.1 (only reachable from this machine)\n\n")
		resolved = "127.0.0.1"
	}
	url := config.PairingURL(resolved, *port, tok)
	if code, ok := qr.Terminal(url, 2); ok {
		fmt.Println(code)
	}
	fmt.Println(url)
	return nil
}

func token(args []string) error {
	fs := newFlags("token")
	rotate := fs.Bool("rotate", false, "Replace the token. Paired phones must pair again.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rotate {
		t, err := config.RotateToken()
		if err != nil {
			return err
		}
		fmt.Println(t)
		fmt.Fprintln(os.Stderr, "rotated; restart `relay serve` and re-run `relay pair`")
		return nil
	}
	t, err := config.LoadToken()
	if err != nil {
		return err
	}
	fmt.Println(t)
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

func installLaunchd(args []string) error {
	fs := newFlags("install-launchd")
	port := fs.Int("port", 7878, "Port to serve on.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	path := config.LaunchAgentPath()
	if err := writeUnit(path, config.LaunchAgentPlist(exe, *port)); err != nil {
		return err
	}
	fmt.Printf("load it with:\n  launchctl bootstrap gui/$(id -u) %s\n", path)
	fmt.Printf("unload with:\n  launchctl bootout gui/$(id -u)/%s\n", config.LaunchAgentLabel)
	return nil
}

func installSystemd(args []string) error {
	fs := newFlags("install-systemd")
	port := fs.Int("port", 7878, "Port to serve on.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if err := config.EnsureHome(); err != nil { // the unit appends to the log here
		return err
	}
	if err := writeUnit(config.SystemdUnitPath(), config.SystemdUnit(exe, *port)); err != nil {
		return err
	}
	fmt.Printf("enable it with:\n  systemctl --user daemon-reload && systemctl --user enable --now %s\n", config.SystemdUnitName)
	fmt.Println("keep it running while logged out with:\n  loginctl enable-linger $USER")
	fmt.Printf("disable with:\n  systemctl --user disable --now %s\n", config.SystemdUnitName)
	return nil
}
