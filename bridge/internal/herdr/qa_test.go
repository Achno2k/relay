package herdr_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
)

// QA R8-5: herdr's client errors map to 4xx, not 502.
func TestClientErrorCodesMapTo4xx(t *testing.T) {
	for code, want := range map[string]struct {
		status int
		code   string
	}{
		"unsupported_agent_kind": {400, "unsupported"},
		"agent_name_taken":       {409, "agent_name_taken"},
		"agent_blocked":          {409, "agent_blocked"},
		"pane_not_found":         {404, "not_found"},
		"invalid_params":         {400, "invalid_params"},
		"agent_pane_busy":        {502, "agent_pane_busy"},
	} {
		got := api.FromError(herdr.Remote(code, "m"))
		if got.Status != want.status || got.Code != want.code {
			t.Errorf("%s -> %d %s", code, got.Status, got.Code)
		}
	}
	if e := api.FromError(herdr.Timeout()); e.Status != 504 || e.Code != "herdr_timeout" {
		t.Errorf("timeout -> %+v", e)
	}
}

// QA R8-6: herdr closing the connection before answering is 503 herdr_unavailable.
func TestHangupMidRequestIsUnavailable(t *testing.T) {
	fake := herdrtest.New(t, func(string, map[string]any) any { return herdrtest.Hangup{} })
	start := time.Now()
	_, err := herdr.NewClient(fake.SocketPath).Agents(context.Background())
	if e := api.FromError(err); e.Status != 503 || e.Code != "herdr_unavailable" {
		t.Fatalf("got %+v (%v)", e, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("too slow")
	}
}

// QA R8-7: a hung herdr answers 504 within a few seconds, many requests in flight don't
// starve the first one after herdr recovers.
func TestHungHerdrTimesOutFastAndRecovers(t *testing.T) {
	var hung atomic.Bool
	hung.Store(true)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	fake := herdrtest.New(t, func(method string, _ map[string]any) any {
		if hung.Load() {
			<-release
		}
		return map[string]any{"agents": []any{}}
	})
	c := herdr.NewClient(fake.SocketPath)
	start := time.Now()
	var wg sync.WaitGroup
	var timeouts atomic.Int64
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Agents(context.Background()); api.FromError(err).Code == "herdr_timeout" {
				timeouts.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := timeouts.Load(); n != 100 {
		t.Fatalf("%d/100 timed out", n)
	}
	if d := time.Since(start); d > herdr.ReadTimeout+2*time.Second {
		t.Fatalf("hung calls took %v", d)
	}
	hung.Store(false)
	once.Do(func() { close(release) })
	start = time.Now()
	if _, err := c.Agents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("first call after recovery took %v", d)
	}
}

// QA R8-20: a socket path too long for sun_path is reported, not a confusing 502.
func TestSocketPathTooLong(t *testing.T) {
	long := "/tmp/" + strings.Repeat("x", 200)
	if err := herdr.CheckSocketPath(long); err == nil {
		t.Fatal("accepted a 205-byte path")
	}
	if err := herdr.CheckSocketPath("/tmp/relay-x/s"); err != nil {
		t.Fatal(err)
	}
	_, err := herdr.NewClient(long).Agents(context.Background())
	var he *herdr.Error
	if !errors.As(err, &he) || he.Kind != herdr.ErrUnavailable || !strings.Contains(he.Message, "longer than") {
		t.Fatalf("got %v", err)
	}
}

func TestCloseTab(t *testing.T) {
	fake := herdrtest.New(t, func(string, map[string]any) any { return map[string]any{} })
	if err := herdr.NewClient(fake.SocketPath).CloseTab(context.Background(), "w1:t2"); err != nil {
		t.Fatal(err)
	}
	if got := fake.Params("tab.close"); got != `{"tab_id":"w1:t2"}` {
		t.Fatal(got)
	}
}
