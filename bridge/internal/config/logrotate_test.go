package config

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
)

func size(t *testing.T, p string) int64 {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

func TestTruncatesOnceOverTheCapAndLeavesSmallFilesAlone(t *testing.T) {
	path := fmt.Sprintf("/tmp/relay-log-%08x.log", rand.Uint32())
	defer os.Remove(path)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Our own fd on it, standing in for launchd's stdout/stderr redirection.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	f.Write(bytes.Repeat([]byte("x"), 100))
	r := &LogRotator{Path: path, MaxBytes: 1024, FDs: []int{int(f.Fd())}}
	if r.RotateIfNeeded() {
		t.Fatal("rotated a small file")
	}
	if size(t, path) != 100 {
		t.Fatal("small file changed")
	}

	f.Write(bytes.Repeat([]byte("y"), 2000))
	if !r.RotateIfNeeded() {
		t.Fatal("did not rotate")
	}
	if size(t, path) != 0 {
		t.Fatal("not truncated")
	}

	// The fd still works after truncation: a later write lands at offset 0, not after a hole.
	f.Write(bytes.Repeat([]byte("z"), 10))
	if got := size(t, path); got != 10 {
		t.Fatalf("size after write = %d", got)
	}
}

func TestMissingFileIsANoOp(t *testing.T) {
	r := &LogRotator{Path: fmt.Sprintf("/tmp/relay-log-does-not-exist-%x.log", rand.Uint64()), MaxBytes: 10}
	if r.RotateIfNeeded() {
		t.Fatal("rotated a missing file")
	}
}
