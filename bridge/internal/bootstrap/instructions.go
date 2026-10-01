package bootstrap

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/Achno2k/agents-cli/internal/sshx"
)

//go:embed scripts/agent-instructions.md
var agentInstructions string

// Markers around the block agents-cli owns inside a global instruction file.
// Everything outside them belongs to the user and is never touched.
const (
	BeginMarker = "<!-- agents:begin -->"
	EndMarker   = "<!-- agents:end -->"
)

// InstructionTargets are the per-harness global instruction files on the box,
// relative to the box user's home. Both get the same body.
var InstructionTargets = []string{
	".claude/CLAUDE.md", // Claude Code, user global
	".codex/AGENTS.md",  // Codex, user global
}

// instructionsDelim ends the heredoc that carries a merged file to the box.
const instructionsDelim = "AGENTS_INSTRUCTIONS_EOF"

// InstructionBody is the reply contract the agents on the box are taught:
// write `.agents/reply.md`, format for Slack, spill long content to artifacts,
// ask for decisions in a parseable block.
func InstructionBody() string { return strings.TrimSpace(agentInstructions) }

// InstructionBlock is that body wrapped in the markers, as it appears in a
// global instruction file.
func InstructionBlock() string {
	return BeginMarker + "\n" + InstructionBody() + "\n" + EndMarker + "\n"
}

// MergeInstructions folds block into an existing instruction file. When the
// markers are already there, only what is between them is replaced; otherwise
// the block is appended. Merging a merged file changes nothing, so bootstrap
// and `agents deploy` can both run it as often as they like.
func MergeInstructions(existing, block string) string {
	block = strings.TrimRight(block, "\n") + "\n"

	if strings.TrimSpace(existing) == "" {
		return block
	}

	start := strings.Index(existing, BeginMarker)
	end := strings.Index(existing, EndMarker)
	if start >= 0 && end > start {
		head := existing[:start]
		tail := strings.TrimPrefix(existing[end+len(EndMarker):], "\n")
		return head + block + tail
	}

	head := existing
	if !strings.HasSuffix(head, "\n") {
		head += "\n"
	}
	return head + "\n" + block
}

// InstallInstructions syncs the block into every harness's global instruction
// file on the box, keeping whatever the user wrote outside the markers.
func InstallInstructions(ctx context.Context, r sshx.Runner, home string, log io.Writer) error {
	block := InstructionBlock()
	for _, rel := range InstructionTargets {
		file := path.Join(home, rel)

		var current bytes.Buffer
		if err := r.Run(ctx, "cat "+shellQuote(file)+" 2>/dev/null || true", &current, io.Discard); err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}

		merged := MergeInstructions(current.String(), block)
		if err := writeRemoteFile(ctx, r, file, merged, "0644"); err != nil {
			return err
		}
		if log != nil {
			fmt.Fprintf(log, "synced %s\n", file)
		}
	}
	return nil
}

// writeRemoteFile writes content to path on the box through a quoted heredoc,
// creating the parent directory. Nothing is expanded on the way.
func writeRemoteFile(ctx context.Context, r sshx.Runner, file, content, mode string) error {
	for _, line := range strings.Split(content, "\n") {
		if line == instructionsDelim {
			return fmt.Errorf("write %s: content contains the heredoc delimiter", file)
		}
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	script := "set -e\n" +
		"mkdir -p " + shellQuote(path.Dir(file)) + "\n" +
		"cat > " + shellQuote(file) + " <<'" + instructionsDelim + "'\n" +
		content +
		instructionsDelim + "\n" +
		"chmod " + mode + " " + shellQuote(file) + "\n"
	return run(ctx, r, script, "write "+file)
}
