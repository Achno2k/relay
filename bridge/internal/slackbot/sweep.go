package slackbot

import (
	"context"
	"time"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/state"
)

// sweepInterval is how often idle sessions are checked against the TTL.
const sweepInterval = 10 * time.Minute

// sweep parks sessions that have been idle past the TTL: the agent is killed,
// the worktree is kept. A parked session frees a slot under MaxLive.
func (b *Bot) sweep(ctx context.Context) {
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.sweepOnce(ctx, time.Now())
		}
	}
}

// sweepOnce is the body of one sweep, with the clock passed in.
func (b *Bot) sweepOnce(ctx context.Context, now time.Time) int {
	ttl := b.idleTTL()
	all, err := b.Store.List(ctx)
	if err != nil {
		b.logf("sweep: list: %v", err)
		return 0
	}
	parked := 0
	for _, s := range all {
		if !s.Live() || s.Status == state.StatusWorking || s.Status == state.StatusBlocked {
			continue
		}
		if now.Sub(s.LastActive) < ttl {
			continue
		}
		b.park(ctx, s)
		parked++
	}
	return parked
}

func (b *Bot) park(ctx context.Context, s state.Session) {
	b.stopWatcher(s.ID)
	if err := b.Herdr.KillAgent(ctx, s.AgentName); err != nil {
		b.logf("sweep: kill %s: %v", s.AgentName, err)
	}
	b.mu.Lock()
	if cur, err := b.Store.Get(ctx, s.ID); err == nil {
		cur.Status = state.StatusParked
		if err := b.Store.Update(ctx, cur); err != nil {
			b.logf("sweep: park %s: %v", s.ID, err)
		}
	}
	b.mu.Unlock()
	if s.ThreadTS != "" {
		b.say(ctx, s.SlackChannel, s.ThreadTS,
			"Parked `"+s.AgentName+"` after "+b.idleTTL().String()+" idle. The worktree is still at `"+s.WorktreePath+"`.")
	}
}

func (b *Bot) idleTTL() time.Duration {
	h := b.config().Sessions.IdleTTLHours
	if h <= 0 {
		h = config.Default().Sessions.IdleTTLHours
	}
	return time.Duration(h) * time.Hour
}
