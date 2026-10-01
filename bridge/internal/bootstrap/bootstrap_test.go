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
		"ssh://git@github.com/org/deep/repo.git":    "repo",
		"agents-cli":                                "agents-cli",
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

// The token must reach the box inside the heredoc and nowhere else: never as an
// argument, so it stays out of the process list and shell history.
func TestGitHubTokenScriptKeepsTokenOffTheCommandLine(t *testing.T) {
	const token = "github_pat_11ABCDEF0123456789_secretvalue"

	script, err := githubTokenScript(token)
	if err != nil {
		t.Fatalf("githubTokenScript: %v", err)
	}

	lines := strings.Split(strings.TrimRight(script, "\n"), "\n")
	openAt, closeAt := -1, -1
	for i, line := range lines {
		switch {
		case strings.HasSuffix(line, "<<'"+ghTokenDelim+"'"):
			openAt = i
		case line == ghTokenDelim:
			closeAt = i
		}
	}
	if openAt < 0 || closeAt < 0 {
		t.Fatalf("script has no %s heredoc:\n%s", ghTokenDelim, script)
	}
	if closeAt != openAt+2 {
		t.Fatalf("heredoc body is not exactly one line (open %d, close %d):\n%s", openAt, closeAt, script)
	}
	if lines[openAt+1] != token {
		t.Errorf("heredoc body = %q, want the token", lines[openAt+1])
	}
	for i, line := range lines {
		if i == openAt+1 {
			continue
		}
		if strings.Contains(line, token) {
			t.Errorf("line %d puts the token on a command line: %q", i, line)
		}
	}
	if strings.Contains(lines[openAt], token) {
		t.Errorf("the gh invocation itself carries the token: %q", lines[openAt])
	}

	for _, want := range []string{"gh auth login --with-token", "gh auth setup-git", "gh auth status"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
}

func TestGitHubTokenScriptRejectsBadTokens(t *testing.T) {
	for _, bad := range []string{"", "   ", "two\nlines", "carriage\rreturn"} {
		if _, err := githubTokenScript(bad); err == nil {
			t.Errorf("githubTokenScript(%q) accepted an invalid token", bad)
		}
	}
	if _, err := githubTokenScript("  padded_token  "); err != nil {
		t.Errorf("githubTokenScript trimmed token: %v", err)
	}
}
