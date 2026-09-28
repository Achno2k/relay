package controls

import (
	"bytes"
	"os"
	"path/filepath"
)

// SettingsGuard: Claude Code's /model and /effort also save the choice as the default for new
// sessions (`~/.claude/settings.json`). A switch from the phone is meant for one agent, so the
// bridge snapshots the file before the command and puts the exact bytes back afterwards. The
// running session keeps its new model/effort; only the saved default is restored.
type SettingsGuard struct {
	Path string // "" = no file to guard
}

func NewSettingsGuard() *SettingsGuard {
	home, _ := os.UserHomeDir()
	return &SettingsGuard{Path: filepath.Join(home, ".claude/settings.json")}
}

// Snapshot is the file's bytes; nil if it couldn't be read.
type Snapshot struct{ data []byte }

func (g *SettingsGuard) Snapshot() Snapshot {
	if g.Path == "" {
		return Snapshot{}
	}
	data, err := os.ReadFile(g.Path)
	if err != nil {
		return Snapshot{}
	}
	return Snapshot{data}
}

// Restore writes the snapshot back if the file changed, in place so it keeps its inode and 0600
// permissions. Returns true if it restored something.
func (g *SettingsGuard) Restore(s Snapshot) bool {
	if g.Path == "" || s.data == nil {
		return false
	}
	now, err := os.ReadFile(g.Path)
	if err != nil || bytes.Equal(now, s.data) {
		return false
	}
	f, err := os.OpenFile(g.Path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return false
	}
	_, werr := f.Write(s.data)
	cerr := f.Close()
	return werr == nil && cerr == nil
}
