package bootstrap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// releaseServer serves a fake v1.2.3 release with one linux/arm64 asset.
func releaseServer(t *testing.T, sums func(sum string) string) {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho relay\n")
	for _, f := range []struct {
		name string
		data []byte
	}{{"README.md", []byte("readme")}, {"relay", body}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(f.data)
	}
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(archive.Bytes())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.2.3/checksums.txt":
			w.Write([]byte(sums(hex.EncodeToString(sum[:]))))
		case "/v1.2.3/relay_linux_arm64.tar.gz":
			w.Write(archive.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := ReleaseBase
	ReleaseBase = srv.URL
	t.Cleanup(func() { ReleaseBase = old })
}

func TestDownloadReleaseChecksTheSum(t *testing.T) {
	releaseServer(t, func(sum string) string {
		return strings.Repeat("0", 64) + "  relay_darwin_arm64.tar.gz\n" + sum + "  relay_linux_arm64.tar.gz\n"
	})
	path, err := DownloadRelease(context.Background(), "v1.2.3", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "echo relay") {
		t.Fatalf("extracted %q, %v", b, err)
	}
}

func TestDownloadReleaseRejectsBadSum(t *testing.T) {
	releaseServer(t, func(string) string { return strings.Repeat("a", 64) + "  relay_linux_arm64.tar.gz\n" })
	if _, err := DownloadRelease(context.Background(), "1.2.3", "arm64"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
}

func TestDownloadReleaseErrors(t *testing.T) {
	releaseServer(t, func(sum string) string { return sum + "  relay_linux_arm64.tar.gz\n" })
	if _, err := DownloadRelease(context.Background(), "1.2.3", "amd64"); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("missing asset: err = %v", err)
	}
	if _, err := DownloadRelease(context.Background(), "9.9.9", "arm64"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing release: err = %v", err)
	}
	for _, v := range []string{"", "dev", "0.2.0-snapshot", "v0.1.0-3-gabc-dirty"} {
		if _, err := DownloadRelease(context.Background(), v, "arm64"); err == nil || !strings.Contains(err.Error(), "RELAY_SRC") {
			t.Errorf("version %q: err = %v, want the RELAY_SRC hint", v, err)
		}
	}
}
