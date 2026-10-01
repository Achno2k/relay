package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"relay/internal/config"
	"relay/internal/herdr"
	"relay/internal/machine"
	"relay/internal/service"
)

func init() {
	var port int
	var localOnly, requireTailscale bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP + WebSocket bridge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(port, localOnly, requireTailscale)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7878, "port to listen on")
	cmd.Flags().BoolVar(&localOnly, "local-only", false, "bind to 127.0.0.1 only, even if Tailscale is up")
	cmd.Flags().BoolVar(&requireTailscale, "require-tailscale", false, "exit instead of falling back to 127.0.0.1 when Tailscale is down (for launchd/systemd)")
	Register(cmd)
}

func runServe(port int, localOnly, requireTailscale bool) error {
	socketPath := herdr.DefaultSocketPath()
	if err := herdr.CheckSocketPath(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, "relay: "+err.Error()+"; set HERDR_SOCKET_PATH to a shorter path")
		return exitCode(78) // EX_CONFIG
	}
	if err := machine.CheckOverrides(); err != nil {
		fmt.Fprintln(os.Stderr, "relay: "+err.Error())
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
	if !localOnly {
		if ip := config.TailscaleIPv4(); ip != "" {
			hosts = append(hosts, ip)
		} else if requireTailscale {
			fmt.Println("tailscale ip -4 unavailable; exiting so launchd or systemd retries")
			return exitCode(75) // EX_TEMPFAIL
		} else {
			fmt.Println("tailscale ip -4 unavailable; listening on 127.0.0.1 only")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// After the first signal, a second one kills the process the default way.
	context.AfterFunc(ctx, stop)
	return service.Run(ctx, service.Options{
		Port:       port,
		Hosts:      hosts,
		Token:      tok,
		SocketPath: socketPath,
		Logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
	})
}
