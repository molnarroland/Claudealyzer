package core

import (
	"sort"
	"strings"

	"claudealyzer/internal/model"
)

// Filter narrows the sessions every list and report works on.
// The zero value matches everything.
type Filter struct {
	// Model keeps sessions that used this model (exact id, or a prefix such as "claude-opus").
	Model string `json:"model"`
	// From and To (YYYY-MM-DD, both inclusive, "" = open) keep sessions with activity on those days.
	From string `json:"from"`
	To   string `json:"to"`
}

func (f Filter) dated() bool { return f.From != "" || f.To != "" }

// inRange reports whether a YYYY-MM-DD day lies in the filter's date range.
func (f Filter) inRange(day string) bool {
	return (f.From == "" || day >= f.From) && (f.To == "" || day <= f.To)
}

func (f Filter) match(s *model.SessionSummary) bool {
	if f.Model != "" {
		ok := false
		for _, m := range s.Models {
			if m == f.Model || strings.HasPrefix(m, f.Model) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.dated() {
		if len(s.Days) == 0 {
			// No dated activity: fall back to the session's start day.
			if s.StartedAt == 0 {
				return false
			}
			return f.inRange(dayOf(s.StartedAt))
		}
		for d := range s.Days {
			if f.inRange(d) {
				return true
			}
		}
		return false
	}
	return true
}

// ModelInfo is one model seen in the history, for the model filter.
type ModelInfo struct {
	Model    string  `json:"model"`
	Sessions int     `json:"sessions"`
	CostUSD  float64 `json:"costUSD"`
}

// Models lists the models used in a source (and agent), most used first.
func (c *Core) Models(sourceID, agent string) []ModelInfo {
	m := map[string]*ModelInfo{}
	for _, s := range c.summaries(sourceID, agent, Filter{}) {
		for _, id := range s.Models {
			mi := m[id]
			if mi == nil {
				mi = &ModelInfo{Model: id}
				m[id] = mi
			}
			mi.Sessions++
			mi.CostUSD += s.ByModel[id].CostUSD
		}
	}
	out := make([]ModelInfo, 0, len(m))
	for _, x := range m {
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sessions != out[j].Sessions {
			return out[i].Sessions > out[j].Sessions
		}
		return out[i].Model < out[j].Model
	})
	return out
}
