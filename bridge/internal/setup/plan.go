package setup

import (
	"reflect"
	"strings"
)

// Harnesses are the agent CLIs pair hooks into herdr, in order.
var Harnesses = []string{"claude", "codex", "pi"}

// State is what pair finds on the machine before it changes anything.
type State struct {
	Root       bool // running as root: no sudo needed
	SudoNoPass bool // `sudo -n true` works

	HerdrPath    string // "" when herdr isn't installed
	HerdrAnswers bool   // a herdr server answers on the socket

	Harnesses    []string          // the Harnesses found on PATH, the mise shims, ~/.local/bin or npm's global bin
	Integrations map[string]string // `herdr integration status`: kind -> "current (v9)", "not installed", …

	TailscalePath  string // "" when tailscale isn't installed
	TailscaleState string // BackendState ("Running", "NeedsLogin", "Stopped", …); "" when tailscaled doesn't answer

	LegacyUserUnit bool   // a round-9 ~/.config/systemd/user/relay.service exists
	RelayUnit      string // current /etc/systemd/system/relay.service, "" when missing
	RelayEnabled   bool
	RelayActive    bool
	RelayExe       string // /proc/<MainPID>/exe of the running service, "" when unknown
}

// Plan is what pair is about to do. The zero Plan does nothing.
type Plan struct {
	SudoPrompt   bool // ask for the sudo password once
	InstallHerdr bool
	HerdrUnit    bool // write and start herdr-server.service
	// Integrations to install with `herdr integration install <kind>`. Without one herdr
	// reports agent_session null and the bridge never finds the agent's transcript.
	Integrations     []string
	InstallTailscale bool
	StartTailscaled  bool
	TailscaleUp      bool
	RemoveLegacyUnit bool
	WriteRelayUnit   bool
	EnableRelay      bool
	RestartRelay     bool
}

// Empty reports whether the machine is already set up.
func (p Plan) Empty() bool { return reflect.DeepEqual(p, Plan{}) }

// NeedsSudo reports whether any step runs as root.
func (p Plan) NeedsSudo() bool {
	q := p
	// The user unit and the integrations live in the user's home and need no root.
	q.SudoPrompt, q.RemoveLegacyUnit, q.Integrations = false, false, nil
	return !q.Empty()
}

// Steps names what the plan will do, in order, for the confirmation.
func (p Plan) Steps() []string {
	var s []string
	add := func(on bool, what string) {
		if on {
			s = append(s, what)
		}
	}
	add(p.InstallHerdr, "install herdr to "+DefaultHerdr)
	add(p.HerdrUnit, "install and start "+HerdrUnitName)
	add(len(p.Integrations) > 0, "install the herdr integration for "+strings.Join(p.Integrations, ", "))
	add(p.InstallTailscale, "install Tailscale (tailscale.com/install.sh)")
	add(p.StartTailscaled, "start tailscaled")
	add(p.TailscaleUp, "log in to Tailscale (tailscale up)")
	add(p.RemoveLegacyUnit, "remove the old systemd user unit ~/.config/systemd/user/relay.service")
	add(p.WriteRelayUnit, "write "+UnitPath(RelayUnitName))
	add(p.EnableRelay, "enable "+RelayUnitName)
	add(p.RestartRelay, "start "+RelayUnitName)
	return s
}

// Decide works out the plan from what is on the machine. wantUnit is the
// relay.service pair would write and exe the binary it should run.
func Decide(st State, wantUnit, exe string) Plan {
	var p Plan
	p.InstallHerdr = st.HerdrPath == ""
	// A herdr server somebody already runs (a unit of their own, or herdr started by
	// hand) owns the socket; a second one would fight it for it.
	p.HerdrUnit = !st.HerdrAnswers
	for _, kind := range st.Harnesses {
		if !IntegrationCurrent(st.Integrations[kind]) {
			p.Integrations = append(p.Integrations, kind)
		}
	}

	p.InstallTailscale = st.TailscalePath == ""
	switch {
	case p.InstallTailscale:
		// The install script starts tailscaled; a fresh install is always logged out.
		p.TailscaleUp = true
	case st.TailscaleState == "":
		p.StartTailscaled, p.TailscaleUp = true, true
	case st.TailscaleState != "Running":
		p.TailscaleUp = true
	}

	p.RemoveLegacyUnit = st.LegacyUserUnit
	p.WriteRelayUnit = st.RelayUnit != wantUnit
	p.EnableRelay = !st.RelayEnabled
	p.RestartRelay = !st.RelayActive || p.WriteRelayUnit || p.RemoveLegacyUnit || BinaryChanged(st.RelayExe, exe)

	p.SudoPrompt = !st.Root && !st.SudoNoPass && p.NeedsSudo()
	return p
}

// BinaryChanged reports whether the running service is an older binary than exe.
// running is the /proc/<pid>/exe link: the kernel appends " (deleted)" once the
// file it ran was replaced, which is how an upgrade in place shows up. An unknown
// running binary ("") counts as unchanged so a second run never restarts for nothing.
func BinaryChanged(running, exe string) bool {
	if running == "" {
		return false
	}
	return strings.HasSuffix(running, " (deleted)") || running != exe
}

// IntegrationCurrent reports whether a `herdr integration status` entry needs no
// install: "current (v9)". "not installed", "outdated …" or no entry at all do.
func IntegrationCurrent(status string) bool { return strings.HasPrefix(status, "current") }
