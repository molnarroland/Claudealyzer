package adapters

import (
	"sort"
	"time"

	"claudealyzer/internal/model"
)

// Finalize fills the session summary from turns, calls and subagents.
func Finalize(s *model.Session) {
	var own, sub model.Usage
	models := map[string]int64{}
	byModel := map[string]model.Usage{}
	daily := map[string]float64{}
	days := map[string]model.DayStat{}
	addCalls := func(calls []model.Call) {
		for _, c := range calls {
			if c.Model != "" && c.Model != "<synthetic>" {
				models[c.Model] += c.Usage.Total()
				u := byModel[c.Model]
				u.Add(c.Usage)
				byModel[c.Model] = u
			}
			if c.Time > 0 {
				d := time.UnixMilli(c.Time).Local().Format("2006-01-02")
				daily[d] += c.Usage.CostUSD
				ds := days[d]
				ds.Usage.Add(c.Usage)
				days[d] = ds
			}
		}
	}
	for _, c := range s.Calls {
		own.Add(c.Usage)
	}
	addCalls(s.Calls)
	for i := range s.Subagents {
		a := &s.Subagents[i]
		a.Usage = model.Usage{}
		for _, c := range a.Calls {
			a.Usage.Add(c.Usage)
			if c.Usage.Context() > a.PeakContext {
				a.PeakContext = c.Usage.Context()
			}
		}
		if a.Model == "" && len(a.Calls) > 0 {
			a.Model = a.Calls[len(a.Calls)-1].Model
		}
		a.TurnCount = len(a.Turns)
		sub.Add(a.Usage)
		addCalls(a.Calls)
		if a.ParentTurn >= 0 && a.ParentTurn < len(s.Turns) {
			s.Turns[a.ParentTurn].SubagentUsage.Add(a.Usage)
		}
	}
	total := own
	total.Add(sub)
	s.Usage = total
	s.SubagentUsage = sub
	s.SubagentCount = len(s.Subagents)
	s.TurnCount = len(s.Turns)
	for _, t := range s.Turns {
		if t.StartedAt > 0 && t.Prompt != "" && t.Prompt != "(session start)" {
			d := time.UnixMilli(t.StartedAt).Local().Format("2006-01-02")
			ds := days[d]
			ds.Prompts++
			days[d] = ds
		}
	}
	s.Daily = daily
	s.Days = days
	s.ByModel = byModel
	s.Models = s.Models[:0]
	type mt struct {
		m string
		t int64
	}
	var ms []mt
	for m, t := range models {
		ms = append(ms, mt{m, t})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].t > ms[j].t })
	for _, x := range ms {
		s.Models = append(s.Models, x.m)
	}
	if len(ms) > 0 {
		s.MainModel = ms[0].m
	}
	s.PeakContext = 0
	for _, t := range s.Turns {
		if t.PeakContext > s.PeakContext {
			s.PeakContext = t.PeakContext
		}
		if s.StartedAt == 0 || (t.StartedAt > 0 && t.StartedAt < s.StartedAt) {
			s.StartedAt = t.StartedAt
		}
		if e := t.StartedAt + t.DurationMs; e > s.EndedAt {
			s.EndedAt = e
		}
	}
	for _, c := range s.Calls {
		if c.Time > s.EndedAt {
			s.EndedAt = c.Time
		}
	}
	if total.Calls == 0 {
		s.NoUsageData = true
	}
	if s.Title == "" {
		for _, t := range s.Turns {
			if t.Prompt != "" && t.Prompt != "(session start)" {
				s.Title = OneLine(t.Prompt, 80)
				break
			}
		}
	}
	if s.Title == "" {
		s.Title = "Untitled session"
	}
}

// ToSummary strips the heavy parts.
func ToSummary(s *model.Session) model.SessionSummary { return s.SessionSummary }
