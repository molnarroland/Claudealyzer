// Package insight runs cost rules over a session and explains what drove cost.
package insight

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"claudealyzer/internal/model"
	"claudealyzer/internal/pricing"
)

type Thresholds struct {
	ContextTokens   int64   `json:"contextTokens"`   // context too large
	LongTurns       int     `json:"longTurns"`       // long session without compaction
	LongCacheRead   int64   `json:"longCacheRead"`   // ... or cache read total
	FilesSession    int     `json:"filesSession"`    // distinct files read
	RereadPct       float64 `json:"rereadPct"`       // % of reads that are re-reads
	FilesStep       int     `json:"filesStep"`       // reads in one turn
	ToolOutput      int64   `json:"toolOutput"`      // tokens per tool result
	SlowRunSec      int     `json:"slowRunSec"`      // test/build wall time per turn
	HeavyPct        float64 `json:"heavyPct"`        // heavy command share of a turn
	CacheChurn      float64 `json:"cacheChurn"`      // cache write / read
	SubagentPct     float64 `json:"subagentPct"`     // subagent share of session cost
	SubagentFanout  int     `json:"subagentFanout"`  // subagents in one turn
	SmallOutput     int64   `json:"smallOutput"`     // output tokens considered "small"
	SmallOutputPct  float64 `json:"smallOutputPct"`  // share of small calls on large models
	FailureStreak   int     `json:"failureStreak"`   // same tool failing in a row
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		ContextTokens: 150000, LongTurns: 40, LongCacheRead: 20_000_000,
		FilesSession: 80, RereadPct: 25, FilesStep: 30, ToolOutput: 20000,
		SlowRunSec: 120, HeavyPct: 50, CacheChurn: 0.3, SubagentPct: 30, SubagentFanout: 4,
		SmallOutput: 300, SmallOutputPct: 50, FailureStreak: 3,
	}
}

type Finding struct {
	Rule       string  `json:"rule"`
	Severity   string  `json:"severity"` // high | medium | ok
	Title      string  `json:"title"`
	Text       string  `json:"text"`
	Fix        string  `json:"fix"`
	Turn       int     `json:"turn"` // -1: whole session
	StepID     string  `json:"stepId"`
	SubagentID string  `json:"subagentId"`
	ImpactUSD  float64 `json:"impactUSD"`
}

func fmtT(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return fmt.Sprint(n)
}

func fmtD(ms int64) string {
	s := ms / 1000
	if s >= 60 {
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// cacheReadRate is $/token for re-sending context with this session's main model.
func cacheReadRate(s *model.Session) float64 {
	r, ok := pricing.Lookup(s.MainModel, s.StartedAt)
	if !ok {
		return 0
	}
	return r.CacheRead / 1e6
}

var largeModel = []string{"opus", "fable", "mythos", "gpt-5", "gpt-6", "pro"}

func isLarge(m string) bool {
	m = strings.ToLower(m)
	for _, x := range largeModel {
		if strings.Contains(m, x) {
			return true
		}
	}
	return false
}

// TurnFindings runs the per-turn rules on one transcript (session or subagent).
func TurnFindings(turns []model.Turn, th Thresholds, crRate float64, subID string) []Finding {
	var out []Finding
	callsAfter := func(ti int) int {
		n := 0
		for _, t := range turns[ti:] {
			n += t.Usage.Calls
		}
		return n
	}
	for ti, t := range turns {
		tc := t.Usage.CostUSD
		add := func(f Finding) {
			f.Turn = ti
			f.SubagentID = subID
			out = append(out, f)
		}
		// Context size.
		if t.PeakContext > th.ContextTokens {
			add(Finding{Rule: "context", Severity: "high", Title: "Context too large", ImpactUSD: t.Usage.CostCacheRead * 0.35,
				Text: fmt.Sprintf("Peak context reached %s tokens; every call in this turn re-sent it (%s cache-read tokens in total).", fmtT(t.PeakContext), fmtT(t.Usage.CacheRead)),
				Fix:  "Run /compact before long loops or fan-out, or continue in a fresh session."})
		} else if t.PeakContext > th.ContextTokens*3/4 {
			add(Finding{Rule: "context", Severity: "medium", Title: "Context growing", ImpactUSD: t.Usage.CostCacheRead * 0.15,
				Text: fmt.Sprintf("Peak context %s tokens.", fmtT(t.PeakContext)), Fix: "Compact before the next large task."})
		}
		// Test / build runs and repeats.
		var runMs, heavyMs int64
		var runs int
		cmdCount := map[string][]model.Step{}
		var heavyNames []string
		readFiles := 0
		agents := 0
		streak, streakTool := 0, ""
		for _, s := range t.Steps {
			switch s.Kind {
			case model.StepShell:
				cmdCount[s.Title] = append(cmdCount[s.Title], s)
				switch s.Category {
				case "test", "build":
					runMs += s.DurationMs
					runs++
				case "heavy":
					heavyMs += s.DurationMs
					heavyNames = append(heavyNames, firstWords(s.Title, 3))
				}
			case model.StepRead:
				n := len(s.Files)
				if n == 0 {
					n = 1
				}
				readFiles += n
			case model.StepAgent:
				agents++
			}
			if s.ResultTokens > th.ToolOutput {
				later := callsAfter(ti)
				add(Finding{Rule: "tool-output", Severity: "medium", Title: "Large tool output", StepID: s.ID,
					ImpactUSD: float64(s.ResultTokens) * crRate * float64(later),
					Text:      fmt.Sprintf("%s added about %s tokens to context, re-sent on up to %d later calls.", firstWords(s.Title, 6), fmtT(s.ResultTokens), later),
					Fix:       "Limit output (head/tail, --quiet, narrower reads) or ask for a summary instead of raw output."})
			}
			if s.Kind != model.StepThinking && s.Kind != model.StepText && s.Kind != model.StepSystem {
				if s.IsError && s.Tool == streakTool {
					streak++
				} else if s.IsError {
					streak, streakTool = 1, s.Tool
				} else {
					streak, streakTool = 0, ""
				}
				if streak == th.FailureStreak {
					add(Finding{Rule: "errors", Severity: "medium", Title: "Errors and retries", StepID: s.ID, ImpactUSD: tc * 0.1,
						Text: fmt.Sprintf("%s failed %d times in a row.", s.Tool, streak),
						Fix:  "Stop and fix the root cause (or give the agent the missing context) instead of retrying."})
				}
			}
		}
		dur := t.DurationMs
		if dur <= 0 {
			dur = 1
		}
		if runMs > int64(th.SlowRunSec)*1000 {
			sev := "medium"
			if runMs > int64(th.SlowRunSec)*2500 {
				sev = "high"
			}
			add(Finding{Rule: "slow-runs", Severity: sev, Title: "Slow test and build runs", ImpactUSD: tc * minf(float64(runMs)/float64(dur), 1) * 0.5,
				Text: fmt.Sprintf("%d run(s), %s of wall time (%d%% of the turn).", runs, fmtD(runMs), runMs*100/dur),
				Fix:  "Run only affected tests or a type-check between edits, and the full suite once at the end."})
		}
		for cmd, ss := range cmdCount {
			if len(ss) >= 2 && cmd != "" {
				short := strings.TrimSuffix(firstWords(cmd, 8), "…")
				if len(short) > 90 {
					short = short[:90] + "…"
				}
				add(Finding{Rule: "repeat", Severity: "medium", Title: "Repeated command", StepID: ss[len(ss)-1].ID, ImpactUSD: tc * 0.05 * float64(len(ss)-1),
					Text: fmt.Sprintf("%q ran %d× in this turn.", short, len(ss)),
					Fix:  "Fix all issues before re-running, or run a narrower check in between."})
			}
		}
		if heavyMs > 0 && (float64(heavyMs)/float64(dur)*100 > th.HeavyPct || heavyMs > int64(th.SlowRunSec)*1000) {
			sev := "medium"
			if float64(heavyMs)/float64(dur)*100 > th.HeavyPct {
				sev = "high"
			}
			add(Finding{Rule: "cpu", Severity: sev, Title: "CPU-heavy commands", ImpactUSD: tc * 0.2,
				Text: fmt.Sprintf("%s: %s, %d%% of the turn.", strings.Join(uniq(heavyNames), ", "), fmtD(heavyMs), heavyMs*100/dur),
				Fix:  "Limit it to changed targets, shard it, or run it yourself outside the agent."})
		}
		if readFiles > th.FilesStep {
			sev := "medium"
			if readFiles > th.FilesStep*8/3 {
				sev = "high"
			}
			add(Finding{Rule: "files", Severity: sev, Title: "Many files read", ImpactUSD: tc * 0.12,
				Text: fmt.Sprintf("%d file reads in this turn.", readFiles), Fix: "Point the agent at specific files or put the needed APIs in the prompt."})
		}
		if agents > th.SubagentFanout {
			add(Finding{Rule: "fanout", Severity: "medium", Title: "Subagent fan-out", ImpactUSD: t.SubagentUsage.CostUSD * 0.2,
				Text: fmt.Sprintf("%d subagents started in this turn.", agents), Fix: "Use a smaller model or a tighter brief for simple scaffolding agents."})
		}
	}
	return out
}

// Session runs every rule on a full session, including its subagents.
func Session(s *model.Session, th Thresholds) []Finding {
	cr := cacheReadRate(s)
	out := TurnFindings(s.Turns, th, cr, "")
	for _, a := range s.Subagents {
		out = append(out, TurnFindings(a.Turns, th, cr, a.ID)...)
	}
	u := s.Usage
	// Long session without compaction.
	compacted := false
	for _, t := range s.Turns {
		compacted = compacted || t.Compacted
	}
	if !compacted && (len(s.Turns) > th.LongTurns || u.CacheRead > th.LongCacheRead) {
		out = append(out, Finding{Rule: "long-session", Severity: "medium", Title: "Long session without compaction", Turn: -1, ImpactUSD: u.CostCacheRead * 0.2,
			Text: fmt.Sprintf("%d turns and %s cache-read tokens with no compaction.", len(s.Turns), fmtT(u.CacheRead)),
			Fix:  "Start a fresh session per task, or /compact between tasks."})
	}
	// Files across the session.
	reads := map[string]int{}
	total := 0
	for _, t := range s.Turns {
		for _, st := range t.Steps {
			if st.Kind == model.StepRead {
				for _, f := range st.Files {
					reads[f]++
					total++
				}
			}
		}
	}
	if len(reads) > th.FilesSession {
		out = append(out, Finding{Rule: "files", Severity: "high", Title: "Too many files read", Turn: -1, ImpactUSD: u.CostCacheRead * 0.1,
			Text: fmt.Sprintf("%d distinct files read in this session.", len(reads)), Fix: "Narrow the task or point the agent at the right files."})
	}
	if total > 10 {
		re := total - len(reads)
		if pct := float64(re) * 100 / float64(total); pct > th.RereadPct {
			out = append(out, Finding{Rule: "rereads", Severity: "medium", Title: "Files read again", Turn: -1, ImpactUSD: u.CostCacheRead * 0.05,
				Text: fmt.Sprintf("%d of %d reads (%.0f%%) were files already read in this session.", re, total, pct),
				Fix:  "Keep key files in context (or in CLAUDE.md) instead of re-reading them."})
		}
	}
	// Cache churn.
	if u.CacheRead > 0 && u.CacheWrite() > 0 {
		ratio := float64(u.CacheWrite()) / float64(u.CacheRead)
		if ratio > th.CacheChurn {
			out = append(out, Finding{Rule: "cache", Severity: "medium", Title: "Cache churn", Turn: -1, ImpactUSD: u.CostCacheWrite * 0.5,
				Text: fmt.Sprintf("Cache write/read ratio is %.2f: cached context is rewritten more than it is reused.", ratio),
				Fix:  "Avoid editing early context (CLAUDE.md, tools, MCP servers) mid-session; keep sessions active (cache expires)."})
		} else {
			out = append(out, Finding{Rule: "cache", Severity: "ok", Title: "Cache healthy", Turn: -1,
				Text: fmt.Sprintf("Cache write/read ratio is %.3f, so cached context is reused well.", ratio)})
		}
	}
	// Subagents.
	if u.CostUSD > 0 && s.SubagentUsage.CostUSD/u.CostUSD*100 > th.SubagentPct {
		out = append(out, Finding{Rule: "subagents", Severity: "medium", Title: "Expensive subagents", Turn: -1, ImpactUSD: s.SubagentUsage.CostUSD * 0.3,
			Text: fmt.Sprintf("Subagents account for %.0f%% of session cost (%d subagents).", s.SubagentUsage.CostUSD/u.CostUSD*100, len(s.Subagents)),
			Fix:  "Write a tight brief and choose a smaller model for simple agents."})
	}
	// Model choice.
	var large, small int
	var smallCost float64
	all := append([]model.Call{}, s.Calls...)
	for _, a := range s.Subagents {
		all = append(all, a.Calls...)
	}
	for _, c := range all {
		if !isLarge(c.Model) {
			continue
		}
		large++
		if c.Usage.Output < th.SmallOutput {
			small++
			smallCost += c.Usage.CostUSD
		}
	}
	if large >= 20 && float64(small)*100/float64(large) > th.SmallOutputPct {
		out = append(out, Finding{Rule: "model", Severity: "medium", Title: "Large model on small steps", Turn: -1, ImpactUSD: smallCost * 0.5,
			Text: fmt.Sprintf("%d of %d calls on a large model produced fewer than %d output tokens (simple reads, edits, commands).", small, large, th.SmallOutput),
			Fix:  "Use a smaller model (or lower effort) for mechanical work and subagents."})
	}
	// Reported vs estimated.
	if s.ReportedCost != nil && *s.ReportedCost > 0 {
		d := (u.CostUSD / *s.ReportedCost - 1) * 100
		if d > 5 || d < -5 {
			out = append(out, Finding{Rule: "reported", Severity: "ok", Title: "Estimate differs from Claude Code's total", Turn: -1,
				Text: fmt.Sprintf("Claude Code recorded $%.2f; this estimate is %+.1f%%. Check the pricing table for the models used.", *s.ReportedCost, d)})
		}
	}
	Sort(out)
	return out
}

func Sort(fs []Finding) {
	order := map[string]int{"high": 0, "medium": 1, "ok": 2}
	sort.SliceStable(fs, func(i, j int) bool {
		if order[fs[i].Severity] != order[fs[j].Severity] {
			return order[fs[i].Severity] < order[fs[j].Severity]
		}
		return fs[i].ImpactUSD > fs[j].ImpactUSD
	})
}

// Group collapses per-turn findings of the same rule into one, keeping the
// worst turn as evidence, the highest severity and the summed impact.
func Group(fs []Finding) []Finding {
	type g struct {
		worst  Finding
		sev    string
		impact float64
		n      int
		turns  map[int]bool
	}
	rank := map[string]int{"high": 0, "medium": 1, "ok": 2}
	var out []Finding
	var keys []string
	groups := map[string]*g{}
	for _, f := range fs {
		if f.Turn < 0 {
			out = append(out, f)
			continue
		}
		key := f.Rule + "|" + f.SubagentID
		x, ok := groups[key]
		if !ok {
			x = &g{worst: f, sev: f.Severity, turns: map[int]bool{}}
			groups[key] = x
			keys = append(keys, key)
		} else if rank[f.Severity] < rank[x.worst.Severity] || (f.Severity == x.worst.Severity && f.ImpactUSD > x.worst.ImpactUSD) {
			x.worst = f
		}
		if rank[f.Severity] < rank[x.sev] {
			x.sev = f.Severity
		}
		x.impact += f.ImpactUSD
		x.n++
		x.turns[f.Turn] = true
	}
	for _, k := range keys {
		x := groups[k]
		f := x.worst
		f.Severity, f.ImpactUSD = x.sev, x.impact
		if x.n > 1 {
			f.Text += fmt.Sprintf(" Found %d times across %d turns; this is the worst one.", x.n, len(x.turns))
		}
		out = append(out, f)
	}
	Sort(out)
	return out
}

func Count(fs []Finding) (high, med int) {
	for _, f := range fs {
		switch f.Severity {
		case "high":
			high++
		case "medium":
			med++
		}
	}
	return
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	out := strings.Join(f, " ")
	if strings.HasPrefix(out, "/") {
		out = filepath.Base(out)
	}
	return out
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
