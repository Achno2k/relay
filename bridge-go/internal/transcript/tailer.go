package transcript

import (
	"bytes"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"relay/internal/api"
)

// How often the tailer re-checks the file with fsnotify running (a safety net: Linux reports a
// delete as a chmod while the file is open), and without it (the polling fallback).
var (
	safetyPoll   = time.Second
	fallbackPoll = 250 * time.Millisecond
)

// Tailer watches one transcript file and emits messages that were created or grew.
// It reads only appended bytes; a partial trailing line waits for the rest. A truncated file
// is parsed again from the start. Once the file is deleted, renamed or replaced (rotation) the
// tailer is dead, and the owner recreates it for whatever the path now holds.
type Tailer struct {
	ref       Ref
	onMessage func(api.Message)

	mu   sync.Mutex
	dead bool

	// Only touched by Start before the loop runs, then by the loop.
	parser  *Parser
	file    *os.File
	offset  int64
	partial []byte
	// The next read starts mid-line (a huge file seeded from its tail).
	skipFragment bool

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	done      chan struct{}
}

// NewTailer: onMessage runs on the tailer's goroutine, one message at a time, and must not call
// Stop. uploads may be nil.
func NewTailer(ref Ref, uploads Uploads, onMessage func(api.Message)) *Tailer {
	return &Tailer{
		ref:       ref,
		onMessage: onMessage,
		parser:    NewParser(ref.Format, ref.Cwd, uploads),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

func (t *Tailer) Path() string { return t.ref.Path }

// Dead is true once the file was deleted, renamed or replaced, or couldn't be opened.
func (t *Tailer) Dead() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dead
}

func (t *Tailer) setDead() {
	t.mu.Lock()
	t.dead = true
	t.mu.Unlock()
}

// Start seeds the parser with what the file holds (nothing is emitted for it) and starts
// watching. Only the first call does anything, and none after Stop.
func (t *Tailer) Start() {
	t.startOnce.Do(t.start)
}

// For tests: force the polling fallback.
var noWatcher = false

func (t *Tailer) start() {
	f, err := os.Open(t.ref.Path)
	if err != nil {
		t.setDead()
		close(t.done)
		return
	}
	t.file = f
	t.seed()
	w, err := fsnotify.NewWatcher()
	if noWatcher && err == nil {
		w.Close()
		err = os.ErrInvalid
	}
	if err == nil {
		if err = w.Add(t.ref.Path); err != nil {
			w.Close()
			w = nil
		}
	} else {
		w = nil
	}
	go t.loop(w)
}

// Stop stops watching and closes the file. No callback runs after it returns.
func (t *Tailer) Stop() {
	t.startOnce.Do(func() { close(t.done) })
	t.stopOnce.Do(func() { close(t.stop) })
	<-t.done
	t.setDead()
}

func (t *Tailer) loop(w *fsnotify.Watcher) {
	defer close(t.done)
	defer t.file.Close()
	var events chan fsnotify.Event
	var errs chan error
	interval := fallbackPoll
	if w != nil {
		defer w.Close()
		events, errs = w.Events, w.Errors
		interval = safetyPoll
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			return
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
				t.setDead()
				return
			}
			if !t.check() {
				return
			}
		case _, ok := <-errs:
			if !ok {
				errs = nil
			}
		case <-tick.C:
			if !t.check() {
				return
			}
		}
	}
}

// check reads any growth, or marks the tailer dead when the path no longer holds our file.
func (t *Tailer) check() bool {
	now, err := os.Stat(t.ref.Path)
	if err != nil {
		t.setDead()
		return false
	}
	if ours, err := t.file.Stat(); err != nil || !os.SameFile(now, ours) {
		t.setDead()
		return false
	}
	for _, m := range t.readAppended() {
		select {
		case <-t.stop:
			return false
		default:
		}
		t.onMessage(m)
	}
	return true
}

// seed parses what exists without emitting it. Only the last MaxReadBytes are read (the same
// bound as /messages), and only the last message is kept, since only it can still grow.
func (t *Tailer) seed() {
	if info, err := t.file.Stat(); err == nil && info.Size() > MaxReadBytes {
		t.offset = info.Size() - MaxReadBytes
		t.skipFragment = true
	}
	t.readAppended()
}

// readAppended reads bytes past offset, feeds complete lines to the parser and returns the
// messages that changed.
func (t *Tailer) readAppended() []api.Message {
	info, err := t.file.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	if size < t.offset {
		// Truncated or replaced in place: start over.
		t.parser = NewParser(t.parser.format, t.parser.scrubber.cwd, t.parser.uploads)
		t.offset = 0
		t.partial = nil
		t.skipFragment = false
	}
	if size <= t.offset {
		return nil
	}
	buf := make([]byte, size-t.offset)
	n, err := t.file.ReadAt(buf, t.offset)
	if err != nil && err != io.EOF {
		return nil
	}
	t.offset += int64(n)
	t.partial = append(t.partial, buf[:n]...)
	if t.skipFragment {
		// Started mid-file: drop the tail of the line the cut landed in.
		i := bytes.IndexByte(t.partial, '\n')
		if i < 0 {
			t.partial = nil
			return nil
		}
		t.partial = t.partial[i+1:]
		t.skipFragment = false
	}
	nl := bytes.LastIndexByte(t.partial, '\n')
	if nl < 0 {
		return nil
	}
	changed := t.parser.ConsumeData(t.partial[:nl+1])
	t.partial = append([]byte(nil), t.partial[nl+1:]...)
	t.parser.DropSettled()
	return changed
}
