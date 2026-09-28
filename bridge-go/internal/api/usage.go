package api

import "time"

// UsageWindow: see api.md "Usage".
type UsageWindow struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	UsedPercent   *float64 `json:"usedPercent,omitzero"`
	WindowMinutes *int     `json:"windowMinutes,omitzero"`
	ResetsAt      *string  `json:"resetsAt,omitzero"`
}

type UsageProvider struct {
	ID                string        `json:"id"`
	Label             string        `json:"label"`
	Plan              *string       `json:"plan,omitzero"`
	Windows           []UsageWindow `json:"windows"`
	UpdatedAt         string        `json:"updatedAt"`
	Source            string        `json:"source"`
	Stale             bool          `json:"stale"`
	UnavailableReason *string       `json:"unavailableReason,omitzero"`
	// Which herdr-driven harnesses ("claude", "codex", "pi") are authenticated against this
	// subscription on this machine, per `pi auth check`. Never guessed.
	UsedBy []string `json:"usedBy"`
}

func (p UsageProvider) MarshalJSON() ([]byte, error) {
	type alias UsageProvider
	p.Windows = nonNil(p.Windows)
	p.UsedBy = nonNil(p.UsedBy)
	return Marshal(alias(p))
}

// MarkedStale sets Stale when UpdatedAt is older than staleAfter (or unparseable).
func (p UsageProvider) MarkedStale(now time.Time, staleAfter time.Duration) UsageProvider {
	at, ok := ParseTimestamp(p.UpdatedAt)
	if !ok {
		p.Stale = true
		return p
	}
	if now.Sub(at) > staleAfter {
		p.Stale = true
	}
	return p
}

// SameDataExcludingFreshness compares everything but UpdatedAt/Stale, to decide whether to
// broadcast or reset backoff.
func (p UsageProvider) SameDataExcludingFreshness(o UsageProvider) bool {
	a, b := p, o
	a.UpdatedAt, b.UpdatedAt = "", ""
	a.Stale, b.Stale = false, false
	ja, err1 := Marshal(a)
	jb, err2 := Marshal(b)
	return err1 == nil && err2 == nil && string(ja) == string(jb)
}

type UsageSnapshot struct {
	Providers []UsageProvider `json:"providers"`
}

func (s UsageSnapshot) MarshalJSON() ([]byte, error) {
	type alias UsageSnapshot
	s.Providers = nonNil(s.Providers)
	return Marshal(alias(s))
}
