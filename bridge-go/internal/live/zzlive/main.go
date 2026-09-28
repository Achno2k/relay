package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/live"
)

type hub struct{}

func (hub) Count() int { return 1 }
func (hub) Broadcast(ev api.ServerEvent) {
	text := "<nil>"
	if ev.Text != nil {
		text = *ev.Text
		if len(text) > 70 {
			text = "…" + text[len(text)-70:]
		}
	}
	tool := "<nil>"
	if ev.Tool != nil {
		tool = ev.Tool.Name + ": " + ev.Tool.Summary
	}
	flag := ""
	if ev.Text != nil && (strings.Contains(*ev.Text, "Update available") || strings.Contains(*ev.Text, "brew upgrade")) {
		flag = "  <<< BANNER: " + *ev.Text
	}
	fmt.Printf("%s seq=%d text=%q tool=%q%s\n", ev.AgentID, ev.Seq, text, tool, flag)
}

func main() {
	c := herdr.NewClient("")
	m := live.NewMonitor(c, hub{})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	go m.Run(ctx)
	for ctx.Err() == nil {
		agents, err := c.Agents(ctx)
		if err == nil {
			var in []live.Agent
			for _, a := range agents {
				in = append(in, live.Agent{ID: a.PaneID, Kind: a.Agent, Status: a.AgentStatus, Cwd: a.CwdOrForeground()})
			}
			m.Update(in)
		}
		time.Sleep(time.Second)
	}
}
