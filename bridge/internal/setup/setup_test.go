package setup

import (
	"reflect"
	"strings"
	"testing"
)

var params = UnitParams{User: "dev", Home: "/home/dev", Relay: "/usr/local/bin/relay", Port: 7878}

func TestRelayUnit(t *testing.T) {
	u, err := Unit(RelayUnitName, params)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"User=dev\n",
		"WorkingDirectory=/home/dev\n",
		"Environment=HOME=/home/dev\n",
		"Environment=PATH=/home/dev/.local/share/mise/shims:/home/dev/.local/bin:/home/dev/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin\n",
		"ExecStart=/usr/local/bin/relay serve --port 7878 --require-tailscale\n",
		"After=network-online.target herdr-server.service tailscaled.service\n",
		"Restart=always\n",
		"RestartSec=5\n",
		"StandardOutput=append:/home/dev/.relay/relay.log\n",
		"StandardError=append:/home/dev/.relay/relay.log\n",
		"WantedBy=multi-user.target\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("relay.service lacks %q\n%s", want, u)
		}
	}
	for _, bad := range []string{"HERDR_SOCKET_PATH", "{{", "<no value>", "default.target", "dangerously"} {
		if strings.Contains(u, bad) {
			t.Errorf("relay.service has %q\n%s", bad, u)
		}
	}
}

func TestHerdrUnit(t *testing.T) {
	u, err := Unit(HerdrUnitName, params)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"User=dev\n",
		"ExecStart=/usr/local/bin/herdr server\n",
		"ExecStop=/usr/local/bin/herdr server stop\n",
		"Restart=always\n",
		"WantedBy=multi-user.target\n",
		"`herdr --remote dev@<host>`",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("herdr-server.service lacks %q\n%s", want, u)
		}
	}
	p := params
	p.Herdr = "/home/dev/.local/bin/herdr"
	if u, _ := Unit(HerdrUnitName, p); !strings.Contains(u, "ExecStart=/home/dev/.local/bin/herdr server\n") {
		t.Errorf("herdr path not used\n%s", u)
	}
}

func TestUnitQuotingAndSocket(t *testing.T) {
	p := params
	p.Relay = "/opt/my relay/relay"
	p.Socket = "/run/herdr/h.sock"
	u, err := Unit(RelayUnitName, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ExecStart="/opt/my relay/relay" serve`,
		"Environment=HERDR_SOCKET_PATH=/run/herdr/h.sock\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("relay.service lacks %q\n%s", want, u)
		}
	}
}

func TestUnitRejects(t *testing.T) {
	for name, p := range map[string]UnitParams{
		"no user":    {Home: "/home/dev", Relay: "/r", Port: 1},
		"space home": {User: "dev", Home: "/home/my dev", Relay: "/r", Port: 1},
		"no relay":   {User: "dev", Home: "/home/dev", Port: 1},
		"no port":    {User: "dev", Home: "/home/dev", Relay: "/r"},
	} {
		if _, err := Unit(RelayUnitName, p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Unit("nope.service", params); err == nil {
		t.Error("unknown unit rendered")
	}
}

// A machine where pair already ran.
func setUp(t *testing.T) (State, string) {
	t.Helper()
	want, err := Unit(RelayUnitName, params)
	if err != nil {
		t.Fatal(err)
	}
	return State{
		SudoNoPass:     true,
		HerdrPath:      DefaultHerdr,
		HerdrAnswers:   true,
		TailscalePath:  "/usr/bin/tailscale",
		TailscaleState: "Running",
		RelayUnit:      want,
		RelayEnabled:   true,
		RelayActive:    true,
		RelayExe:       params.Relay,
		Harnesses:      []string{"claude", "codex", "pi"},
		Integrations:   map[string]string{"claude": "current (v9)", "codex": "current (v8)", "pi": "current (v8)", "omp": "not installed"},
	}, want
}

func TestDecideSecondRunDoesNothing(t *testing.T) {
	st, want := setUp(t)
	if p := Decide(st, want, params.Relay); !p.Empty() {
		t.Errorf("second run plans %+v", p)
	}
	st.SudoNoPass = false
	if p := Decide(st, want, params.Relay); !p.Empty() {
		t.Errorf("second run asks for sudo: %+v", p)
	}
}

func TestDecideFreshMachine(t *testing.T) {
	_, want := setUp(t)
	p := Decide(State{Harnesses: []string{"claude", "pi"}}, want, params.Relay)
	exp := Plan{
		SudoPrompt: true, InstallHerdr: true, HerdrUnit: true, Integrations: []string{"claude", "pi"},
		InstallTailscale: true, TailscaleUp: true,
		WriteRelayUnit: true, EnableRelay: true, RestartRelay: true,
	}
	if !reflect.DeepEqual(p, exp) {
		t.Errorf("got %+v\nwant %+v", p, exp)
	}
	if got := len(p.Steps()); got != 8 {
		t.Errorf("%d steps: %q", got, p.Steps())
	}
}

func TestDecideEachStep(t *testing.T) {
	cases := map[string]struct {
		change func(*State)
		want   Plan
	}{
		"herdr missing, server down": {func(s *State) { s.HerdrPath, s.HerdrAnswers = "", false },
			Plan{InstallHerdr: true, HerdrUnit: true}},
		"herdr server down": {func(s *State) { s.HerdrAnswers = false }, Plan{HerdrUnit: true}},
		// Somebody runs herdr by hand: leave their server alone.
		"herdr by hand":        {func(s *State) {}, Plan{}},
		"tailscale logged out": {func(s *State) { s.TailscaleState = "NeedsLogin" }, Plan{TailscaleUp: true}},
		"tailscale stopped":    {func(s *State) { s.TailscaleState = "Stopped" }, Plan{TailscaleUp: true}},
		"tailscaled down":      {func(s *State) { s.TailscaleState = "" }, Plan{StartTailscaled: true, TailscaleUp: true}},
		"unit changed":         {func(s *State) { s.RelayUnit += "# edited\n" }, Plan{WriteRelayUnit: true, RestartRelay: true}},
		"unit disabled":        {func(s *State) { s.RelayEnabled = false }, Plan{EnableRelay: true}},
		"unit stopped":         {func(s *State) { s.RelayActive, s.RelayExe = false, "" }, Plan{RestartRelay: true}},
		"binary replaced":      {func(s *State) { s.RelayExe = params.Relay + " (deleted)" }, Plan{RestartRelay: true}},
		"binary moved":         {func(s *State) { s.RelayExe = "/home/dev/relay" }, Plan{RestartRelay: true}},
		"integration missing":  {func(s *State) { s.Integrations["codex"] = "not installed" }, Plan{Integrations: []string{"codex"}}},
		"integration outdated": {func(s *State) { s.Integrations["pi"] = "outdated (v7, latest v8)" }, Plan{Integrations: []string{"pi"}}},
		"herdr status failed":  {func(s *State) { s.Integrations = nil }, Plan{Integrations: []string{"claude", "codex", "pi"}}},
		// A harness that isn't installed gets no integration, current or not.
		"only claude": {func(s *State) {
			s.Harnesses = []string{"claude"}
			s.Integrations = map[string]string{"claude": "current (v9)"}
		}, Plan{}},
		"no harnesses":  {func(s *State) { s.Harnesses = nil }, Plan{}},
		"old user unit": {func(s *State) { s.LegacyUserUnit = true }, Plan{RemoveLegacyUnit: true, RestartRelay: true}},
	}
	for name, c := range cases {
		st, want := setUp(t)
		c.change(&st)
		if got := Decide(st, want, params.Relay); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v\nwant %+v", name, got, c.want)
		}
	}
}

func TestDecideSudo(t *testing.T) {
	st, want := setUp(t)
	st.SudoNoPass = false
	st.HerdrAnswers = false
	if p := Decide(st, want, params.Relay); !p.SudoPrompt {
		t.Error("needs root but doesn't ask for sudo")
	}
	st.Root = true
	if p := Decide(st, want, params.Relay); p.SudoPrompt {
		t.Error("root asked for sudo")
	}
	// Removing the user unit needs no root.
	st, want = setUp(t)
	st.SudoNoPass = false
	st.LegacyUserUnit = true
	st.RelayActive = true
	p := Decide(st, want, params.Relay)
	if !p.SudoPrompt {
		t.Error("restart after removing the user unit needs sudo")
	}
}

func TestIntegrationsNeedNoSudo(t *testing.T) {
	st, want := setUp(t)
	st.SudoNoPass = false
	st.Integrations = map[string]string{}
	p := Decide(st, want, params.Relay)
	if p.SudoPrompt || p.NeedsSudo() {
		t.Errorf("integrations asked for sudo: %+v", p)
	}
	if len(p.Integrations) != 3 || len(p.Steps()) != 1 {
		t.Errorf("plan %+v steps %q", p, p.Steps())
	}
}

func TestParseIntegrationStatus(t *testing.T) {
	out := "pi: current (v8) (/home/dev/.pi/agent/extensions/herdr-agent-state.ts)\n" +
		"claude: not installed (/home/dev/.claude/hooks/herdr-agent-state.sh)\n" +
		"codex: outdated (v7) (/home/dev/.codex/herdr-agent-state.sh)\n" +
		"\nusage: herdr integration status [--outdated-only]\n"
	got := ParseIntegrationStatus(out)
	want := map[string]string{"pi": "current (v8)", "claude": "not installed", "codex": "outdated (v7)"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if !IntegrationCurrent(got["pi"]) || IntegrationCurrent(got["claude"]) || IntegrationCurrent(got["codex"]) || IntegrationCurrent("") {
		t.Errorf("IntegrationCurrent wrong for %v", got)
	}
}

func TestLastLines(t *testing.T) {
	l := &lastLines{n: 2}
	l.Write([]byte("a\nb\nc\nd"))
	if got := l.lines(); !reflect.DeepEqual(got, []string{"b", "c", "d"}) {
		t.Errorf("%q", got)
	}
}

func TestBinaryChanged(t *testing.T) {
	for _, c := range []struct {
		running, exe string
		want         bool
	}{
		{"", "/usr/local/bin/relay", false},
		{"/usr/local/bin/relay", "/usr/local/bin/relay", false},
		{"/usr/local/bin/relay (deleted)", "/usr/local/bin/relay", true},
		{"/home/dev/relay", "/usr/local/bin/relay", true},
	} {
		if got := BinaryChanged(c.running, c.exe); got != c.want {
			t.Errorf("BinaryChanged(%q, %q) = %v", c.running, c.exe, got)
		}
	}
}

func TestParseHerdrManifest(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	m := []byte(`{"version":"0.9.1","assets":{"linux-x86_64":"https://dl.example/herdr-x86","linux-aarch64":"https://dl.example/herdr-arm"},` +
		`"sha256":{"linux-x86_64":"` + strings.ToUpper(sha) + `","linux-aarch64":"` + sha + `"}}`)
	got, err := ParseHerdrManifest(m, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if want := (HerdrRelease{"0.9.1", "https://dl.example/herdr-arm", sha}); !reflect.DeepEqual(got, want) {
		t.Errorf("arm64: %+v", got)
	}
	if got, _ := ParseHerdrManifest(m, "amd64"); got.SHA256 != sha || got.URL != "https://dl.example/herdr-x86" {
		t.Errorf("amd64: %+v", got)
	}
	for name, bad := range map[string][]byte{
		"not json":  []byte("<html>"),
		"no asset":  []byte(`{"version":"1","assets":{},"sha256":{}}`),
		"short sha": []byte(`{"version":"1","assets":{"linux-x86_64":"https://x"},"sha256":{"linux-x86_64":"abc"}}`),
		"http":      []byte(`{"version":"1","assets":{"linux-x86_64":"http://x"},"sha256":{"linux-x86_64":"` + sha + `"}}`),
	} {
		if _, err := ParseHerdrManifest(bad, "amd64"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseHerdrManifest(m, "386"); err == nil {
		t.Error("386 accepted")
	}
}

func TestTailscaleBackendState(t *testing.T) {
	for in, want := range map[string]string{
		`{"Version":"1.80.0","BackendState":"Running","Self":{}}`: "Running",
		`{"BackendState":"NeedsLogin","AuthURL":""}`:              "NeedsLogin",
		"failed to connect to local tailscaled":                   "",
		"":                                                        "",
	} {
		if got := TailscaleBackendState([]byte(in)); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestAuthURL(t *testing.T) {
	for in, want := range map[string]string{
		"\thttps://login.tailscale.com/a/0123abcd": "https://login.tailscale.com/a/0123abcd",
		"To authenticate, visit:":                  "",
		"Success.":                                 "",
		"see http://example.com":                   "",
	} {
		if got := AuthURL(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
