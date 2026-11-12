package core

import (
	"runtime"
	"sort"
	"sync"
	"time"

	"claudealyzer/internal/model"
)

// ---------- calendar ----------

const dayFmt = "2006-01-02"

// billing turns list prices into what was paid, per source and day.
// Subscription periods are summed once per source and reused.
type billing struct {
	c      *Core
	daily  map[string]map[string]float64
	period map[string]float64 // sourceID + period start -> period list price
	unset  bool
}

func (c *Core) newBilling() *billing {
	return &billing{c: c, daily: map[string]map[string]float64{}, period: map[string]float64{}}
}

func (b *billing) cost(sourceID, day string, list float64) float64 {
	src, _ := b.c.source(sourceID)
	bl := src.Billing
	switch bl.Kind {
	case "free":
		return 0
	case "api":
		return list * (1 - bl.DiscountPct/100)
	case "subscription", "team":
		at, err := time.ParseInLocation(dayFmt, day, time.Local)
		if err != nil {
			return 0
		}
		start, end := period(bl.RenewDay, at)
		k := sourceID + start.Format(dayFmt)
		pl, ok := b.period[k]
		if !ok {
			d := b.daily[sourceID]
			if d == nil {
				d = b.c.sourceDaily(sourceID)
				b.daily[sourceID] = d
			}
			pl = sumDays(d, start, end)
			b.period[k] = pl
		}
		if pl <= 0 {
			return 0
		}
		return bl.Fee * list / pl
	default:
		b.unset = true
		return list
	}
}

type CalendarDay struct {
	Date      string             `json:"date"`
	Prompts   int                `json:"prompts"`
	Sessions  int                `json:"sessions"`
	Usage     model.Usage        `json:"usage"`
	Effective float64            `json:"effective"`
	ByAgent   map[string]float64 `json:"byAgent"`
}

type Calendar struct {
	Title      string        `json:"title"`
	Month      string        `json:"month"` // YYYY-MM
	Days       []CalendarDay `json:"days"`  // every day of the month, in order
	Prompts    int           `json:"prompts"`
	Sessions   int           `json:"sessions"`
	Usage      model.Usage   `json:"usage"`
	Effective  float64       `json:"effective"`
	ActiveDays int           `json:"activeDays"`
	MaxCost    float64       `json:"maxCost"`
	MaxPrompts int           `json:"maxPrompts"`
	MaxTokens  int64         `json:"maxTokens"`
	First      string        `json:"first"` // earliest month with data (YYYY-MM)
	Last       string        `json:"last"`  // latest month with data (YYYY-MM)
	HasUnset   bool          `json:"hasUnset"`
}

func (c *Core) scopeTitle(projectKey, sourceID string) string {
	if projectKey != "" {
		return projectName(projectKey)
	}
	if src, ok := c.source(sourceID); ok {
		return src.Name
	}
	return "All sources"
}

func (c *Core) scoped(projectKey, sourceID, agent string) []model.SessionSummary {
	var out []model.SessionSummary
	for _, s := range c.summaries(sourceID, agent) {
		if projectKey == "" || s.ProjectKey == projectKey {
			out = append(out, s)
		}
	}
	return out
}

// Calendar returns one month of daily activity. month is YYYY-MM ("" = current month).
func (c *Core) Calendar(projectKey, sourceID, agent, month string) *Calendar {
	start, err := time.ParseInLocation("2006-01", month, time.Local)
	if err != nil {
		n := time.Now()
		start = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.Local)
	}
	end := start.AddDate(0, 1, 0)
	cal := &Calendar{Title: c.scopeTitle(projectKey, sourceID), Month: start.Format("2006-01")}
	idx := map[string]int{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		k := d.Format(dayFmt)
		idx[k] = len(cal.Days)
		cal.Days = append(cal.Days, CalendarDay{Date: k, ByAgent: map[string]float64{}})
	}
	b := c.newBilling()
	sessions := map[string]bool{}
	for _, s := range c.scoped(projectKey, sourceID, agent) {
		for k, ds := range s.Days {
			m := k[:7]
			if cal.First == "" || m < cal.First {
				cal.First = m
			}
			if m > cal.Last {
				cal.Last = m
			}
			i, ok := idx[k]
			if !ok {
				continue
			}
			d := &cal.Days[i]
			d.Prompts += ds.Prompts
			d.Sessions++
			d.Usage.Add(ds.Usage)
			d.Effective += b.cost(s.SourceID, k, ds.Usage.CostUSD)
			d.ByAgent[string(s.Agent)] += ds.Usage.CostUSD
			sessions[s.SourceID+"/"+s.ID] = true
		}
	}
	for _, d := range cal.Days {
		cal.Prompts += d.Prompts
		cal.Usage.Add(d.Usage)
		cal.Effective += d.Effective
		if d.Sessions > 0 {
			cal.ActiveDays++
		}
		cal.MaxCost = max(cal.MaxCost, d.Usage.CostUSD)
		cal.MaxPrompts = max(cal.MaxPrompts, d.Prompts)
		cal.MaxTokens = max(cal.MaxTokens, d.Usage.Total())
	}
	cal.Sessions = len(sessions)
	cal.HasUnset = b.unset
	return cal
}

// ---------- one day ----------

type DayPrompt struct {
	SourceID   string      `json:"sourceId"`
	SessionID  string      `json:"sessionId"`
	ProjectKey string      `json:"projectKey"`
	Session    string      `json:"session"`
	Agent      model.Agent `json:"agent"`
	Turn       int         `json:"turn"`
	Prompt     string      `json:"prompt"`
	At         int64       `json:"at"`
	DurationMs int64       `json:"durationMs"`
	Steps      int         `json:"steps"`
	Usage      model.Usage `json:"usage"` // the turn's own calls plus its subagents
}

type DaySession struct {
	SessionRow
	Project      string      `json:"project"`
	DayPrompts   int         `json:"dayPrompts"`
	DayUsage     model.Usage `json:"dayUsage"`
	DayEffective float64     `json:"dayEffective"`
}

type DayHour struct {
	Hour    int     `json:"hour"`
	Prompts int     `json:"prompts"`
	CostUSD float64 `json:"costUSD"`
}

type DayDetail struct {
	Date      string             `json:"date"`
	Title     string             `json:"title"`
	Prompts   int                `json:"prompts"`
	Usage     model.Usage        `json:"usage"`
	Effective float64            `json:"effective"`
	HasUnset  bool               `json:"hasUnset"`
	ByAgent   map[string]float64 `json:"byAgent"`
	ByModel   map[string]float64 `json:"byModel"`
	Hours     []DayHour          `json:"hours"`
	Sessions  []DaySession       `json:"sessions"`
	List      []DayPrompt        `json:"list"`
	Failed    int                `json:"failed"` // sessions that could not be read for the prompt list
}

// Day returns everything that happened on one day (YYYY-MM-DD).
// Sessions are re-read from disk for the prompt list and the hourly split.
func (c *Core) Day(projectKey, sourceID, agent, date string) *DayDetail {
	dd := &DayDetail{Date: date, Title: c.scopeTitle(projectKey, sourceID), ByAgent: map[string]float64{}, ByModel: map[string]float64{}}
	names := map[string]string{}
	for _, s := range c.sources() {
		names[s.ID] = s.Name
	}
	b := c.newBilling()
	var active []model.SessionSummary
	for _, s := range c.scoped(projectKey, sourceID, agent) {
		ds, ok := s.Days[date]
		if !ok {
			continue
		}
		eff := b.cost(s.SourceID, date, ds.Usage.CostUSD)
		row := SessionRow{SessionSummary: s, SourceName: names[s.SourceID], EffectiveUSD: c.effective(s.SourceID, s.Usage.CostUSD, s.StartedAt).Effective}
		dd.Sessions = append(dd.Sessions, DaySession{SessionRow: row, Project: projectName(s.ProjectKey), DayPrompts: ds.Prompts, DayUsage: ds.Usage, DayEffective: eff})
		dd.Prompts += ds.Prompts
		dd.Usage.Add(ds.Usage)
		dd.Effective += eff
		dd.ByAgent[string(s.Agent)] += ds.Usage.CostUSD
		active = append(active, s)
	}
	dd.HasUnset = b.unset
	sort.Slice(dd.Sessions, func(i, j int) bool { return dd.Sessions[i].DayUsage.CostUSD > dd.Sessions[j].DayUsage.CostUSD })

	hours := make([]DayHour, 24)
	for i := range hours {
		hours[i].Hour = i
	}
	var mu sync.Mutex
	sem := make(chan struct{}, max(2, runtime.NumCPU()/2))
	var wg sync.WaitGroup
	for _, sum := range active {
		wg.Add(1)
		sem <- struct{}{}
		go func(sum model.SessionSummary) {
			defer wg.Done()
			defer func() { <-sem }()
			s, err := c.parse(sum.SourceID, sum.ID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				dd.Failed++
				return
			}
			onDay := func(ms int64) (time.Time, bool) {
				t := time.UnixMilli(ms).Local()
				return t, ms > 0 && t.Format(dayFmt) == date
			}
			calls := append([]model.Call{}, s.Calls...)
			for _, a := range s.Subagents {
				calls = append(calls, a.Calls...)
			}
			for _, cl := range calls {
				if t, ok := onDay(cl.Time); ok {
					hours[t.Hour()].CostUSD += cl.Usage.CostUSD
					if cl.Model != "" && cl.Model != "<synthetic>" {
						dd.ByModel[cl.Model] += cl.Usage.CostUSD
					}
				}
			}
			for ti, t := range s.Turns {
				at, ok := onDay(t.StartedAt)
				if !ok || t.Prompt == "" || t.Prompt == "(session start)" {
					continue
				}
				hours[at.Hour()].Prompts++
				u := t.Usage
				u.Add(t.SubagentUsage)
				dd.List = append(dd.List, DayPrompt{
					SourceID: sum.SourceID, SessionID: sum.ID, ProjectKey: sum.ProjectKey, Session: sum.Title, Agent: sum.Agent,
					Turn: ti, Prompt: oneLine(t.Prompt, 240), At: t.StartedAt, DurationMs: t.DurationMs, Steps: len(t.Steps), Usage: u,
				})
			}
		}(sum)
	}
	wg.Wait()
	sort.Slice(dd.List, func(i, j int) bool { return dd.List[i].At < dd.List[j].At })
	dd.Hours = hours
	return dd
}
