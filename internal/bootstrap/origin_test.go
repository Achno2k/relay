package bootstrap

import "testing"

func TestSplitRemote(t *testing.T) {
	cases := map[string][3]string{
		"git@github.com:Achno2k/agents-cli.git":       {"git", "github.com", "Achno2k/agents-cli"},
		"git@github-personal:Achno2k/agents-cli.git":  {"git", "github-personal", "Achno2k/agents-cli"},
		"ssh://git@github.com/Achno2k/agents-cli.git": {"git", "github.com", "Achno2k/agents-cli"},
		"https://github.com/Achno2k/agents-cli":       {"", "github.com", "Achno2k/agents-cli"},
		"https://gitlab.example.com/a/b.git":          {"", "gitlab.example.com", "a/b"},
		"/local/path":                                 {"", "", ""},
	}
	for in, want := range cases {
		u, h, p := splitRemote(in)
		if u != want[0] || h != want[1] || p != want[2] {
			t.Errorf("%s: got %q %q %q, want %v", in, u, h, p, want)
		}
	}
}

func TestNormalizeOriginGitHubBecomesHTTPS(t *testing.T) {
	got := NormalizeOrigin(t.Context(), "git@github.com:Achno2k/agents-cli.git")
	if got != "https://github.com/Achno2k/agents-cli.git" {
		t.Fatalf("got %q", got)
	}
}

func TestShellPathExpandsHome(t *testing.T) {
	if got := shellPath("$HOME/work/a b/main"); got != `"$HOME"/'work/a b/main'` {
		t.Fatalf("got %s", got)
	}
	if got := shellPath("/opt/x"); got != "'/opt/x'" {
		t.Fatalf("got %s", got)
	}
}

func TestLooksLikeGitAuthFailure(t *testing.T) {
	if !LooksLikeGitAuthFailure("remote: Repository not found.\nfatal: repository 'https://github.com/x/y.git/' not found") {
		t.Fatal("not-found should count as auth")
	}
	if LooksLikeGitAuthFailure("fatal: unable to access: Could not resolve host: github.com") {
		t.Fatal("dns failure is not auth")
	}
}
