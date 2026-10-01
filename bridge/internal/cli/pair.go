package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"relay/internal/config"
	"relay/internal/qr"
	"relay/internal/setup"
	"relay/internal/ui"
)

func init() {
	var port int
	var host, base, authKey string
	var yes bool
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Set up this machine (Linux) and print the pairing QR code and URL",
		Long: "On Linux, pair first installs whatever is missing: herdr and herdr-server.service,\n" +
			"Tailscale and its login, and relay.service. Each step checks first, so running it\n" +
			"again changes nothing. Then it prints the pairing QR code and URL.\n" +
			"On macOS it only prints.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPair(port, host, base, authKey, yes)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7878, "port the bridge listens on")
	cmd.Flags().StringVar(&host, "host", "", "host to put in the URL (defaults to the Tailscale IPv4)")
	cmd.Flags().StringVar(&base, "url", "", "base URL to put in the link verbatim, e.g. http://100.101.102.103:7878 (overrides --host and --port)")
	cmd.Flags().StringVar(&authKey, "authkey", "", "Tailscale auth key, to log in without the browser (Linux)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "set up without asking first (Linux)")
	Register(cmd)
}

func runPair(port int, host, base, authKey string, yes bool) error {
	if setup.Supported() {
		ip, err := setupLinux(port, authKey, yes)
		if err != nil {
			return err
		}
		if host == "" {
			host = ip
		}
		fmt.Println()
	}
	tok, err := config.LoadToken()
	if err != nil {
		return err
	}
	if base != "" {
		u, err := config.ParseBaseURL(base)
		if err != nil {
			return err
		}
		printPairing(config.PairingURLFor(u, tok))
		return nil
	}
	if host == "" {
		host = config.TailscaleIPv4()
	}
	if host == "" {
		fmt.Print("warning: no Tailscale IPv4 found; using 127.0.0.1 (only reachable from this machine)\n\n")
		host = "127.0.0.1"
	}
	printPairing(config.PairingURL(host, port, tok))
	return nil
}

func setupLinux(port int, authKey string, yes bool) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ui.Title("Setting up this machine")
	return setup.Run(ctx, pairUI{}, setup.Options{Port: port, AuthKey: authKey, Yes: yes, Relay: exe})
}

// pairUI shows setup's progress through internal/ui.
type pairUI struct{}

func (pairUI) Info(s string)    { ui.Info(s) }
func (pairUI) Success(s string) { ui.Success(s) }
func (pairUI) Warn(s string)    { ui.Warn(s) }
func (pairUI) Muted(s string)   { ui.Muted(s) }

func (pairUI) Spinner(ctx context.Context, label string, fn func(ctx context.Context) error) error {
	return ui.Spinner(ctx, label, fn)
}

func (pairUI) Confirm(title string, steps []string) (bool, error) {
	ui.Info(title)
	for _, s := range steps {
		ui.Muted("  - " + s)
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		return false, errors.New("no terminal to confirm on; run `relay pair --yes` to set up without asking")
	}
	return ui.Confirm("Go ahead?", true)
}

func (pairUI) Login(url string) {
	if code, ok := qr.Terminal(url, 2); ok {
		fmt.Println(code)
	}
	ui.Info(url)
	ui.Muted("waiting for the login to finish…")
}

func printPairing(url string) {
	if code, ok := qr.Terminal(url, 2); ok {
		fmt.Println(code)
	}
	fmt.Println(url)
}
