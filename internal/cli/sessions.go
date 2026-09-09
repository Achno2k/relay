package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/herdr"
	"github.com/Achno2k/agents-cli/internal/sshx"
	"github.com/Achno2k/agents-cli/internal/state"
	"github.com/Achno2k/agents-cli/internal/ui"
)

var sessionsAttachedBy string

func init() {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List and control the agent sessions on the box",
		Long: "Sessions live in the box's sqlite store, one per agent pane.\n" +
			"Runs on the box; from the laptop it proxies itself over ssh.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return sessionsRun(c.Context(), sessionsList)
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Show every session as an aligned table",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return sessionsRun(c.Context(), sessionsList)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "json",
		Short: "Print every session as JSON (machine output for the laptop)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return sessionsRun(c.Context(), sessionsJSON)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "kill <id>",
		Short: "Kill the agent and mark the session done (worktree is kept)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return sessionsRun(c.Context(), func(ctx context.Context, st state.Store) error {
				return sessionsStop(ctx, st, args[0], state.StatusDone, "killed")
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "park <id>",
		Short: "Kill the agent but keep the session and its worktree for later",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return sessionsRun(c.Context(), func(ctx context.Context, st state.Store) error {
				return sessionsStop(ctx, st, args[0], state.StatusParked, "parked")
			})
		},
	})

	attached := &cobra.Command{
		Use:   "mark-attached <id>",
		Short: "Record that someone took the session over from a terminal",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if sessionsAttachedBy == "" {
				return errors.New("--by is required, e.g. --by aman@amans-mbp")
			}
			return sessionsRun(c.Context(), func(ctx context.Context, st state.Store) error {
				return sessionsMarkAttached(ctx, st, args[0], sessionsAttachedBy)
			})
		},
	}
	attached.Flags().StringVar(&sessionsAttachedBy, "by", "", "who is attaching, as user@host")
	cmd.AddCommand(attached)

	Register(cmd)
}

// sessionsRun proxies to the box when this is the laptop, otherwise opens the
// store and hands it to fn.
func sessionsRun(ctx context.Context, fn func(context.Context, state.Store) error) error {
	if os.Getenv("AGENTS_ON_BOX") != "1" {
		return sessionsProxyToBox(ctx)
	}
	st, err := state.Open(state.DefaultPath())
	if err != nil {
		return err
	}
	defer st.Close()
	return fn(ctx, st)
}

// sessionsProxyToBox re-runs this exact command on the box over ssh.
func sessionsProxyToBox(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Box.Host == "" && cfg.AWS.InstanceID == "" {
		return config.ErrNotInitialised
	}
	r := sshx.New(sshx.TargetFromConfig(cfg.AWS, cfg.Box))
	return r.Interactive(ctx, "agents "+sessionsQuoteArgs(os.Args[1:]))
}

// sessionsQuoteArgs single-quotes each argument so the remote shell sees the
// same argv this process was given.
func sessionsQuoteArgs(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
	}
	return strings.Join(out, " ")
}

func sessionsList(ctx context.Context, st state.Store) error {
	all, err := st.List(ctx)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		ui.Muted("no sessions")
		return nil
	}
	rows := [][]string{{"ID", "AGENT", "KIND", "REPO", "BRANCH", "STATUS", "IDLE", "ATTACHED"}}
	for _, s := range all {
		rows = append(rows, []string{
			s.ID, s.AgentName, s.Kind, s.Repo, s.Branch, string(s.Status),
			shortDuration(time.Since(s.LastActive)), dashIfEmpty(s.AttachedBy),
		})
	}
	printTable(rows)
	return nil
}

func sessionsJSON(ctx context.Context, st state.Store) error {
	all, err := st.List(ctx)
	if err != nil {
		return err
	}
	if all == nil {
		all = []state.Session{}
	}
	// Machine output, read by the laptop; it deliberately bypasses ui.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(all)
}

// sessionsStop kills the agent in herdr and records the new status. The
// worktree is never touched here; parked sessions are meant to be resumable and
// killed ones still hold work worth looking at.
func sessionsStop(ctx context.Context, st state.Store, ref string, status state.Status, verb string) error {
	s, err := sessionsResolve(ctx, st, ref)
	if err != nil {
		return err
	}
	// KillAgent is idempotent: an agent that is already gone is not an error.
	if err := herdr.New().KillAgent(ctx, s.AgentName); err != nil {
		return fmt.Errorf("killing agent %s: %w", s.AgentName, err)
	}
	s.Status = status
	s.AttachedBy = ""
	s.AttachedAt = nil
	s.LastActive = time.Now()
	if err := st.Update(ctx, s); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("%s %s (%s)", verb, s.ID, s.AgentName))
	ui.Muted("worktree kept at " + s.WorktreePath)
	return nil
}

func sessionsMarkAttached(ctx context.Context, st state.Store, ref, by string) error {
	s, err := sessionsResolve(ctx, st, ref)
	if err != nil {
		return err
	}
	now := time.Now()
	s.AttachedBy = by
	s.AttachedAt = &now
	s.LastActive = now
	if err := st.Update(ctx, s); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("%s (%s) attached by %s", s.ID, s.AgentName, by))
	return nil
}

// sessionsResolve accepts either a session id or an agent name, which is what
// `agents attach` passes through.
func sessionsResolve(ctx context.Context, st state.Store, ref string) (state.Session, error) {
	s, err := st.Get(ctx, ref)
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return state.Session{}, err
	}
	s, err = st.ByAgent(ctx, ref)
	if errors.Is(err, state.ErrNotFound) {
		return state.Session{}, fmt.Errorf("no session with id or agent name %q", ref)
	}
	return s, err
}

// printTable renders rows as aligned columns. Row 0 is the header.
func printTable(rows [][]string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, cell := range r {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	for i, r := range rows {
		var b strings.Builder
		for j, cell := range r {
			b.WriteString(cell)
			if j < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-len(cell)+2))
			}
		}
		if i == 0 {
			ui.Muted(b.String())
			continue
		}
		ui.Info(b.String())
	}
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// shortDuration renders an age as 12s / 34m / 5h / 6d.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
