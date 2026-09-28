package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"relay/internal/api"
)

// wsWriteTimeout drops a client whose socket stopped taking frames, so its goroutine never
// hangs on a dead TCP connection.
const wsWriteTimeout = 30 * time.Second

// serveWS streams hub events, server → client only, starting with `hello`.
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		// Hummingbird answers a plain GET /ws with an empty 200.
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()

	id, events := s.opt.Hub.Subscribe()
	defer s.opt.Hub.Unsubscribe(id)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Inbound frames are ignored; reading notices when the client goes away and answers pings.
	go func() {
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()

	if write(ctx, c, api.Hello()) != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok || write(ctx, c, e) != nil {
				return
			}
		}
	}
}

func write(ctx context.Context, c *websocket.Conn, e api.ServerEvent) error {
	data, err := api.Marshal(e)
	if err != nil {
		return nil // skip an unencodable event rather than drop the client
	}
	ctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return c.Write(ctx, websocket.MessageText, data)
}
