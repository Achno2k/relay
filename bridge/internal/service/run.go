package service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"relay/internal/config"
	"relay/internal/herdr"
	"relay/internal/live"
	"relay/internal/machine"
	"relay/internal/server"
	"relay/internal/uploads"
	"relay/internal/usage"
)

// Options is what `relay serve` passes to Run.
type Options struct {
	Port       int
	Hosts      []string // one listener per address, e.g. 127.0.0.1 and the Tailscale IP
	Token      string
	SocketPath string       // "": herdr.DefaultSocketPath()
	Logger     *slog.Logger // nil: slog.Default()
}

// Run wires herdr, the monitors and one HTTP server per host, and serves until ctx is done
// (the caller cancels it on SIGTERM/SIGINT). It returns the first listener error, if any.
func Run(ctx context.Context, opt Options) error {
	log := opt.Logger
	if log == nil {
		log = slog.Default()
	}
	socketPath := opt.SocketPath
	if socketPath == "" {
		socketPath = herdr.DefaultSocketPath()
	}
	client := herdr.NewClient(socketPath)
	svc := New(Deps{Herdr: client})
	hub := server.NewHub()
	liveReply := live.NewMonitor(client, hub)
	monitor := NewMonitor(svc, hub, herdr.NewEventStream(socketPath), MonitorOptions{
		OnSnapshots: func(snaps []Snapshot) { liveReply.Update(liveAgents(snaps)) },
		OnMessage:   liveReply.Landed,
	})
	svc.SetOnChange(func() { go monitor.Trigger(ctx) })
	usageMonitor := usage.NewMonitor(hub)
	handler := server.New(server.Options{
		Backend:        svc,
		Hub:            hub,
		Token:          opt.Token,
		HerdrReachable: monitor.HerdrReachable,
		Usage:          usageMonitor,
		Machine:        machine.Current,
		StartedAt:      time.Now(),
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	background := func(f func(context.Context)) {
		wg.Add(1)
		go func() { defer wg.Done(); f(ctx) }()
	}
	background(monitor.Run)
	background(liveReply.Run)
	background(usageMonitor.Run)
	background(func(ctx context.Context) { uploads.RunCleaner(ctx, svc.Uploads(), log) })
	background(func(ctx context.Context) { config.RunLogRotator(ctx, config.LogPath(), 10<<20, 5*time.Minute) })

	errs := make(chan error, len(opt.Hosts))
	servers := make([]*http.Server, 0, len(opt.Hosts))
	for _, host := range opt.Hosts {
		addr := net.JoinHostPort(host, strconv.Itoa(opt.Port))
		srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 30 * time.Second,
			// Request contexts end with Run, which closes hijacked WebSockets too.
			BaseContext: func(net.Listener) context.Context { return ctx }}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			cancel()
			for _, s := range servers {
				s.Close()
			}
			wg.Wait()
			return err
		}
		servers = append(servers, srv)
		log.Info("listening on http://" + addr)
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errs:
	}
	cancel()
	shutdownCtx, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	for _, s := range servers {
		_ = s.Shutdown(shutdownCtx)
	}
	// Background work stops with ctx, but a probe mid-subprocess may take a while to notice;
	// don't hold the exit for it.
	stopped := make(chan struct{})
	go func() { wg.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(backgroundGrace):
		log.Warn("background work still running at shutdown; exiting anyway")
	}
	return runErr
}

// backgroundGrace is how long Run waits for the monitors to stop after ctx is done.
const backgroundGrace = 1500 * time.Millisecond

func liveAgents(snaps []Snapshot) []live.Agent {
	out := make([]live.Agent, len(snaps))
	for i, s := range snaps {
		out[i] = live.Agent{ID: s.Agent.ID, Kind: s.Agent.Kind, Status: s.Agent.Status, Cwd: s.Raw.CwdOrForeground()}
	}
	return out
}
