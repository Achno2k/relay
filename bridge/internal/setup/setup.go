package setup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"relay/internal/config"
	"relay/internal/herdr"
)

// UI is how setup talks to the person running `relay pair`.
type UI interface {
	Info(string)
	Success(string)
	Warn(string)
	// Confirm asks before changing the machine.
	Confirm(title string, steps []string) (bool, error)
	// Login shows the Tailscale login link (as a QR code).
	Login(url string)
}

// Options are pair's flags.
type Options struct {
	Port    int
	AuthKey string // Tailscale auth key: skips the browser login
	Yes     bool   // no confirmation
	Relay   string // this binary, resolved
}

// ErrDeclined is returned when the user says no to the plan.
var ErrDeclined = errors.New("nothing changed")

// Supported reports whether this machine runs systemd, which pair needs to set it up.
func Supported() bool {
	_, err := exec.LookPath("systemctl")
	return runtime.GOOS == "linux" && err == nil && fileExists("/run/systemd/system")
}

// HealthTimeout is how long Run waits for the bridge to answer once started.
var HealthTimeout = 45 * time.Second

// Run brings the machine up to a paired Relay box and returns its Tailscale IPv4.
func Run(ctx context.Context, ui UI, o Options) (string, error) {
	u, err := currentUser()
	if err != nil {
		return "", err
	}
	s := &sys{ui: ui, root: os.Geteuid() == 0}
	st := s.survey(ctx)
	params := UnitParams{User: u.name, Home: u.home, Relay: o.Relay, Herdr: st.HerdrPath, Port: o.Port, Socket: os.Getenv("HERDR_SOCKET_PATH")}
	want, err := Unit(RelayUnitName, params)
	if err != nil {
		return "", err
	}
	p := Decide(st, want, o.Relay)

	if !p.Empty() && !o.Yes {
		ok, err := ui.Confirm("relay pair will set up this machine:", p.Steps())
		if err != nil {
			return "", err
		}
		if !ok {
			return "", ErrDeclined
		}
	}

	if p.SudoPrompt {
		ui.Info("sudo needs your password once")
		if err := s.interactive(ctx, "sudo", "-v"); err != nil {
			return "", fmt.Errorf("sudo: %w", err)
		}
	}
	if p.NeedsSudo() && !s.root {
		stop := s.keepSudo(ctx)
		defer stop()
	}

	if p.InstallHerdr {
		if err := s.installHerdr(ctx); err != nil {
			return "", err
		}
		params.Herdr = DefaultHerdr
	} else {
		ui.Success("herdr found at " + st.HerdrPath)
	}

	if p.HerdrUnit {
		if err := s.startHerdrServer(ctx, params); err != nil {
			return "", err
		}
	} else {
		ui.Success("a herdr server already answers on " + herdr.DefaultSocketPath())
	}

	if err := s.tailscale(ctx, p, o.AuthKey); err != nil {
		return "", err
	}
	ip := config.TailscaleIPv4()
	if ip == "" {
		return "", errors.New("tailscale is up but `tailscale ip -4` gave no address")
	}
	if !p.InstallTailscale && !p.TailscaleUp {
		ui.Success("Tailscale is up: " + ip)
	}

	if p.RemoveLegacyUnit {
		s.removeLegacyUnit(ctx)
	}
	if err := s.relayService(ctx, p, want); err != nil {
		return "", err
	}
	if err := waitHealth(ctx, ip, o.Port, HealthTimeout); err != nil {
		return "", err
	}
	ui.Success(fmt.Sprintf("%s answers on http://%s:%d", RelayUnitName, ip, o.Port))
	return ip, nil
}

type userInfo struct{ name, home string }

func currentUser() (userInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return userInfo{}, err
	}
	name := os.Getenv("USER")
	if name == "" {
		out, err := exec.Command("id", "-un").Output()
		if err != nil {
			return userInfo{}, fmt.Errorf("who am I: %w", err)
		}
		name = strings.TrimSpace(string(out))
	}
	return userInfo{name, home}, nil
}

// sys runs the steps on this machine.
type sys struct {
	ui   UI
	root bool
}

func (s *sys) survey(ctx context.Context) State {
	st := State{Root: s.root}
	st.SudoNoPass = s.root || exec.CommandContext(ctx, "sudo", "-n", "true").Run() == nil
	st.HerdrPath = findHerdr()
	st.HerdrAnswers = herdrAnswers(ctx)
	st.TailscalePath = config.TailscaleBinary()
	if st.TailscalePath != "" {
		st.TailscaleState = tailscaleState(ctx, st.TailscalePath)
	}
	st.LegacyUserUnit = fileExists(legacyUserUnit())
	if b, err := os.ReadFile(UnitPath(RelayUnitName)); err == nil {
		st.RelayUnit = string(b)
	}
	st.RelayEnabled = systemctlIs(ctx, "is-enabled", RelayUnitName) == "enabled"
	st.RelayActive = systemctlIs(ctx, "is-active", RelayUnitName) == "active"
	if st.RelayActive {
		st.RelayExe = mainExe(ctx, RelayUnitName)
	}
	return st
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func legacyUserUnit() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config/systemd/user", RelayUnitName)
}

func findHerdr() string {
	if p, err := exec.LookPath("herdr"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{DefaultHerdr, filepath.Join(home, ".local/bin/herdr")} {
		if fi, err := os.Stat(p); err == nil && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// herdrAnswers reports whether anything speaks herdr's protocol on the socket.
// An error reply still means a server is there.
func herdrAnswers(ctx context.Context) bool {
	err := herdr.NewClient("").Call(ctx, "ping", map[string]any{}, nil, 2*time.Second)
	var he *herdr.Error
	if errors.As(err, &he) {
		return he.Kind == herdr.ErrRemote || he.Kind == herdr.ErrDecoding
	}
	return err == nil
}

func tailscaleState(ctx context.Context, bin string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Exits non-zero when logged out but still prints the JSON.
	out, _ := exec.CommandContext(ctx, bin, "status", "--json").Output()
	return TailscaleBackendState(out)
}

func systemctlIs(ctx context.Context, verb, unit string) string {
	out, _ := exec.CommandContext(ctx, "systemctl", verb, unit).Output()
	return strings.TrimSpace(string(out))
}

// mainExe is the binary the unit's main process runs, "" when it can't be read.
func mainExe(ctx context.Context, unit string) string {
	out, err := exec.CommandContext(ctx, "systemctl", "show", "-p", "MainPID", "--value", unit).Output()
	if err != nil {
		return ""
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return ""
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return exe
}

// sudo runs a command as root, stdout and stderr captured into the error.
func (s *sys) sudo(ctx context.Context, stdin io.Reader, args ...string) error {
	if !s.root {
		args = append([]string{"sudo"}, args...)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = stdin
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(out.String()); msg != "" {
			return fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// interactive runs a command on the terminal.
func (s *sys) interactive(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// keepSudo refreshes sudo's timestamp while setup runs, so a slow Tailscale
// login doesn't make it ask again.
func (s *sys) keepSudo(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = exec.CommandContext(ctx, "sudo", "-n", "-v").Run()
			}
		}
	}()
	return func() { cancel(); wg.Wait() }
}

// writeRoot writes a root-owned file when its content differs. It reports whether it wrote.
func (s *sys) writeRoot(ctx context.Context, path, content string) (bool, error) {
	if b, err := os.ReadFile(path); err == nil && string(b) == content {
		return false, nil
	}
	if err := s.sudo(ctx, strings.NewReader(content), "tee", path); err != nil {
		return false, err
	}
	return true, s.sudo(ctx, nil, "chmod", "0644", path)
}

func (s *sys) installHerdr(ctx context.Context) error {
	rel, err := fetchHerdrRelease(ctx)
	if err != nil {
		return err
	}
	s.ui.Info("downloading herdr " + rel.Version)
	tmp, err := os.CreateTemp("", "herdr-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := download(ctx, rel.URL, tmp, 5*time.Minute); err != nil {
		tmp.Close()
		return fmt.Errorf("download herdr: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := verifySHA256(tmp.Name(), rel.SHA256); err != nil {
		return fmt.Errorf("herdr: %w", err)
	}
	if err := s.sudo(ctx, nil, "install", "-m", "0755", tmp.Name(), DefaultHerdr); err != nil {
		return err
	}
	s.ui.Success("installed herdr " + rel.Version + " to " + DefaultHerdr)
	return nil
}

func fetchHerdrRelease(ctx context.Context) (HerdrRelease, error) {
	var buf bytes.Buffer
	if err := download(ctx, HerdrManifestURL, &buf, 30*time.Second); err != nil {
		return HerdrRelease{}, fmt.Errorf("cannot reach %s: %w", HerdrManifestURL, err)
	}
	return ParseHerdrManifest(buf.Bytes(), runtime.GOARCH)
}

func download(ctx context.Context, url string, w io.Writer, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch (want %s, got %s)", want, got)
	}
	return nil
}

func (s *sys) startHerdrServer(ctx context.Context, p UnitParams) error {
	if p.Herdr == "" {
		p.Herdr = DefaultHerdr
	}
	unit, err := Unit(HerdrUnitName, p)
	if err != nil {
		return err
	}
	wrote, err := s.writeRoot(ctx, UnitPath(HerdrUnitName), unit)
	if err != nil {
		return err
	}
	if wrote {
		if err := s.sudo(ctx, nil, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if err := s.sudo(ctx, nil, "systemctl", "enable", HerdrUnitName); err != nil {
		return err
	}
	if err := s.sudo(ctx, nil, "systemctl", "restart", HerdrUnitName); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for !herdrAnswers(ctx) {
		if time.Now().After(deadline) {
			// The bridge runs without herdr and picks it up later, so this isn't fatal.
			s.ui.Warn(HerdrUnitName + " started but herdr doesn't answer yet; check `systemctl status " + HerdrUnitName + "`")
			return nil
		}
		if err := sleep(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
	s.ui.Success("started " + HerdrUnitName)
	return nil
}

func (s *sys) tailscale(ctx context.Context, p Plan, authKey string) error {
	if p.InstallTailscale {
		if _, err := exec.LookPath("curl"); err != nil {
			return errors.New("installing Tailscale needs curl")
		}
		s.ui.Info("installing Tailscale")
		if err := s.interactive(ctx, "sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh"); err != nil {
			return fmt.Errorf("install Tailscale: %w", err)
		}
		if config.TailscaleBinary() == "" {
			return errors.New("the Tailscale installer finished but tailscale isn't on PATH")
		}
		s.ui.Success("installed Tailscale")
	}
	bin := config.TailscaleBinary()
	state := tailscaleState(ctx, bin)
	if state == "" {
		if err := s.sudo(ctx, nil, "systemctl", "enable", "--now", "tailscaled"); err != nil {
			return err
		}
		if state, _ = waitFor(ctx, 15*time.Second, func() (string, bool) {
			st := tailscaleState(ctx, bin)
			return st, st != ""
		}); state == "" {
			return errors.New("tailscaled doesn't answer; check `systemctl status tailscaled`")
		}
	}
	if state != "Running" {
		if err := s.tailscaleUp(ctx, bin, authKey); err != nil {
			return err
		}
	}
	if ip, ok := waitFor(ctx, 30*time.Second, func() (string, bool) {
		ip := config.TailscaleIPv4()
		return ip, ip != ""
	}); ok && (p.InstallTailscale || p.TailscaleUp) {
		s.ui.Success("Tailscale is up: " + ip)
	}
	return nil
}

// tailscaleUp logs in. Without an auth key `tailscale up` prints a login URL and
// blocks until someone opens it; the URL is shown as a QR code for the phone.
func (s *sys) tailscaleUp(ctx context.Context, bin, authKey string) error {
	args := []string{bin, "up"}
	if authKey != "" {
		// file: keeps the key off the process list.
		f, err := os.CreateTemp("", "ts-authkey-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.WriteString(authKey); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		args = append(args, "--auth-key=file:"+f.Name())
	} else {
		s.ui.Info("log this machine in to Tailscale: open the link or scan the QR code")
	}
	if !s.root {
		args = append([]string{"sudo"}, args...)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	var tail []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		shown := ""
		for sc.Scan() {
			line := sc.Text()
			if u := AuthURL(line); u != "" {
				if u != shown {
					shown = u
					s.ui.Login(u)
				}
				continue
			}
			if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "To authenticate") {
				tail = append(tail, t)
			}
		}
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("tailscale up: %w\n%s", err, strings.Join(tail, "\n"))
	}
	return nil
}

func (s *sys) removeLegacyUnit(ctx context.Context) {
	// Best effort: without a user session bus the stop fails, the file still goes.
	_ = exec.CommandContext(ctx, "systemctl", "--user", "disable", "--now", RelayUnitName).Run()
	if err := os.Remove(legacyUserUnit()); err != nil && !os.IsNotExist(err) {
		s.ui.Warn("could not remove " + legacyUserUnit() + ": " + err.Error())
		return
	}
	_ = exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload").Run()
	s.ui.Success("removed the old systemd user unit")
}

func (s *sys) relayService(ctx context.Context, p Plan, unit string) error {
	// The unit appends to ~/.relay/relay.log; create it as the user, not root.
	if err := config.EnsureHome(); err != nil {
		return err
	}
	if f, err := os.OpenFile(config.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		f.Close()
	}
	if _, err := config.LoadToken(); err != nil {
		return err
	}
	if p.WriteRelayUnit {
		if _, err := s.writeRoot(ctx, UnitPath(RelayUnitName), unit); err != nil {
			return err
		}
		if err := s.sudo(ctx, nil, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		s.ui.Success("wrote " + UnitPath(RelayUnitName))
	}
	if p.EnableRelay {
		if err := s.sudo(ctx, nil, "systemctl", "enable", RelayUnitName); err != nil {
			return err
		}
	}
	if p.RestartRelay {
		if err := s.sudo(ctx, nil, "systemctl", "restart", RelayUnitName); err != nil {
			return err
		}
		s.ui.Success("started " + RelayUnitName)
	}
	return nil
}

// waitHealth polls the bridge's /health on the Tailscale IP until it answers 200.
func waitHealth(ctx context.Context, ip string, port int, timeout time.Duration) error {
	url := fmt.Sprintf("http://%s:%d/health", ip, port)
	client := &http.Client{Timeout: 2 * time.Second}
	var last string
	_, ok := waitFor(ctx, timeout, func() (string, bool) {
		resp, err := client.Get(url)
		if err != nil {
			last = err.Error()
			return "", false
		}
		resp.Body.Close()
		last = resp.Status
		return "", resp.StatusCode == http.StatusOK
	})
	if !ok {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("%s doesn't answer on %s after %s (%s)\ncheck: systemctl status %s; tail %s",
			RelayUnitName, url, timeout, last, RelayUnitName, config.LogPath())
	}
	return nil
}

// waitFor polls f every half second until it says done or timeout passes.
func waitFor(ctx context.Context, timeout time.Duration, f func() (string, bool)) (string, bool) {
	deadline := time.Now().Add(timeout)
	for {
		v, ok := f()
		if ok {
			return v, true
		}
		if time.Now().After(deadline) || sleep(ctx, 500*time.Millisecond) != nil {
			return v, false
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// InstallRelayUnit writes relay.service for the current user and reloads
// systemd. It enables nothing. It reports the unit's path.
func InstallRelayUnit(ctx context.Context, exe string, port int) (string, error) {
	u, err := currentUser()
	if err != nil {
		return "", err
	}
	unit, err := Unit(RelayUnitName, UnitParams{User: u.name, Home: u.home, Relay: exe, Port: port, Socket: os.Getenv("HERDR_SOCKET_PATH")})
	if err != nil {
		return "", err
	}
	if err := config.EnsureHome(); err != nil {
		return "", err
	}
	s := &sys{root: os.Geteuid() == 0}
	path := UnitPath(RelayUnitName)
	if _, err := s.writeRoot(ctx, path, unit); err != nil {
		return "", err
	}
	return path, s.sudo(ctx, nil, "systemctl", "daemon-reload")
}
