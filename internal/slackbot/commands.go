package slackbot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Achno2k/agents-cli/internal/state"
	"github.com/Achno2k/agents-cli/internal/worktree"
)

// maxInlineChars is the point past which output becomes a file attachment
// instead of a message. Slack truncates long messages badly.
const maxInlineChars = 2500

// runCommand answers a thread command against a bound session.
func (b *Bot) runCommand(ctx context.Context, d decision, sess state.Session) {
	switch d.Command {
	case "diff":
		b.cmdDiff(ctx, sess)
	case "show":
		b.cmdShow(ctx, sess, d.Args)
	case "full":
		b.cmdFull(ctx, sess)
	case "pr":
		b.cmdPR(ctx, sess, d.Args)
	case "done":
		b.cmdDone(ctx, sess, strings.Contains(d.Args, "--clean"))
	}
}

func (b *Bot) cmdDiff(ctx context.Context, sess state.Session) {
	patch, err := worktree.Diff(ctx, sess.WorktreePath)
	if err != nil {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "Could not read the diff: "+err.Error())
		return
	}
	if strings.TrimSpace(patch) == "" {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "No changes yet.")
		return
	}
	b.deliver(ctx, sess, sess.AgentName+".patch", "diff for "+sess.AgentName, patch)
}

func (b *Bot) cmdShow(ctx context.Context, sess state.Session, arg string) {
	rel := strings.TrimSpace(firstField(arg))
	if rel == "" {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "Say `show <path>` with a path inside the worktree.")
		return
	}
	full, err := safeJoin(sess.WorktreePath, rel)
	if err != nil {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, err.Error())
		return
	}
	body, err := os.ReadFile(full)
	if err != nil {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "Cannot read `"+rel+"`: "+err.Error())
		return
	}
	b.deliver(ctx, sess, filepath.Base(rel), rel, string(body))
}

func (b *Bot) cmdFull(ctx context.Context, sess state.Session) {
	r := replyReader{worktree: sess.WorktreePath}
	// Force a read of the file even when the watcher already consumed it.
	text, _ := r.read(ctx, b.Herdr, sess.AgentName)
	if strings.TrimSpace(text) == "" {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "Nothing to show yet.")
		return
	}
	b.deliver(ctx, sess, "reply.md", "full reply from "+sess.AgentName, text)
}

// cmdPR opens a pull request from the session branch using the gh CLI.
func (b *Bot) cmdPR(ctx context.Context, sess state.Session, arg string) {
	if dirty, err := worktree.IsDirty(ctx, sess.WorktreePath); err == nil && dirty {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "The worktree has uncommitted changes. Ask the agent to commit first.")
		return
	}
	if out, err := runIn(ctx, sess.WorktreePath, "git", "push", "-u", "origin", sess.Branch); err != nil {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "Push failed:\n```\n"+tail(out, 800)+"\n```")
		return
	}
	args := []string{"pr", "create", "--fill"}
	if title := strings.TrimSpace(arg); title != "" {
		args = []string{"pr", "create", "--fill", "--title", title}
	}
	out, err := runIn(ctx, sess.WorktreePath, "gh", args...)
	if err != nil {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "`gh pr create` failed:\n```\n"+tail(out, 800)+"\n```")
		return
	}
	b.say(ctx, sess.SlackChannel, sess.ThreadTS, strings.TrimSpace(out))
}

// cmdDone ends a session. The worktree survives unless clean was asked for,
// and a dirty worktree is never deleted.
func (b *Bot) cmdDone(ctx context.Context, sess state.Session, clean bool) {
	b.stopWatcher(sess.ID)
	if err := b.Herdr.KillAgent(ctx, sess.AgentName); err != nil {
		b.logf("kill %s: %v", sess.AgentName, err)
	}

	msg := "Done. `" + sess.AgentName + "` is stopped, the worktree is still at `" + sess.WorktreePath + "`."
	if clean {
		err := worktree.Remove(ctx, b.repoDir(sess.Repo), sess.WorktreePath, false)
		switch {
		case errors.Is(err, worktree.ErrDirty):
			msg = "Stopped `" + sess.AgentName + "`, but the worktree has uncommitted work so I kept it at `" + sess.WorktreePath + "`."
		case err != nil:
			msg = "Stopped `" + sess.AgentName + "`, but could not remove the worktree: " + err.Error()
		default:
			msg = "Done. `" + sess.AgentName + "` is stopped and the worktree is removed."
		}
	}

	b.mu.Lock()
	if cur, err := b.Store.Get(ctx, sess.ID); err == nil {
		cur.Status = state.StatusDone
		cur.LastActive = time.Now()
		if err := b.Store.Update(ctx, cur); err != nil {
			b.logf("mark done %s: %v", sess.ID, err)
		}
	}
	b.mu.Unlock()

	_ = b.Slack.RemoveReaction(ctx, sess.SlackChannel, sess.ThreadTS, reactionWorking)
	_ = b.Slack.RemoveReaction(ctx, sess.SlackChannel, sess.ThreadTS, reactionBlocked)
	b.say(ctx, sess.SlackChannel, sess.ThreadTS, msg)
}

// deliver posts short output inline and attaches anything longer.
func (b *Bot) deliver(ctx context.Context, sess state.Session, filename, title, body string) {
	if len(body) <= maxInlineChars {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "```\n"+body+"\n```")
		return
	}
	if err := b.Slack.UploadText(ctx, sess.SlackChannel, sess.ThreadTS, filename, title, body); err != nil {
		b.logf("upload %s: %v", filename, err)
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "```\n"+tail(body, maxInlineChars)+"\n```")
	}
}

// excludeAgentsDir keeps the reply file out of every diff by adding .agents/
// to the worktree's own exclude file. git computes that path for us, since a
// linked worktree keeps it under .git/worktrees/<id>.
func excludeAgentsDir(ctx context.Context, worktreePath string) error {
	out, err := runIn(ctx, worktreePath, "git", "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return fmt.Errorf("resolve info/exclude: %w", err)
	}
	path := strings.TrimSpace(out)
	if path == "" {
		return errors.New("git returned no path for info/exclude")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(worktreePath, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if b, err := os.ReadFile(path); err == nil && bytes.Contains(b, []byte("\n.agents/")) {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n# written by agents: the bot's reply file\n.agents/\n")
	return err
}

// safeJoin resolves rel inside root and refuses to escape it.
func safeJoin(root, rel string) (string, error) {
	if root == "" {
		return "", errors.New("this session has no worktree")
	}
	if filepath.IsAbs(rel) {
		return "", errors.New("give a path relative to the worktree")
	}
	full := filepath.Clean(filepath.Join(root, rel))
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", errors.New("that path is outside the worktree")
	}
	return full, nil
}

// runIn runs a command in dir and returns its combined output.
func runIn(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func firstField(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
