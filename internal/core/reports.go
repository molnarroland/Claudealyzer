package core

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"claudealyzer/internal/insight"
	"claudealyzer/internal/model"
	"claudealyzer/internal/pricing"
)

// ---------- effective cost ----------

type Effective struct {
	Kind        string  `json:"kind"`
	Label       string  `json:"label"`
	List        float64 `json:"list"`
	Effective   float64 `json:"effective"`
	Multiplier  float64 `json:"multiplier"` // list ÷ what you paid; 0 = n/a
	PeriodStart string  `json:"periodStart"`
	PeriodEnd   string  `json:"periodEnd"`
	PeriodList  float64 `json:"periodList"`
	Fee         float64 `json:"fee"`
	Share       float64 `json:"share"` // share of the period's list price
}

func period(renewDay int, at time.Time) (time.Time, time.Time) {
	if renewDay < 1 || renewDay > 28 {
		renewDay = 1
	}
	y, m, d := at.Date()
	start := time.Date(y, m, renewDay, 0, 0, 0, 0, time.Local)
	if d < renewDay {
		start = start.AddDate(0, -1, 0)
	}
	return start, start.AddDate(0, 1, 0)
}

func (c *Core) sourceDaily(sourceID string) map[string]float64 {
	out := map[string]float64{}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.idx[sourceID] {
		for d, v := range e.Sum.Daily {
			out[d] += v
		}
	}
	return out
}

func sumDays(daily map[string]float64, start, end time.Time) float64 {
	s, e := start.Format("2006-01-02"), end.Format("2006-01-02")
	t := 0.0
	for d, v := range daily {
		if d >= s && d < e {
			t += v
		}
	}
	return t
}

func (c *Core) effective(sourceID string, list float64, at int64) Effective {
	src, _ := c.source(sourceID)
	b := src.Billing
	e := Effective{Kind: b.Kind, List: list, Effective: list}
	switch b.Kind {
	case "free":
		e.Label, e.Effective = "Free tier", 0
	case "subscription", "team":
		when := time.UnixMilli(at)
		if at == 0 {
			when = time.Now()
		}
		start, end := period(b.RenewDay, when)
		e.PeriodStart, e.PeriodEnd = start.Format("2006-01-02"), end.Format("2006-01-02")
		e.PeriodList = sumDays(c.sourceDaily(sourceID), start, end)
		e.Fee = b.Fee
		e.Label = "Subscription"
		if b.Kind == "team" {
			e.Label = "Team seat"
		}
		if b.Plan != "" {
			e.Label += " · " + b.Plan
		}
		if e.PeriodList > 0 {
			e.Share = list / e.PeriodList
			e.Effective = b.Fee * e.Share
		} else {
			e.Effective = 0
		}
		if b.Fee > 0 {
			e.Multiplier = e.PeriodList / b.Fee
		}
	case "api":
		e.Label = "API key"
		e.Effective = list * (1 - b.DiscountPct/100)
		e.Multiplier = 1
	default:
		e.Kind = "unset"
		e.Label = "Billing not set (shown as API price)"
		e.Multiplier = 1
	}
	return e
}

// ---------- step detail ----------

type StepDetail struct {
	Step           model.Step        `json:"step"`
	Turn           int               `json:"turn"`
	TurnSteps      int               `json:"turnSteps"`
	TurnDurationMs int64             `json:"turnDurationMs"`
	Output         string            `json:"output"`
	Repeats        int               `json:"repeats"`
	RepeatsMs      int64             `json:"repeatsMs"`
	SessionRepeats int               `json:"sessionRepeats"`
	LaterCalls     int               `json:"laterCalls"`
	EstCostUSD     float64           `json:"estCostUSD"`
	Agent          model.Agent       `json:"agent"`
	Findings       []insight.Finding `json:"findings"`
}

func (c *Core) target(sourceID, sessionID, subID string) (*model.Session, []model.Turn, []model.Call, map[string]string, []insight.Finding, error) {
	s, fs, err := c.Load(sourceID, sessionID)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if subID == "" {
		return s, s.Turns, s.Calls, s.FullOutputs, fs, nil
	}
	for _, a := range s.Subagents {
		if a.ID == subID {
			var sf []insight.Finding
			for _, f := range fs {
				if f.SubagentID == subID {
					sf = append(sf, f)
				}
			}
			return s, a.Turns, a.Calls, a.FullOutputs, sf, nil
		}
	}
	return nil, nil, nil, nil, nil, errors.New("subagent not found")
}

func (c *Core) StepDetail(sourceID, sessionID, subID, stepID string) (*StepDetail, error) {
	s, turns, calls, full, fs, err := c.target(sourceID, sessionID, subID)
	if err != nil {
		return nil, err
	}
	for ti, t := range turns {
		for _, st := range t.Steps {
			if st.ID != stepID {
				continue
			}
			d := &StepDetail{Step: st, Turn: ti, TurnSteps: len(t.Steps), TurnDurationMs: t.DurationMs, Output: st.Output, Agent: s.Agent}
			if f, ok := full[st.ID]; ok {
				d.Output = f
			}
			for _, x := range t.Steps {
				if x.Title == st.Title && x.Kind == st.Kind {
					d.Repeats++
					d.RepeatsMs += x.DurationMs
				}
			}
			for _, tt := range turns {
				for _, x := range tt.Steps {
					if x.Title == st.Title && x.Kind == st.Kind {
						d.SessionRepeats++
					}
				}
			}
			for _, cl := range calls {
				if cl.Time > st.StartedAt+st.DurationMs {
					d.LaterCalls++
				}
			}
			if r, ok := pricing.Lookup(s.MainModel, st.StartedAt); ok {
				cw := r.CacheWrite1h
				if cw == 0 {
					cw = r.Input
				}
				d.EstCostUSD = float64(st.ResultTokens) * (cw + r.CacheRead*float64(d.LaterCalls)) / 1e6
			}
			for _, f := range fs {
				if f.StepID == st.ID || (f.Turn == ti && relevant(f.Rule, st)) {
					d.Findings = append(d.Findings, f)
				}
			}
			return d, nil
		}
	}
	return nil, errors.New("step not found")
}

func relevant(rule string, st model.Step) bool {
	switch rule {
	case "slow-runs":
		return st.Category == "test" || st.Category == "build"
	case "cpu":
		return st.Category == "heavy"
	case "files":
		return st.Kind == model.StepRead
	case "repeat":
		return st.Kind == model.StepShell
	}
	return false
}

// ---------- session analysis ----------

type TurnBar struct {
	Turn       int     `json:"turn"`
	Prompt     string  `json:"prompt"`
	Input      float64 `json:"input"`
	CacheWrite float64 `json:"cacheWrite"`
	CacheRead  float64 `json:"cacheRead"`
	Output     float64 `json:"output"`
	Total      float64 `json:"total"`
	Peak       int64   `json:"peak"`
	DurationMs int64   `json:"durationMs"`
}

type TimeBucket struct {
	Name string `json:"name"`
	Ms   int64  `json:"ms"`
}

type CommandRow struct {
	Command  string `json:"command"`
	Runs     int    `json:"runs"`
	TotalMs  int64  `json:"totalMs"`
	MaxMs    int64  `json:"maxMs"`
	Turn     int    `json:"turn"`
	StepID   string `json:"stepId"`
	Category string `json:"category"`
	Failures int    `json:"failures"`
}

type FileRow struct {
	Path      string  `json:"path"`
	Reads     int     `json:"reads"`
	Tokens    int64   `json:"tokens"`
	ResentUSD float64 `json:"resentUSD"`
}

type Analysis struct {
	Title         string            `json:"title"`
	Agent         model.Agent       `json:"agent"`
	Model         string            `json:"model"`
	IsSubagent    bool              `json:"isSubagent"`
	Usage         model.Usage       `json:"usage"`
	WallMs        int64             `json:"wallMs"`
	ToolMs        int64             `json:"toolMs"`
	TurnCount     int               `json:"turnCount"`
	PeakContext   int64             `json:"peakContext"`
	PeakTurn      int               `json:"peakTurn"`
	SubagentUSD   float64           `json:"subagentUSD"`
	SubagentShare float64           `json:"subagentShare"`
	ContextLimit  int64             `json:"contextLimit"`
	Turns         []TurnBar         `json:"turns"`
	Time          []TimeBucket      `json:"time"`
	Commands      []CommandRow      `json:"commands"`
	Files         []FileRow         `json:"files"`
	Findings      []insight.Finding `json:"findings"`
	HasCacheWrite bool              `json:"hasCacheWrite"`
}

func (c *Core) Analysis(sourceID, sessionID, subID string) (*Analysis, error) {
	s, turns, calls, _, fs, err := c.target(sourceID, sessionID, subID)
	if err != nil {
		return nil, err
	}
	a := &Analysis{Title: s.Title, Agent: s.Agent, Model: s.MainModel, TurnCount: len(turns), Findings: insight.Group(fs), ContextLimit: c.cfg.conf.Thresholds.ContextTokens}
	if subID != "" {
		a.IsSubagent = true
		for _, x := range s.Subagents {
			if x.ID == subID {
				a.Title, a.Usage, a.Model = x.Description, x.Usage, x.Model
			}
		}
	} else {
		a.Usage = s.Usage
		a.SubagentUSD = s.SubagentUsage.CostUSD
		if s.Usage.CostUSD > 0 {
			a.SubagentShare = s.SubagentUsage.CostUSD / s.Usage.CostUSD
		}
	}
	a.HasCacheWrite = a.Usage.CacheWrite() > 0
	buckets := map[string]int64{}
	cmds := map[string]*CommandRow{}
	type fr struct {
		reads  int
		tokens int64
		first  int64
	}
	files := map[string]*fr{}
	for ti, t := range turns {
		u := t.Usage
		u.Add(t.SubagentUsage)
		a.Turns = append(a.Turns, TurnBar{Turn: ti, Prompt: oneLine(t.Prompt, 120), Input: u.CostInput, CacheWrite: u.CostCacheWrite, CacheRead: u.CostCacheRead, Output: u.CostOutput, Total: u.CostUSD, Peak: t.PeakContext, DurationMs: t.DurationMs})
		if t.PeakContext > a.PeakContext {
			a.PeakContext, a.PeakTurn = t.PeakContext, ti
		}
		a.WallMs += t.DurationMs
		var tool int64
		for _, st := range t.Steps {
			d := st.DurationMs
			switch {
			case st.Kind == model.StepAgent:
				buckets["Subagents"] += d
				continue
			case st.Category == "test":
				buckets["Tests"] += d
			case st.Category == "build":
				buckets["Builds"] += d
			case st.Category == "heavy":
				buckets["Heavy commands"] += d
			case st.Kind == model.StepShell:
				buckets["Other shell"] += d
			case st.Kind == model.StepRead || st.Kind == model.StepEdit || st.Kind == model.StepSearch:
				buckets["File tools"] += d
			case st.Kind == model.StepTool:
				buckets["Other tools"] += d
			}
			tool += d
			if st.Kind == model.StepShell && st.Title != "" {
				r := cmds[st.Title]
				if r == nil {
					r = &CommandRow{Command: st.Title, Turn: ti, StepID: st.ID, Category: st.Category}
					cmds[st.Title] = r
				}
				r.Runs++
				r.TotalMs += d
				if d > r.MaxMs {
					r.MaxMs, r.Turn, r.StepID = d, ti, st.ID
				}
				if st.IsError {
					r.Failures++
				}
			}
			if st.Kind == model.StepRead {
				per := st.ResultTokens
				if n := len(st.Files); n > 1 {
					per /= int64(n)
				}
				for _, f := range st.Files {
					x := files[f]
					if x == nil {
						x = &fr{first: st.StartedAt}
						files[f] = x
					}
					x.reads++
					x.tokens += per
				}
			}
		}
		if think := t.DurationMs - tool; think > 0 {
			buckets["Model time"] += think
		}
		a.ToolMs += tool
	}
	for k, v := range buckets {
		if v > 0 {
			a.Time = append(a.Time, TimeBucket{k, v})
		}
	}
	sort.Slice(a.Time, func(i, j int) bool { return a.Time[i].Ms > a.Time[j].Ms })
	for _, r := range cmds {
		a.Commands = append(a.Commands, *r)
	}
	sort.Slice(a.Commands, func(i, j int) bool { return a.Commands[i].TotalMs > a.Commands[j].TotalMs })
	if len(a.Commands) > 8 {
		a.Commands = a.Commands[:8]
	}
	cr := 0.0
	if r, ok := pricing.Lookup(a.Model, s.StartedAt); ok {
		cr = r.CacheRead / 1e6
	}
	for p, x := range files {
		later := 0
		for _, cl := range calls {
			if cl.Time > x.first {
				later++
			}
		}
		avg := x.tokens / int64(x.reads)
		a.Files = append(a.Files, FileRow{Path: ShortHome(p), Reads: x.reads, Tokens: x.tokens, ResentUSD: float64(avg) * cr * float64(later)})
	}
	sort.Slice(a.Files, func(i, j int) bool {
		if a.Files[i].Reads != a.Files[j].Reads {
			return a.Files[i].Reads > a.Files[j].Reads
		}
		return a.Files[i].Tokens > a.Files[j].Tokens
	})
	if len(a.Files) > 10 {
		a.Files = a.Files[:10]
	}
	return a, nil
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ---------- overview ----------

type DayRow struct {
	Date    string             `json:"date"`
	ByAgent map[string]float64 `json:"byAgent"`
	Total   float64            `json:"total"`
}

type Overview struct {
	Title        string             `json:"title"`
	Path         string             `json:"path"`
	Days         []DayRow           `json:"days"`
	TotalUSD     float64            `json:"totalUSD"`
	ByAgent      map[string]float64 `json:"byAgent"`
	Sessions     int                `json:"sessions"`
	Agents       []string           `json:"agents"`
	AvgUSD       float64            `json:"avgUSD"`
	MedianUSD    float64            `json:"medianUSD"`
	HighSessions int                `json:"highSessions"`
	Rows         []SessionRow       `json:"rows"`
}

// Overview: projectKey "" means the whole source (or all sources). days 0 = all time.
func (c *Core) Overview(projectKey, sourceID, agent string, days int) *Overview {
	rows := c.Sessions(projectKey, sourceID, agent, "cost")
	o := &Overview{ByAgent: map[string]float64{}}
	if projectKey != "" {
		o.Title, o.Path = projectName(projectKey), ShortHome(projectKey)
	} else if src, ok := c.source(sourceID); ok {
		o.Title, o.Path = src.Name, ShortHome(src.HomeDir)
	} else {
		o.Title, o.Path = "All sources", fmt.Sprintf("%d sources", len(c.sources()))
	}
	today := time.Now()
	var from time.Time
	if days > 0 {
		from = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -(days - 1))
	}
	fromS := from.Format("2006-01-02")
	byDay := map[string]map[string]float64{}
	var costs []float64
	agents := map[string]bool{}
	for _, r := range rows {
		inRange := 0.0
		for d, v := range r.Daily {
			if days > 0 && d < fromS {
				continue
			}
			if byDay[d] == nil {
				byDay[d] = map[string]float64{}
			}
			byDay[d][string(r.Agent)] += v
			inRange += v
		}
		if days > 0 && inRange == 0 && r.EndedAt < from.UnixMilli() {
			continue
		}
		o.Rows = append(o.Rows, r)
		o.TotalUSD += inRange
		o.ByAgent[string(r.Agent)] += inRange
		costs = append(costs, r.Usage.CostUSD)
		agents[string(r.Agent)] = true
		if r.FindingsHigh > 0 {
			o.HighSessions++
		}
	}
	if days == 0 && len(byDay) > 0 {
		for d := range byDay {
			if fromS == "0001-01-01" || d < fromS {
				fromS = d
			}
		}
		from, _ = time.ParseInLocation("2006-01-02", fromS, time.Local)
	}
	for d := from; len(byDay) > 0 && !d.After(today) && !from.IsZero(); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		row := DayRow{Date: k, ByAgent: byDay[k]}
		if row.ByAgent == nil {
			row.ByAgent = map[string]float64{}
		}
		for _, v := range row.ByAgent {
			row.Total += v
		}
		o.Days = append(o.Days, row)
	}
	o.Sessions = len(o.Rows)
	for _, a := range []string{"claude", "codex", "gemini", "opencode"} {
		if agents[a] {
			o.Agents = append(o.Agents, a)
		}
	}
	if len(costs) > 0 {
		sum := 0.0
		for _, x := range costs {
			sum += x
		}
		o.AvgUSD = sum / float64(len(costs))
		sort.Float64s(costs)
		o.MedianUSD = costs[len(costs)/2]
	}
	return o
}

// ---------- cost explained ----------

type FormulaRow struct {
	Name   string  `json:"name"`
	Key    string  `json:"key"`
	Tokens int64   `json:"tokens"`
	Rate   float64 `json:"rate"` // blended USD / MTok
	Cost   float64 `json:"cost"`
}

type Scenario struct {
	Name      string  `json:"name"`
	Note      string  `json:"note"`
	Cost      float64 `json:"cost"`
	Highlight bool    `json:"highlight"`
}

type PeriodPoint struct {
	Date string  `json:"date"`
	Cum  float64 `json:"cum"`
}

type PeriodReport struct {
	Start       string        `json:"start"`
	End         string        `json:"end"`
	Day         int           `json:"day"`
	Days        int           `json:"days"`
	List        float64       `json:"list"`
	Fee         float64       `json:"fee"`
	Projected   float64       `json:"projected"`
	Multiplier  float64       `json:"multiplier"`
	Points      []PeriodPoint `json:"points"`
	PaidOffDate string        `json:"paidOffDate"`
	SourceName  string        `json:"sourceName"`
}

type CostReport struct {
	Title        string        `json:"title"`
	Agent        model.Agent   `json:"agent"`
	Model        string        `json:"model"`
	SourceName   string        `json:"sourceName"`
	Billing      Billing       `json:"billing"`
	Effective    Effective     `json:"effective"`
	List         float64       `json:"list"`
	Rows         []FormulaRow  `json:"rows"`
	Calls        int           `json:"calls"`
	AvgContext   int64         `json:"avgContext"`
	TotalContext int64         `json:"totalContext"`
	CacheShare   float64       `json:"cacheShare"`
	NoCache      float64       `json:"noCache"`
	Scenarios    []Scenario    `json:"scenarios"`
	Period       *PeriodReport `json:"period"`
	Rate         pricing.Rate  `json:"rate"`
	Reported     *float64      `json:"reported"`
	RateLimitPct *float64      `json:"rateLimitPct"`
	Unpriced     int64         `json:"unpriced"`
}

func (c *Core) CostReport(sourceID, sessionID string, factor float64) (*CostReport, error) {
	s, _, err := c.Load(sourceID, sessionID)
	if err != nil {
		return nil, err
	}
	src, _ := c.source(sourceID)
	u := s.Usage
	r := &CostReport{Title: s.Title, Agent: s.Agent, Model: s.MainModel, SourceName: src.Name, Billing: src.Billing, List: u.CostUSD,
		Calls: u.Calls, TotalContext: u.Context(), Reported: s.ReportedCost, RateLimitPct: s.RateLimitPct, Unpriced: u.UnpricedTokens}
	r.Effective = c.effective(sourceID, u.CostUSD, s.StartedAt)
	if u.Calls > 0 {
		r.AvgContext = u.Context() / int64(u.Calls)
	}
	if u.Context() > 0 {
		r.CacheShare = float64(u.CacheRead) / float64(u.Context())
	}
	r.Rate, _ = pricing.Lookup(s.MainModel, s.StartedAt)
	rate := func(cost float64, tok int64) float64 {
		if tok == 0 {
			return 0
		}
		return cost / float64(tok) * 1e6
	}
	r.Rows = []FormulaRow{
		{"Cache read", "cacheRead", u.CacheRead, rate(u.CostCacheRead, u.CacheRead), u.CostCacheRead},
		{"Cache write", "cacheWrite", u.CacheWrite(), rate(u.CostCacheWrite, u.CacheWrite()), u.CostCacheWrite},
		{"Output", "output", u.Output, rate(u.CostOutput, u.Output), u.CostOutput},
		{"Input", "input", u.Input, rate(u.CostInput, u.Input), u.CostInput},
	}
	sort.SliceStable(r.Rows, func(i, j int) bool { return r.Rows[i].Cost > r.Rows[j].Cost })
	all := append([]model.Call{}, s.Calls...)
	for _, a := range s.Subagents {
		all = append(all, a.Calls...)
	}
	// Without caching: every context token at full input price.
	noCache := pricing.Reprice(all, func(cl model.Call) (pricing.Rate, bool) {
		rt, ok := pricing.Lookup(cl.Model, cl.Time)
		rt.CacheRead, rt.CacheWrite5m, rt.CacheWrite1h = rt.Input, rt.Input, rt.Input
		return rt, ok
	})
	r.NoCache = noCache.CostUSD
	today := pricing.Reprice(all, func(cl model.Call) (pricing.Rate, bool) { return pricing.Lookup(cl.Model, 0) })
	r.Scenarios = append(r.Scenarios, Scenario{"API list price, rates on the day", "Published pay-as-you-go rates valid when each request ran. Same method as Claude Code's own cost total.", u.CostUSD, true})
	if math.Abs(today.CostUSD-u.CostUSD) > 0.005 {
		r.Scenarios = append(r.Scenarios, Scenario{"API list price at today's rates", "The same tokens re-priced with the current price table.", today.CostUSD, false})
	}
	r.Scenarios = append(r.Scenarios, Scenario{"Same tokens without prompt caching", "Every call pays the full input price for its whole context.", r.NoCache, false})
	tbl := pricing.Get()
	if cheap := tbl.Cheaper[r.Rate.Provider]; cheap != "" && !strings.HasPrefix(s.MainModel, cheap) {
		cr, ok := pricing.Lookup(cheap, 0)
		if ok {
			sm := pricing.Reprice(all, func(model.Call) (pricing.Rate, bool) { return cr, true })
			r.Scenarios = append(r.Scenarios, Scenario{"Same tokens on " + cheap, fmt.Sprintf("$%g / $%g per MTok. Only valid if the cheaper model would have done the job in the same number of steps.", cr.Input, cr.Output), sm.CostUSD, false})
		}
	}
	r.Scenarios = append(r.Scenarios,
		Scenario{"Through the Batch API (−50%)", "Only for unattended jobs. Interactive agent sessions can't use it.", u.CostUSD * 0.5, false},
		Scenario{fmt.Sprintf("Custom price factor (%.0f%% of list)", factor*100), "For example an announced price change. Edit the factor, or load a pricing table in Settings.", u.CostUSD * factor, false},
	)
	paid := "What you paid"
	note := "Billed per token, so equal to the list price."
	switch r.Effective.Kind {
	case "subscription", "team":
		paid += " (share of your plan)"
		note = fmt.Sprintf("$%.0f plan × %.1f%% of this billing period's list price.", r.Effective.Fee, r.Effective.Share*100)
	case "free":
		note = "Free tier: no charge."
	case "unset":
		paid += " (billing not set)"
		note = "Set this source's billing in Settings to see your real cost."
	case "api":
		if src.Billing.DiscountPct > 0 {
			note = fmt.Sprintf("List price minus your %.0f%% discount.", src.Billing.DiscountPct)
		}
	}
	r.Scenarios = append(r.Scenarios, Scenario{paid, note, r.Effective.Effective, true})

	if (src.Billing.Kind == "subscription" || src.Billing.Kind == "team") && src.Billing.Fee > 0 {
		now := time.Now()
		start, end := period(src.Billing.RenewDay, now)
		daily := c.sourceDaily(sourceID)
		pr := &PeriodReport{Start: start.Format("2006-01-02"), End: end.Format("2006-01-02"), Fee: src.Billing.Fee, SourceName: src.Name}
		pr.Days = int(end.Sub(start).Hours()/24 + 0.5)
		cum := 0.0
		for d := start; d.Before(end) && !d.After(now); d = d.AddDate(0, 0, 1) {
			k := d.Format("2006-01-02")
			cum += daily[k]
			pr.Points = append(pr.Points, PeriodPoint{k, cum})
			if pr.PaidOffDate == "" && cum >= pr.Fee {
				pr.PaidOffDate = k
			}
		}
		pr.Day = len(pr.Points)
		pr.List = cum
		if pr.Day > 0 {
			pr.Projected = cum / float64(pr.Day) * float64(pr.Days)
		}
		pr.Multiplier = cum / pr.Fee
		r.Period = pr
	}
	return r, nil
}
