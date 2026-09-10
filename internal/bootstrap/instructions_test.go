package bootstrap

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeInstructionsIntoEmptyFile(t *testing.T) {
	block := InstructionBlock()
	for name, existing := range map[string]string{
		"missing":    "",
		"whitespace": "\n\n   \n",
	} {
		if got := MergeInstructions(existing, block); got != block {
			t.Errorf("%s file: got %q, want just the block", name, got)
		}
	}
}

func TestMergeInstructionsAppendsWhenUnmarked(t *testing.T) {
	existing := "# My notes\n\nAlways use tabs.\n"
	got := MergeInstructions(existing, InstructionBlock())

	if !strings.HasPrefix(got, existing) {
		t.Errorf("the user's own file was not kept intact:\n%s", got)
	}
	if !strings.Contains(got, BeginMarker) || !strings.Contains(got, EndMarker) {
		t.Error("markers missing after append")
	}
	if strings.Index(got, "Always use tabs.") > strings.Index(got, BeginMarker) {
		t.Error("block was appended above the user's content")
	}
}

func TestMergeInstructionsReplacesOnlyTheBlock(t *testing.T) {
	head := "# My notes\n\nAlways use tabs.\n\n"
	tail := "\n## Trailing section\n\nKeep me.\n"
	existing := head + BeginMarker + "\nstale contract, long since changed\n" + EndMarker + "\n" + tail

	got := MergeInstructions(existing, InstructionBlock())

	if strings.Contains(got, "stale contract") {
		t.Error("the old block survived the merge")
	}
	if !strings.Contains(got, "Always use tabs.") || !strings.Contains(got, "Keep me.") {
		t.Errorf("content outside the markers was lost:\n%s", got)
	}
	if !strings.Contains(got, "artifact: <kebab-name>.md") {
		t.Error("the new block was not written in")
	}
	if n := strings.Count(got, BeginMarker); n != 1 {
		t.Errorf("file has %d begin markers, want 1", n)
	}
	if n := strings.Count(got, EndMarker); n != 1 {
		t.Errorf("file has %d end markers, want 1", n)
	}
	if strings.Index(got, "Keep me.") < strings.Index(got, EndMarker) {
		t.Error("the trailing section moved above the block")
	}
}

// Bootstrap and `agents deploy` both run this on every box, repeatedly.
func TestMergeInstructionsIsIdempotent(t *testing.T) {
	block := InstructionBlock()
	for name, existing := range map[string]string{
		"empty":             "",
		"unmarked":          "# My notes\n\nAlways use tabs.\n",
		"no newline at eof": "# My notes",
		"already merged":    "head\n\n" + BeginMarker + "\nold\n" + EndMarker + "\n\ntail\n",
	} {
		once := MergeInstructions(existing, block)
		twice := MergeInstructions(once, block)
		if once != twice {
			t.Errorf("%s: second merge changed the file:\n--- once ---\n%s\n--- twice ---\n%s", name, once, twice)
		}
		if n := strings.Count(twice, BeginMarker); n != 1 {
			t.Errorf("%s: %d begin markers after two merges", name, n)
		}
	}
}

// A file whose markers are out of order is not a block we can edit safely;
// append rather than mangle it.
func TestMergeInstructionsHandlesBrokenMarkers(t *testing.T) {
	existing := EndMarker + "\nsomething odd\n" + BeginMarker + "\n"
	got := MergeInstructions(existing, InstructionBlock())
	if !strings.HasPrefix(got, existing) {
		t.Errorf("broken-marker file was rewritten instead of appended to:\n%s", got)
	}
}

// The body is the contract the bot parses; these two formats are exact.
func TestInstructionBodyKeepsTheContract(t *testing.T) {
	body := InstructionBody()
	for _, want := range []string{
		"`.agents/`",
		".agents/reply.md",
		".agents/artifacts/<kebab-name>.md",
		"artifact: <kebab-name>.md",
		"decision:\n1. <option>\n2. <option>\n3. <option>",
		"under 12 lines",
		"*single asterisks*",
		"<https://url|label>",
		"Never commit or push unless the thread asks for it.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("instruction body is missing %q", want)
		}
	}
	if strings.Contains(body, BeginMarker) {
		t.Error("the body carries its own markers; InstructionBlock adds them")
	}
}

func TestInstructionBlockWraps(t *testing.T) {
	block := InstructionBlock()
	if !strings.HasPrefix(block, BeginMarker+"\n") {
		t.Error("block does not open with the begin marker")
	}
	if !strings.HasSuffix(block, EndMarker+"\n") {
		t.Error("block does not close with the end marker")
	}
	if !strings.Contains(block, InstructionBody()) {
		t.Error("block does not carry the body verbatim")
	}
}

func TestInstructionTargets(t *testing.T) {
	want := map[string]bool{".claude/CLAUDE.md": true, ".codex/AGENTS.md": true}
	if len(InstructionTargets) != len(want) {
		t.Fatalf("targets = %v", InstructionTargets)
	}
	for _, target := range InstructionTargets {
		if !want[target] {
			t.Errorf("unexpected target %q", target)
		}
	}
}

// The merged file travels to the box inside a quoted heredoc; a body line that
// matched the delimiter would truncate it.
func TestInstructionBlockAvoidsHeredocDelimiter(t *testing.T) {
	for _, line := range strings.Split(InstructionBlock(), "\n") {
		if line == instructionsDelim {
			t.Fatalf("instruction block contains the heredoc delimiter %q", instructionsDelim)
		}
	}
}

// localRunner runs a script through the local bash instead of over ssh, so the
// heredoc that carries a merged file can be exercised for real.
type localRunner struct{ home string }

func (l localRunner) Run(ctx context.Context, script string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Env = append(os.Environ(), "HOME="+l.home)
	return cmd.Run()
}

func (localRunner) Interactive(context.Context, string) error { return nil }
func (localRunner) Copy(context.Context, string, string) error {
	return errors.New("not used")
}
func (localRunner) Args() []string             { return nil }
func (localRunner) Ping(context.Context) error { return nil }

// End to end over a real bash: the block lands in both files, survives a second
// run, and leaves the user's own text alone. Backticks, $ and fences in the body
// must come through the heredoc byte for byte.
func TestInstallInstructionsRoundTripsThroughBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	home := t.TempDir()
	r := localRunner{home: home}
	ctx := context.Background()

	own := "# My notes\n\nAlways use tabs.\n"
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "CLAUDE.md"), []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallInstructions(ctx, r, home, io.Discard); err != nil {
		t.Fatalf("InstallInstructions: %v", err)
	}

	first := map[string]string{}
	for _, rel := range InstructionTargets {
		b, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		got := string(b)
		first[rel] = got
		if !strings.Contains(got, InstructionBody()) {
			t.Errorf("%s did not receive the body verbatim:\n%s", rel, got)
		}
		if !strings.Contains(got, "artifact: <kebab-name>.md") {
			t.Errorf("%s lost the artifact format", rel)
		}
	}
	if !strings.Contains(first[".claude/CLAUDE.md"], "Always use tabs.") {
		t.Error("the user's own CLAUDE.md content was overwritten")
	}

	if err := InstallInstructions(ctx, r, home, io.Discard); err != nil {
		t.Fatalf("second InstallInstructions: %v", err)
	}
	for _, rel := range InstructionTargets {
		b, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != first[rel] {
			t.Errorf("%s changed on the second sync", rel)
		}
	}
}
