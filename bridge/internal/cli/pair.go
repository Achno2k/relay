package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"relay/internal/config"
	"relay/internal/qr"
)

func init() {
	var port int
	var host, base string
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Print the pairing QR code and URL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPair(port, host, base)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7878, "port the bridge listens on")
	cmd.Flags().StringVar(&host, "host", "", "host to put in the URL (defaults to the Tailscale IPv4)")
	cmd.Flags().StringVar(&base, "url", "", "base URL to put in the link verbatim, e.g. http://100.101.102.103:7878 (overrides --host and --port)")
	Register(cmd)
}

func runPair(port int, host, base string) error {
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

func printPairing(url string) {
	if code, ok := qr.Terminal(url, 2); ok {
		fmt.Println(code)
	}
	fmt.Println(url)
}
