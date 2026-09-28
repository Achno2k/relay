package herdr_test

import (
	"bufio"
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
)

// Each socket fd must be closed exactly once, and never while another goroutine still uses it;
// either mistake closes whatever the OS handed that fd number to next. Go's netpoller owns the
// fds here, so these tests guard the Swift regressions: they churn sockets while a watcher keeps
// opening pipes and checks nobody else closed them.
type watcher struct {
	stray   atomic.Int64
	running atomic.Bool
	done    sync.WaitGroup
}

func newWatcher() *watcher {
	w := &watcher{}
	w.running.Store(true)
	w.done.Add(1)
	go func() {
		defer w.done.Done()
		for w.running.Load() {
			var p [2]int
			if unix.Pipe(p[:]) != nil {
				continue
			}
			for range 50 {
				_, e1 := unix.FcntlInt(uintptr(p[0]), unix.F_GETFD, 0)
				_, e2 := unix.FcntlInt(uintptr(p[1]), unix.F_GETFD, 0)
				if e1 != nil || e2 != nil {
					w.stray.Add(1)
					break
				}
			}
			unix.Close(p[0])
			unix.Close(p[1])
		}
	}()
	return w
}

func (w *watcher) finish() int64 {
	w.running.Store(false)
	w.done.Wait()
	return w.stray.Load()
}

func TestFailedConnectClosesItsFdOnce(t *testing.T) {
	w := newWatcher()
	missing := fmt.Sprintf("/tmp/relay-missing-%016x", rand.Uint64())
	for range 3000 {
		if c, err := herdr.Dial(context.Background(), missing); err == nil {
			c.Close()
		}
	}
	if n := w.finish(); n != 0 {
		t.Fatalf("%d stray closes", n)
	}
}

func TestCloseWhileReadingNeverClosesSomeoneElsesFd(t *testing.T) {
	fake := herdrtest.New(t, func(string, map[string]any) any { return map[string]any{"type": "subscription_started"} })
	w := newWatcher()
	for range 300 {
		c, err := herdr.Dial(context.Background(), fake.SocketPath)
		if err != nil {
			t.Fatal(err)
		}
		c.Write([]byte(`{"id":"1","method":"events.subscribe","params":{}}` + "\n"))
		r := bufio.NewReader(c)
		if _, err := r.ReadBytes('\n'); err != nil {
			t.Fatal(err)
		}
		reading, finished := make(chan struct{}), make(chan struct{})
		go func() {
			close(reading)
			r.ReadBytes('\n') // blocks until Close below wakes it
			close(finished)
		}()
		<-reading
		c.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("reader never woke")
		}
	}
	if n := w.finish(); n != 0 {
		t.Fatalf("%d stray closes", n)
	}
}
