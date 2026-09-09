package bootstrap

import (
	"strings"
	"testing"
)

func TestRepoName(t *testing.T) {
	cases := map[string]string{
		"git@github.com:Achno2k/agents-cli.git":     "agents-cli",
		"https://github.com/Achno2k/agents-cli.git": "agents-cli",
		"https://github.com/Achno2k/agents-cli":     "agents-cli",
		"https://github.com/Achno2k/agents-cli/":    "agents-cli",
		"ssh://git@github.com/org/deep/repo.git":      "repo",
		"agents-cli":                                  "agents-cli",
	}
	for url, want := range cases {
		if got := RepoName(url); got != want {
			t.Errorf("RepoName(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestExpandHome(t *testing.T) {
	cases := map[string]string{
		"~/work":    "$HOME/work",
		"~":         "$HOME",
		"":          "$HOME/work",
		"/srv/work": "/srv/work",
		"~work":     "~work",
		"~/a/b/c":   "$HOME/a/b/c",
	}
	for in, want := range cases {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckoutDir(t *testing.T) {
	if got, want := CheckoutDir("~/work", "agents-cli"), "$HOME/work/agents-cli/main"; got != want {
		t.Errorf("CheckoutDir = %q, want %q", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":       "'plain'",
		"with space":  "'with space'",
		"it's":        `'it'\''s'`,
		"$(rm -rf /)": "'$(rm -rf /)'",
		"a'b'c":       `'a'\''b'\''c'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModulePathOf(t *testing.T) {
	gomod := "module github.com/Achno2k/agents-cli\n\ngo 1.24\n"
	if got := modulePathOf(gomod); got != ModulePath {
		t.Errorf("modulePathOf = %q, want %q", got, ModulePath)
	}
	if got := modulePathOf("go 1.24\n"); got != "" {
		t.Errorf("modulePathOf(no module) = %q, want empty", got)
	}
}

func TestHarnessTasks(t *testing.T) {
	got := HarnessTasks([]string{"claude", "Codex", "nope"})
	if len(got) != 2 {
		t.Fatalf("HarnessTasks returned %d tasks, want 2: %+v", len(got), got)
	}
	if got[0].Func != "bs_claude" || got[1].Func != "bs_codex" {
		t.Errorf("unexpected funcs: %+v", got)
	}
}

// Every task must name a function that bootstrap.sh actually defines.
func TestTasksExistInScript(t *testing.T) {
	tasks := append(BaseTasks(), HarnessTasks(KnownHarnesses())...)
	tasks = append(tasks, ProfileTask())
	for _, task := range tasks {
		if !strings.Contains(bootstrapSH, "\n"+task.Func+"()") {
			t.Errorf("bootstrap.sh defines no %s()", task.Func)
		}
		if !strings.HasSuffix(Script(task.Func), "\n"+task.Func+"\n") {
			t.Errorf("Script(%q) does not end with the call", task.Func)
		}
	}
}

func TestUnitsRender(t *testing.T) {
	for _, name := range Units {
		body, err := Unit(name, "ubuntu", "/home/ubuntu")
		if err != nil {
			t.Fatalf("Unit(%q): %v", name, err)
		}
		if strings.Contains(body, "{{") {
			t.Errorf("Unit(%q) left a template action unrendered", name)
		}
		for _, want := range []string{"User=ubuntu", "Restart=always", "[Install]"} {
			if !strings.Contains(body, want) {
				t.Errorf("Unit(%q) missing %q", name, want)
			}
		}
	}

	bot, err := Unit("agents-bot.service", "ubuntu", "/home/ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"EnvironmentFile=/home/ubuntu/.agents/slack.env",
		"Environment=AGENTS_ON_BOX=1",
		"ExecStart=/usr/local/bin/agents bot",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("agents-bot.service missing %q", want)
		}
	}

	herdr, err := Unit("herdr-server.service", "ubuntu", "/home/ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(herdr, "ExecStart=/usr/local/bin/herdr server") {
		t.Error("herdr-server.service does not run `herdr server`")
	}
}

func TestSlackEnvPath(t *testing.T) {
	if got, want := SlackEnvPath("/home/ubuntu"), "/home/ubuntu/.agents/slack.env"; got != want {
		t.Errorf("SlackEnvPath = %q, want %q", got, want)
	}
}
