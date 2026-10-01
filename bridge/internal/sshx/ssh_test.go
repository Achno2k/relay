package sshx

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Achno2k/agents-cli/internal/config"
)

func withAgentsHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AGENTS_HOME", dir)
	return dir
}

func joined(argv []string) string { return strings.Join(argv, " ") }

func hasPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

func optValue(argv []string, prefix string) (string, bool) {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "-o" && strings.HasPrefix(argv[i+1], prefix) {
			return strings.TrimPrefix(argv[i+1], prefix), true
		}
	}
	return "", false
}

func TestArgsSSHTransport(t *testing.T) {
	home := withAgentsHome(t)
	tg := Target{Host: "ec2-1-2-3-4.compute.amazonaws.com", User: "ubuntu", Transport: "ssh", KeyPath: "/keys/box.pem"}
	argv := New(tg).Args()

	if argv[0] != "ssh" {
		t.Fatalf("argv[0] = %q", argv[0])
	}
	if last := argv[len(argv)-1]; last != "ubuntu@ec2-1-2-3-4.compute.amazonaws.com" {
		t.Errorf("destination = %q", last)
	}
	if !hasPair(argv, "-i", "/keys/box.pem") {
		t.Errorf("missing key flag in %s", joined(argv))
	}
	if _, ok := optValue(argv, "ProxyCommand="); ok {
		t.Errorf("ssh transport must not set ProxyCommand: %s", joined(argv))
	}
	cp, ok := optValue(argv, "ControlPath=")
	if !ok {
		t.Fatalf("missing ControlPath in %s", joined(argv))
	}
	if want := filepath.Join(home, "cm"); filepath.Dir(cp) != want {
		t.Errorf("ControlPath dir = %q, want %q", filepath.Dir(cp), want)
	}
	for _, want := range []string{"ControlMaster=auto", "ControlPersist=300"} {
		if !hasPair(argv, "-o", want) {
			t.Errorf("missing -o %s in %s", want, joined(argv))
		}
	}
}

func TestArgsSSMTransportUsesInstanceIDAndProxy(t *testing.T) {
	withAgentsHome(t)
	tg := Target{
		Host: "10.0.0.5", User: "ubuntu", Transport: "ssm",
		InstanceID: "i-0abc123", Profile: "work", Region: "ap-south-1",
		KeyPath: "/keys/box.pem",
	}
	argv := New(tg).Args()

	if last := argv[len(argv)-1]; last != "ubuntu@i-0abc123" {
		t.Errorf("ssm destination = %q, want ubuntu@i-0abc123", last)
	}
	pc, ok := optValue(argv, "ProxyCommand=")
	if !ok {
		t.Fatalf("missing ProxyCommand in %s", joined(argv))
	}
	want := "aws ssm start-session --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p"
	if !strings.HasPrefix(pc, want) {
		t.Errorf("ProxyCommand = %q, want prefix %q", pc, want)
	}
	if !strings.Contains(pc, "--profile work") || !strings.Contains(pc, "--region ap-south-1") {
		t.Errorf("ProxyCommand missing profile/region: %q", pc)
	}
	if !hasPair(argv, "-i", "/keys/box.pem") {
		t.Errorf("ssm still needs the ssh key: %s", joined(argv))
	}
}

func TestDefaultsUserAndTransport(t *testing.T) {
	withAgentsHome(t)
	argv := New(Target{Host: "box"}).Args()
	if last := argv[len(argv)-1]; last != "ubuntu@box" {
		t.Errorf("destination = %q, want ubuntu@box", last)
	}
	if _, ok := optValue(argv, "ProxyCommand="); ok {
		t.Error("empty transport must default to ssh")
	}
}

func TestInteractiveArgvPutsTTYBeforeDestination(t *testing.T) {
	withAgentsHome(t)
	tg := normalize(Target{Host: "box", User: "ubuntu"})

	argv := interactiveArgv(tg, "herdr")
	if argv[1] != "-t" {
		t.Errorf("argv[1] = %q, want -t", argv[1])
	}
	if last := argv[len(argv)-1]; last != "herdr" {
		t.Errorf("last arg = %q, want herdr", last)
	}
	if argv[len(argv)-2] != "ubuntu@box" {
		t.Errorf("destination should sit before the command: %s", joined(argv))
	}

	bare := interactiveArgv(tg, "  ")
	if last := bare[len(bare)-1]; last != "ubuntu@box" {
		t.Errorf("blank command should give a login shell, got %q", last)
	}
}

func TestScpArgv(t *testing.T) {
	withAgentsHome(t)
	tg := normalize(Target{Host: "box", User: "ubuntu", KeyPath: "/keys/box.pem"})
	argv := scpArgv(tg, "/tmp/bootstrap.sh", "/home/ubuntu/bootstrap.sh")

	if argv[0] != "scp" {
		t.Fatalf("argv[0] = %q", argv[0])
	}
	if argv[len(argv)-2] != "/tmp/bootstrap.sh" {
		t.Errorf("local path = %q", argv[len(argv)-2])
	}
	if want := "ubuntu@box:/home/ubuntu/bootstrap.sh"; argv[len(argv)-1] != want {
		t.Errorf("remote target = %q, want %q", argv[len(argv)-1], want)
	}
	if !hasPair(argv, "-i", "/keys/box.pem") {
		t.Errorf("scp missing key: %s", joined(argv))
	}
}

func TestControlPathIsStableAndPerTarget(t *testing.T) {
	withAgentsHome(t)
	a := Target{Host: "box", User: "ubuntu"}
	b := Target{Host: "box", User: "admin"}

	if ControlPath(a) != ControlPath(a) {
		t.Error("ControlPath is not stable")
	}
	if ControlPath(a) == ControlPath(b) {
		t.Error("different users must not share a control socket")
	}
	if got := filepath.Base(ControlPath(a)); len(got) != 12 {
		t.Errorf("socket name %q should be 12 chars to stay under the unix path limit", got)
	}
	if ControlDir() != filepath.Join(config.Dir(), "cm") {
		t.Errorf("ControlDir = %q", ControlDir())
	}
}

func TestTargetFromConfig(t *testing.T) {
	withAgentsHome(t)
	got := TargetFromConfig(
		config.AWS{Profile: "work", Region: "ap-south-1", InstanceID: "i-0abc123"},
		config.Box{Host: "box.example.com", User: "", Transport: "", KeyPath: "~/k.pem"},
	)
	want := Target{
		Host: "box.example.com", User: "ubuntu", Transport: "ssh", KeyPath: "~/k.pem",
		InstanceID: "i-0abc123", Profile: "work", Region: "ap-south-1",
	}
	if got != want {
		t.Errorf("TargetFromConfig = %+v, want %+v", got, want)
	}
}

func TestExpandHome(t *testing.T) {
	withAgentsHome(t)
	got := expandHome("~/keys/box.pem")
	if strings.HasPrefix(got, "~") {
		t.Errorf("expandHome left a tilde: %q", got)
	}
	if p := expandHome("/abs/box.pem"); p != "/abs/box.pem" {
		t.Errorf("absolute path changed: %q", p)
	}
}
