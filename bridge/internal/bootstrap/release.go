package bootstrap

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"relay/internal/api"
)

// ReleaseBase is where goreleaser publishes relay. A var so tests can point it
// at a local server.
var ReleaseBase = "https://github.com/Achno2k/relay/releases/download"

// BoxBinary returns a linux/<goarch> relay binary for the box:
//   - built from source when this binary can find its checkout (BuildSourceDir,
//     RELAY_SRC or a go.mod above it), as during development;
//   - this very binary when it is a release already built for linux/<goarch>;
//   - otherwise the matching asset of this version's release, checked against
//     its checksums.txt.
func BoxBinary(ctx context.Context, goarch string, log io.Writer) (string, error) {
	if goarch == "" {
		goarch = "amd64"
	}
	if _, err := ModuleDir(); err == nil {
		fmt.Fprintf(log, "building relay for linux/%s from source\n", goarch)
		return BuildForBox(ctx, goarch)
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == goarch {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(log, "using this relay binary (linux/%s)\n", goarch)
		return exe, nil
	}
	fmt.Fprintf(log, "downloading relay %s for linux/%s\n", api.Version, goarch)
	return DownloadRelease(ctx, api.Version, goarch)
}

// ReleaseAsset is the goreleaser archive name for linux/<goarch>.
func ReleaseAsset(goarch string) string { return "relay_linux_" + goarch + ".tar.gz" }

// DownloadRelease fetches relay_linux_<goarch>.tar.gz from the v<version>
// release, checks its sha256 against the release's checksums.txt, and returns
// the path of the extracted binary.
func DownloadRelease(ctx context.Context, version, goarch string) (string, error) {
	version = strings.TrimPrefix(version, "v")
	if version == "" || version == "dev" || strings.Contains(version, "snapshot") || strings.Contains(version, "dirty") {
		return "", fmt.Errorf("relay %q is not a release and cannot find its source; set RELAY_SRC to the bridge/ directory of a checkout", version)
	}
	base := ReleaseBase + "/v" + version + "/"
	asset := ReleaseAsset(goarch)

	sums, err := fetch(ctx, base+"checksums.txt")
	if err != nil {
		return "", err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return "", err
	}
	archive, err := fetch(ctx, base+asset)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return "", fmt.Errorf("%s checksum mismatch (want %s, got %s)", asset, want, got)
	}

	bin, err := untarRelay(archive)
	if err != nil {
		return "", fmt.Errorf("%s: %w", asset, err)
	}
	out := filepath.Join(os.TempDir(), "relay-linux-"+goarch+"-"+version)
	if err := os.WriteFile(out, bin, 0o755); err != nil {
		return "", err
	}
	return out, nil
}

// fetch GETs url and returns the body, failing on any non-200.
func fetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// checksumFor finds asset's sha256 in a goreleaser checksums.txt
// ("<hex>  <name>" per line).
func checksumFor(sums []byte, asset string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", asset)
}

// untarRelay returns the `relay` file from a .tar.gz.
func untarRelay(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("archive has no relay binary")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "relay" {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}
