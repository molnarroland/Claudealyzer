package core

import (
	"errors"
	"math"
	"sort"
	"time"

	"claudealyzer/internal/insight"
	"claudealyzer/internal/model"
)

// These reports work on session summaries only, so they stay fast on large histories.

// RuleRow is one insight rule summed over many sessions.
type RuleRow struct {
	Rule      string  `json:"rule"`
	Title     string  `json:"title"`
	Sessions  int     `json:"sessions"` // sessions where it fired
	Findings  int     `json:"findings"`
	High      int     `json:"high"`
	ImpactUSD float64 `json:"impactUSD"`
	Share     float64 `json:"share"` // of the sessions in scope
}

func ruleRows(sums []model.SessionSummary) []RuleRow {
	m := map[string]*RuleRow{}
	for _, s := range sums {
		for r, st := range s.Rules {
			x := m[r]
			if x == nil {
				x = &RuleRow{Rule: r, Title: insight.RuleTitle(r)}
				m[r] = x
			}
			x.Sessions++
			x.Findings += st.Count
			x.High += st.High
			x.ImpactUSD += st.ImpactUSD
		}
	}
	out := make([]RuleRow, 0, len(m))
	for _, x := range m {
		if len(sums) > 0 {
			x.Share = float64(x.Sessions) / float64(len(sums))
		}
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ImpactUSD != out[j].ImpactUSD {
			return out[i].ImpactUSD > out[j].ImpactUSD
		}
		return out[i].Sessions > out[j].Sessions
	})
	return out
}

// ---------- project dashboard ----------

type FileCount struct {
	Path     string `json:"path"`
	Edits    int    `json:"edits"`
	Sessions int    `json:"sessions"`
}

type ModelCost struct {
	Model   string  `json:"model"`
	CostUSD float64 `json:"costUSD"`
	Tokens  int64   `json:"tokens"`
}

type WeekRow struct {
	Week     string  `json:"week"` // Monday, YYYY-MM-DD
	CostUSD  float64 `json:"costUSD"`
	Sessions int     `json:"sessions"`
	Prompts  int     `json:"prompts"`
}

type ProjectReport struct {
	Key          string       `json:"key"`
	Name         string       `json:"name"`
	Path         string       `json:"path"`
	Agents       []string     `json:"agents"`
	Sessions     int          `json:"sessions"`
	Prompts      int          `json:"prompts"`
	Usage        model.Usage  `json:"usage"`
	Effective    float64      `json:"effective"`
	HasUnset     bool         `json:"hasUnset"`
	FirstActive  int64        `json:"firstActive"`
	LastActive   int64        `json:"lastActive"`
	ActiveDays   int          `json:"activeDays"`
	AvgUSD       float64      `json:"avgUSD"`
	CacheHit     float64      `json:"cacheHit"`
	HighSessions int          `json:"highSessions"`
	Days         []DayRow     `json:"days"`
	Weeks        []WeekRow    `json:"weeks"`
	TopSessions  []SessionRow `json:"topSessions"`
	Files        []FileCount  `json:"files"`
	Rules        []RuleRow    `json:"rules"`
	Models       []ModelCost  `json:"models"`
}

// ProjectReport sums up one project. days limits the daily chart and every total (0 = all time).
func (c *Core) ProjectReport(projectKey, sourceID, agent string, days int, f Filter) (*ProjectReport, error) {
	if projectKey == "" {
		return nil, errors.New("no project selected")
	}
	if days > 0 && !f.dated() {
		f.From = time.Now().AddDate(0, 0, -(days - 1)).Format(dayFmt)
	}
	sums := c.scoped(projectKey, sourceID, agent, f)
	r := &ProjectReport{Key: projectKey, Name: projectName(projectKey), Path: ShortHome(projectKey)}
	if len(sums) == 0 {
		return r, nil
	}
	names := map[string]string{}
	for _, s := range c.sources() {
		names[s.ID] = s.Name
	}
	b := c.newBilling()
	agents := map[string]bool{}
	byDay := map[string]map[string]float64{}
	weeks := map[string]*WeekRow{}
	files := map[string]*FileCount{}
	models := map[string]*ModelCost{}
	var rows []SessionRow
	for _, s := range sums {
		agents[string(s.Agent)] = true
		r.Sessions++
		if s.FindingsHigh > 0 {
			r.HighSessions++
		}
		if r.FirstActive == 0 || (s.StartedAt > 0 && s.StartedAt < r.FirstActive) {
			r.FirstActive = s.StartedAt
		}
		if s.EndedAt > r.LastActive {
			r.LastActive = s.EndedAt
		}
		for d, ds := range s.Days {
			if !f.inRange(d) {
				continue
			}
			r.Prompts += ds.Prompts
			r.Usage.Add(ds.Usage)
			r.Effective += b.cost(s.SourceID, d, ds.Usage.CostUSD)
			if byDay[d] == nil {
				byDay[d] = map[string]float64{}
			}
			byDay[d][string(s.Agent)] += ds.Usage.CostUSD
			wk := weekOf(d)
			w := weeks[wk]
			if w == nil {
				w = &WeekRow{Week: wk}
				weeks[wk] = w
			}
			w.CostUSD += ds.Usage.CostUSD
			w.Prompts += ds.Prompts
		}
		if s.StartedAt > 0 {
			if w := weeks[weekOf(dayOf(s.StartedAt))]; w != nil {
				w.Sessions++
			}
		}
		for p, n := range s.EditedFiles {
			x := files[p]
			if x == nil {
				x = &FileCount{Path: ShortHome(p)}
				files[p] = x
			}
			x.Edits += n
			x.Sessions++
		}
		for m, u := range s.ByModel {
			x := models[m]
			if x == nil {
				x = &ModelCost{Model: m}
				models[m] = x
			}
			x.CostUSD += u.CostUSD
			x.Tokens += u.Total()
		}
		rows = append(rows, SessionRow{SessionSummary: s, SourceName: names[s.SourceID], EffectiveUSD: c.effective(s.SourceID, s.Usage.CostUSD, s.StartedAt).Effective})
	}
	r.HasUnset = b.unset
	r.ActiveDays = len(byDay)
	r.AvgUSD = r.Usage.CostUSD / float64(r.Sessions)
	if ctx := r.Usage.Context(); ctx > 0 {
		r.CacheHit = float64(r.Usage.CacheRead) / float64(ctx)
	}
	for _, a := range []string{"claude", "codex", "gemini", "opencode"} {
		if agents[a] {
			r.Agents = append(r.Agents, a)
		}
	}
	// Daily series: every day from the first active (or the range start) to today.
	// The series starts at the first active day in range, so a short history isn't squeezed to one side.
	first := ""
	for d := range byDay {
		if first == "" || d < first {
			first = d
		}
	}
	last := time.Now().Format(dayFmt)
	if f.To != "" && f.To < last {
		last = f.To
	}
	if start, err := time.ParseInLocation(dayFmt, first, time.Local); err == nil {
		end, _ := time.ParseInLocation(dayFmt, last, time.Local)
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			k := d.Format(dayFmt)
			row := DayRow{Date: k, ByAgent: byDay[k]}
			if row.ByAgent == nil {
				row.ByAgent = map[string]float64{}
			}
			for _, v := range row.ByAgent {
				row.Total += v
			}
			r.Days = append(r.Days, row)
		}
	}
	for _, w := range weeks {
		r.Weeks = append(r.Weeks, *w)
	}
	sort.Slice(r.Weeks, func(i, j int) bool { return r.Weeks[i].Week < r.Weeks[j].Week })
	sort.Slice(rows, func(i, j int) bool { return rows[i].Usage.CostUSD > rows[j].Usage.CostUSD })
	if len(rows) > 10 {
		rows = rows[:10]
	}
	r.TopSessions = rows
	for _, x := range files {
		r.Files = append(r.Files, *x)
	}
	sort.Slice(r.Files, func(i, j int) bool {
		if r.Files[i].Edits != r.Files[j].Edits {
			return r.Files[i].Edits > r.Files[j].Edits
		}
		return r.Files[i].Path < r.Files[j].Path
	})
	if len(r.Files) > 15 {
		r.Files = r.Files[:15]
	}
	r.Rules = ruleRows(sums)
	for _, x := range models {
		if x.Model != "" && x.Model != "<synthetic>" {
			r.Models = append(r.Models, *x)
		}
	}
	sort.Slice(r.Models, func(i, j int) bool { return r.Models[i].CostUSD > r.Models[j].CostUSD })
	return r, nil
}

// weekOf returns the Monday of a YYYY-MM-DD day's week.
func weekOf(day string) string {
	t, err := time.ParseInLocation(dayFmt, day, time.Local)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)).Format(dayFmt)
}

// ---------- compare ----------

// Stats describes a session or a period, so two of them can be put side by side.
type Stats struct {
	Label        string             `json:"label"`
	Sub          string             `json:"sub"`
	From         string             `json:"from"`
	To           string             `json:"to"`
	Sessions     int                `json:"sessions"`
	Prompts      int                `json:"prompts"`
	Turns        int                `json:"turns"`
	Subagents    int                `json:"subagents"`
	Usage        model.Usage        `json:"usage"`
	Effective    float64            `json:"effective"`
	AvgUSD       float64            `json:"avgUSD"` // per session
	PerPrompt    float64            `json:"perPrompt"`
	CacheHit     float64            `json:"cacheHit"`
	PeakContext  int64              `json:"peakContext"` // highest
	AvgPeak      int64              `json:"avgPeak"`
	WallMs       int64              `json:"wallMs"`
	SubagentUSD  float64            `json:"subagentUSD"`
	FindingsHigh int                `json:"findingsHigh"`
	FindingsMed  int                `json:"findingsMed"`
	Rules        []RuleRow          `json:"rules"`
	ByModel      map[string]float64 `json:"byModel"`
	ByAgent      map[string]float64 `json:"byAgent"`
}

type Comparison struct {
	Kind string `json:"kind"` // sessions | periods
	A    Stats  `json:"a"`
	B    Stats  `json:"b"`
}

func (c *Core) summary(sourceID, id string) (model.SessionSummary, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e := c.idx[sourceID][id]
	if e == nil {
		return model.SessionSummary{}, false
	}
	return e.Sum, true
}

// stats sums sessions; with a date range, only their activity on those days counts.
func (c *Core) stats(sums []model.SessionSummary, f Filter) Stats {
	st := Stats{From: f.From, To: f.To, ByModel: map[string]float64{}, ByAgent: map[string]float64{}}
	b := c.newBilling()
	var peaks int64
	for _, s := range sums {
		st.Sessions++
		st.Turns += s.TurnCount
		st.Subagents += s.SubagentCount
		st.FindingsHigh += s.FindingsHigh
		st.FindingsMed += s.FindingsMed
		st.SubagentUSD += s.SubagentUsage.CostUSD
		st.PeakContext = max(st.PeakContext, s.PeakContext)
		peaks += s.PeakContext
		if s.EndedAt > s.StartedAt && s.StartedAt > 0 {
			st.WallMs += s.EndedAt - s.StartedAt
		}
		share := 1.0
		if f.dated() {
			share = 0
		}
		for d, ds := range s.Days {
			if !f.inRange(d) {
				continue
			}
			st.Prompts += ds.Prompts
			st.Usage.Add(ds.Usage)
			st.Effective += b.cost(s.SourceID, d, ds.Usage.CostUSD)
			st.ByAgent[string(s.Agent)] += ds.Usage.CostUSD
			if f.dated() && s.Usage.CostUSD > 0 {
				share += ds.Usage.CostUSD / s.Usage.CostUSD
			}
		}
		if len(s.Days) == 0 && !f.dated() {
			st.Usage.Add(s.Usage)
		}
		// Models are only known per session: split by the share of the session in range.
		for m, u := range s.ByModel {
			if m != "" && m != "<synthetic>" {
				st.ByModel[m] += u.CostUSD * math.Min(share, 1)
			}
		}
	}
	if st.Sessions > 0 {
		st.AvgUSD = st.Usage.CostUSD / float64(st.Sessions)
		st.AvgPeak = peaks / int64(st.Sessions)
	}
	if st.Prompts > 0 {
		st.PerPrompt = st.Usage.CostUSD / float64(st.Prompts)
	}
	if ctx := st.Usage.Context(); ctx > 0 {
		st.CacheHit = float64(st.Usage.CacheRead) / float64(ctx)
	}
	st.Rules = ruleRows(sums)
	return st
}

// CompareSessions puts two sessions side by side.
func (c *Core) CompareSessions(aSource, aID, bSource, bID string) (*Comparison, error) {
	a, ok := c.summary(aSource, aID)
	if !ok {
		return nil, errors.New("first session not found")
	}
	bb, ok := c.summary(bSource, bID)
	if !ok {
		return nil, errors.New("second session not found")
	}
	out := &Comparison{Kind: "sessions", A: c.stats([]model.SessionSummary{a}, Filter{}), B: c.stats([]model.SessionSummary{bb}, Filter{})}
	out.A.Label, out.A.Sub = a.Title, projectName(a.ProjectKey)+" · "+dayOf(a.StartedAt)
	out.B.Label, out.B.Sub = bb.Title, projectName(bb.ProjectKey)+" · "+dayOf(bb.StartedAt)
	return out, nil
}

// ComparePeriods compares two date ranges (YYYY-MM-DD, inclusive) of one scope.
func (c *Core) ComparePeriods(projectKey, sourceID, agent, modelID, aFrom, aTo, bFrom, bTo string) (*Comparison, error) {
	if aFrom == "" || aTo == "" || bFrom == "" || bTo == "" {
		return nil, errors.New("both periods need a start and an end")
	}
	fa := Filter{Model: modelID, From: aFrom, To: aTo}
	fb := Filter{Model: modelID, From: bFrom, To: bTo}
	out := &Comparison{Kind: "periods", A: c.stats(c.scoped(projectKey, sourceID, agent, fa), fa), B: c.stats(c.scoped(projectKey, sourceID, agent, fb), fb)}
	scope := c.scopeTitle(projectKey, sourceID)
	out.A.Label, out.A.Sub = aFrom+" – "+aTo, scope
	out.B.Label, out.B.Sub = bFrom+" – "+bTo, scope
	return out, nil
}

// ---------- insight trends ----------

type RuleTrend struct {
	Rule      string    `json:"rule"`
	Title     string    `json:"title"`
	Counts    []int     `json:"counts"` // sessions where it fired, per week
	Shares    []float64 `json:"shares"` // of that week's sessions
	Impact    []float64 `json:"impact"`
	Total     int       `json:"total"`
	ImpactUSD float64   `json:"impactUSD"`
	Recent    float64   `json:"recent"` // share over the last 4 weeks
	Before    float64   `json:"before"` // share over the 4 weeks before
	Trend     string    `json:"trend"`  // better | worse | steady | new | none
}

type TrendReport struct {
	Title    string      `json:"title"`
	Weeks    []string    `json:"weeks"` // Mondays
	Sessions []int       `json:"sessions"`
	CostUSD  []float64   `json:"costUSD"`
	Impact   []float64   `json:"impact"`  // estimated impact of all findings, per week
	Flagged  []float64   `json:"flagged"` // share of sessions with a high or medium finding
	Rules    []RuleTrend `json:"rules"`
}

// InsightTrends shows, week by week, how often each rule fired, so you can see whether changes help.
func (c *Core) InsightTrends(projectKey, sourceID, agent string, weeks int, f Filter) *TrendReport {
	if weeks <= 0 {
		weeks = 12
	}
	now := time.Now()
	first, _ := time.ParseInLocation(dayFmt, weekOf(now.Format(dayFmt)), time.Local)
	first = first.AddDate(0, 0, -7*(weeks-1))
	tr := &TrendReport{Title: c.scopeTitle(projectKey, sourceID)}
	idx := map[string]int{}
	for i := 0; i < weeks; i++ {
		w := first.AddDate(0, 0, 7*i).Format(dayFmt)
		idx[w] = i
		tr.Weeks = append(tr.Weeks, w)
	}
	tr.Sessions = make([]int, weeks)
	tr.CostUSD = make([]float64, weeks)
	tr.Impact = make([]float64, weeks)
	tr.Flagged = make([]float64, weeks)
	flagged := make([]int, weeks)
	rules := map[string]*RuleTrend{}
	for _, s := range c.scoped(projectKey, sourceID, agent, f) {
		if s.StartedAt == 0 {
			continue
		}
		i, ok := idx[weekOf(dayOf(s.StartedAt))]
		if !ok {
			continue
		}
		tr.Sessions[i]++
		tr.CostUSD[i] += s.Usage.CostUSD
		if s.FindingsHigh+s.FindingsMed > 0 {
			flagged[i]++
		}
		for r, st := range s.Rules {
			x := rules[r]
			if x == nil {
				x = &RuleTrend{Rule: r, Title: insight.RuleTitle(r), Counts: make([]int, weeks), Shares: make([]float64, weeks), Impact: make([]float64, weeks)}
				rules[r] = x
			}
			x.Counts[i]++
			x.Impact[i] += st.ImpactUSD
			x.Total++
			x.ImpactUSD += st.ImpactUSD
			tr.Impact[i] += st.ImpactUSD
		}
	}
	for i := range tr.Weeks {
		if tr.Sessions[i] > 0 {
			tr.Flagged[i] = float64(flagged[i]) / float64(tr.Sessions[i])
		}
	}
	window := func(x *RuleTrend, from, to int) (float64, int) {
		n, k := 0, 0
		for i := max(from, 0); i < to && i < weeks; i++ {
			n += tr.Sessions[i]
			k += x.Counts[i]
		}
		if n == 0 {
			return 0, 0
		}
		return float64(k) / float64(n), n
	}
	for _, x := range rules {
		for i := range x.Counts {
			if tr.Sessions[i] > 0 {
				x.Shares[i] = float64(x.Counts[i]) / float64(tr.Sessions[i])
			}
		}
		var nr, nb int
		x.Recent, nr = window(x, weeks-4, weeks)
		x.Before, nb = window(x, weeks-8, weeks-4)
		switch {
		case nr == 0 || nb == 0:
			x.Trend = "none"
		case x.Before == 0 && x.Recent > 0:
			x.Trend = "new"
		case x.Recent < x.Before*0.8 && x.Before-x.Recent >= 0.05:
			x.Trend = "better"
		case x.Recent > x.Before*1.2 && x.Recent-x.Before >= 0.05:
			x.Trend = "worse"
		default:
			x.Trend = "steady"
		}
		tr.Rules = append(tr.Rules, *x)
	}
	sort.Slice(tr.Rules, func(i, j int) bool {
		if tr.Rules[i].ImpactUSD != tr.Rules[j].ImpactUSD {
			return tr.Rules[i].ImpactUSD > tr.Rules[j].ImpactUSD
		}
		return tr.Rules[i].Total > tr.Rules[j].Total
	})
	return tr
}
