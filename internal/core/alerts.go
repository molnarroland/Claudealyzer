package core

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"claudealyzer/internal/model"
)

// Notifier shows an OS notification. Errors are ignored by the caller (for example an
// unsigned macOS build that may not post notifications).
type Notifier func(title, body string) error

// SetNotifier installs the OS notification sender.
func (c *Core) SetNotifier(n Notifier) { c.notify = n }

// ---------- budgets ----------

// BudgetStatus is a budget with its spending in the current period.
type BudgetStatus struct {
	Budget      Budget      `json:"budget"`
	Scope       string      `json:"scope"` // readable scope
	PeriodStart string      `json:"periodStart"`
	PeriodEnd   string      `json:"periodEnd"` // exclusive
	Spent       float64     `json:"spent"`
	Pct         float64     `json:"pct"`
	Projected   float64     `json:"projected"` // at the end of the period, at the pace so far
	Level       string      `json:"level"`     // ok | warn | over
	Days        []BudgetDay `json:"days"`
}

type BudgetDay struct {
	Date  string  `json:"date"`
	Spent float64 `json:"spent"`
}

func budgetPeriod(period string, now time.Time) (time.Time, time.Time) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	switch period {
	case "day":
		return day, day.AddDate(0, 0, 1)
	case "week":
		wd := (int(day.Weekday()) + 6) % 7 // Monday = 0
		start := day.AddDate(0, 0, -wd)
		return start, start.AddDate(0, 0, 7)
	default:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		return start, start.AddDate(0, 1, 0)
	}
}

func (c *Core) Budgets() []Budget {
	c.cfg.mu.Lock()
	defer c.cfg.mu.Unlock()
	return append([]Budget(nil), c.cfg.conf.Budgets...)
}

func (c *Core) SaveBudgets(bs []Budget) error {
	for i := range bs {
		b := &bs[i]
		if b.LimitUSD <= 0 {
			return fmt.Errorf("budget %q needs a limit above $0", b.Name)
		}
		switch b.Period {
		case "day", "week", "month":
		default:
			b.Period = "month"
		}
		if b.Basis != "paid" {
			b.Basis = "list"
		}
		if b.ID == "" {
			b.ID = fmt.Sprintf("b%d-%d", time.Now().UnixNano()%1e9, i)
		}
	}
	c.cfg.mu.Lock()
	defer c.cfg.mu.Unlock()
	c.cfg.conf.Budgets = bs
	return c.cfg.save()
}

func (c *Core) AlertSettings() AlertSettings {
	c.cfg.mu.Lock()
	defer c.cfg.mu.Unlock()
	return c.cfg.conf.Alerts
}

func (c *Core) SaveAlertSettings(a AlertSettings) error {
	if a.WarnPct <= 0 || a.WarnPct > 100 {
		a.WarnPct = 80
	}
	if a.SessionUSD < 0 {
		return errors.New("the session alert can't be negative")
	}
	c.cfg.mu.Lock()
	defer c.cfg.mu.Unlock()
	c.cfg.conf.Alerts = a
	return c.cfg.save()
}

func (c *Core) budgetScope(b Budget) string {
	src := "All sources"
	if s, ok := c.source(b.SourceID); ok {
		src = s.Name
	}
	if b.ProjectKey != "" {
		return projectName(b.ProjectKey) + " · " + src
	}
	return src
}

func (c *Core) budgetStatus(b Budget, warnPct float64, now time.Time) BudgetStatus {
	start, end := budgetPeriod(b.Period, now)
	st := BudgetStatus{Budget: b, Scope: c.budgetScope(b), PeriodStart: start.Format(dayFmt), PeriodEnd: end.Format(dayFmt)}
	src := b.SourceID
	if src == "" {
		src = "all"
	}
	bl := c.newBilling()
	perDay := map[string]float64{}
	for _, s := range c.scoped(b.ProjectKey, src, "all", Filter{}) {
		for d, ds := range s.Days {
			if d < st.PeriodStart || d >= st.PeriodEnd {
				continue
			}
			v := ds.Usage.CostUSD
			if b.Basis == "paid" {
				v = bl.cost(s.SourceID, d, v)
			}
			perDay[d] += v
			st.Spent += v
		}
	}
	for d := start; d.Before(end) && !d.After(now); d = d.AddDate(0, 0, 1) {
		k := d.Format(dayFmt)
		st.Days = append(st.Days, BudgetDay{k, perDay[k]})
	}
	if b.LimitUSD > 0 {
		st.Pct = st.Spent / b.LimitUSD * 100
	}
	elapsed := now.Sub(start).Hours()
	total := end.Sub(start).Hours()
	if elapsed > 1 {
		st.Projected = st.Spent / elapsed * total
	} else {
		st.Projected = st.Spent
	}
	st.Level = level(st.Pct, warnPct)
	return st
}

func level(pct, warn float64) string {
	switch {
	case pct >= 100:
		return "over"
	case pct >= warn:
		return "warn"
	}
	return "ok"
}

// BudgetStatuses returns every budget with its current spending.
func (c *Core) BudgetStatuses() []BudgetStatus {
	warn := c.AlertSettings().WarnPct
	now := time.Now()
	var out []BudgetStatus
	for _, b := range c.Budgets() {
		out = append(out, c.budgetStatus(b, warn, now))
	}
	return out
}

// ---------- usage limits (5-hour and weekly windows) ----------

// UsageWindow is spending inside one usage window. Times are Unix ms.
type UsageWindow struct {
	Start       int64   `json:"start"`
	End         int64   `json:"end"`
	Spent       float64 `json:"spent"`
	Limit       float64 `json:"limit"`
	Pct         float64 `json:"pct"`
	Projected   float64 `json:"projected"`
	BurnPerHour float64 `json:"burnPerHour"`
	Active      bool    `json:"active"`
	Level       string  `json:"level"`
}

type BlockPoint struct {
	Start int64   `json:"start"`
	Spent float64 `json:"spent"`
}

type SourceLimits struct {
	SourceID     string       `json:"sourceId"`
	Name         string       `json:"name"`
	Agent        model.Agent  `json:"agent"`
	Billing      Billing      `json:"billing"`
	Block        *UsageWindow `json:"block"` // the running 5-hour window, nil when idle
	LastBlock    *UsageWindow `json:"lastBlock"`
	MaxBlock     float64      `json:"maxBlock"`
	AvgBlock     float64      `json:"avgBlock"`
	Blocks       []BlockPoint `json:"blocks"` // the most recent windows, oldest first
	Week         UsageWindow  `json:"week"`
	PrevWeek     float64      `json:"prevWeek"`
	MaxWeek      float64      `json:"maxWeek"`
	RateLimitPct *float64     `json:"rateLimitPct"` // last value the agent itself reported (Codex)
	RateLimitAt  int64        `json:"rateLimitAt"`
}

const blockHours = 5

// blocks groups active hours into 5-hour windows the way the agents do: a window opens on the
// first activity after the previous one closed, at the start of that hour.
func blocks(hourly map[int64]float64) []BlockPoint {
	hours := make([]int64, 0, len(hourly))
	for h, v := range hourly {
		if v > 0 {
			hours = append(hours, h)
		}
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })
	var out []BlockPoint
	for _, h := range hours {
		if len(out) == 0 || h >= out[len(out)-1].Start/1000+blockHours*3600 {
			out = append(out, BlockPoint{Start: h * 1000})
		}
		out[len(out)-1].Spent += hourly[h]
	}
	return out
}

func weekStart(resetDay int, now time.Time) time.Time {
	if resetDay < 1 || resetDay > 7 {
		return now.Add(-7 * 24 * time.Hour).Truncate(time.Hour)
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	want := resetDay % 7 // time.Weekday: Sunday = 0, Monday = 1
	back := (int(day.Weekday()) - want + 7) % 7
	return day.AddDate(0, 0, -back)
}

func (c *Core) sourceHourly(sourceID string) (map[int64]float64, *float64, int64) {
	out := map[int64]float64{}
	var rl *float64
	var rlAt int64
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.idx[sourceID] {
		for h, v := range e.Sum.Hourly {
			out[h] += v
		}
		if e.Sum.RateLimitPct != nil && e.Sum.EndedAt > rlAt {
			v := *e.Sum.RateLimitPct
			rl, rlAt = &v, e.Sum.EndedAt
		}
	}
	return out, rl, rlAt
}

func (c *Core) limitsFor(src Source, warn float64, now time.Time) SourceLimits {
	b := src.Billing
	sl := SourceLimits{SourceID: src.ID, Name: src.Name, Agent: src.Agent, Billing: b}
	hourly, rl, rlAt := c.sourceHourly(src.ID)
	sl.RateLimitPct, sl.RateLimitAt = rl, rlAt
	bs := blocks(hourly)
	nowMs := now.UnixMilli()
	closed := bs
	if n := len(bs); n > 0 && bs[n-1].Start+blockHours*3600*1000 > nowMs {
		cur := bs[n-1]
		closed = bs[:n-1]
		w := &UsageWindow{Start: cur.Start, End: cur.Start + blockHours*3600*1000, Spent: cur.Spent, Limit: b.Limit5hUSD, Active: true}
		elapsed := math.Max(float64(nowMs-cur.Start)/3.6e6, 0.25)
		w.BurnPerHour = cur.Spent / elapsed
		w.Projected = cur.Spent + w.BurnPerHour*float64(w.End-nowMs)/3.6e6
		if w.Limit > 0 {
			w.Pct = w.Spent / w.Limit * 100
		}
		w.Level = level(w.Pct, warn)
		sl.Block = w
	}
	if n := len(closed); n > 0 {
		last := closed[n-1]
		sl.LastBlock = &UsageWindow{Start: last.Start, End: last.Start + blockHours*3600*1000, Spent: last.Spent, Limit: b.Limit5hUSD}
		sum := 0.0
		for _, x := range closed {
			sum += x.Spent
			sl.MaxBlock = math.Max(sl.MaxBlock, x.Spent)
		}
		sl.AvgBlock = sum / float64(n)
	}
	if sl.Block != nil {
		sl.MaxBlock = math.Max(sl.MaxBlock, sl.Block.Spent)
	}
	if len(bs) > 24 {
		bs = bs[len(bs)-24:]
	}
	sl.Blocks = bs

	ws := weekStart(b.WeekResetDay, now)
	we := ws.Add(7 * 24 * time.Hour)
	if b.WeekResetDay >= 1 && b.WeekResetDay <= 7 {
		we = ws.AddDate(0, 0, 7)
	}
	sl.Week = UsageWindow{Start: ws.UnixMilli(), End: we.UnixMilli(), Limit: b.LimitWeekUSD, Active: true}
	prevStart := ws.AddDate(0, 0, -7).Unix()
	weekly := map[int64]float64{} // week index (relative to ws) -> spent
	for h, v := range hourly {
		switch {
		case h >= ws.Unix() && h < we.Unix():
			sl.Week.Spent += v
		case h >= prevStart && h < ws.Unix():
			sl.PrevWeek += v
		}
		if h < ws.Unix() {
			weekly[(ws.Unix()-h-1)/(7*24*3600)] += v
		}
	}
	for _, v := range weekly {
		sl.MaxWeek = math.Max(sl.MaxWeek, v)
	}
	sl.MaxWeek = math.Max(sl.MaxWeek, sl.Week.Spent)
	elapsed := now.Sub(ws).Hours()
	if b.WeekResetDay >= 1 && b.WeekResetDay <= 7 && elapsed > 1 {
		sl.Week.BurnPerHour = sl.Week.Spent / elapsed
		sl.Week.Projected = sl.Week.Spent / elapsed * we.Sub(ws).Hours()
	} else {
		sl.Week.Projected = sl.Week.Spent
		sl.Week.BurnPerHour = sl.Week.Spent / (7 * 24)
	}
	if sl.Week.Limit > 0 {
		sl.Week.Pct = sl.Week.Spent / sl.Week.Limit * 100
	}
	sl.Week.Level = level(sl.Week.Pct, warn)
	return sl
}

// Limits reports the 5-hour and weekly windows of every source.
func (c *Core) Limits() []SourceLimits {
	warn := c.AlertSettings().WarnPct
	now := time.Now()
	var out []SourceLimits
	for _, s := range c.sources() {
		out = append(out, c.limitsFor(s, warn, now))
	}
	return out
}

// ---------- alerts ----------

// Alert is one notice about spending, shown in the app and optionally as an OS notification.
type Alert struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`  // budget | limit | session
	Level      string `json:"level"` // warn | over
	Title      string `json:"title"`
	Body       string `json:"body"`
	At         int64  `json:"at"`
	SourceID   string `json:"sourceId"`
	SessionID  string `json:"sessionId"`
	ProjectKey string `json:"projectKey"`
}

type alertState struct {
	mu       sync.Mutex
	baseline bool
	since    int64              // first check; sessions started later may alert on their first sight
	cost     map[string]float64 // last seen list price per session key
	recent   []Alert
}

// RecentAlerts returns the alerts raised since the app started, newest first.
func (c *Core) RecentAlerts() []Alert {
	c.alerts.mu.Lock()
	defer c.alerts.mu.Unlock()
	out := make([]Alert, len(c.alerts.recent))
	for i, a := range c.alerts.recent {
		out[len(out)-1-i] = a
	}
	return out
}

func (c *Core) raise(a Alert) {
	a.At = time.Now().UnixMilli()
	c.alerts.mu.Lock()
	c.alerts.recent = append(c.alerts.recent, a)
	if len(c.alerts.recent) > 50 {
		c.alerts.recent = c.alerts.recent[len(c.alerts.recent)-50:]
	}
	c.alerts.mu.Unlock()
	c.emit("alert", a)
	if c.notify != nil && c.AlertSettings().Notify {
		_ = c.notify(a.Title, a.Body)
	}
}

// once reports whether an alert key has not fired yet in this period, and marks it.
func (c *Core) once(key, period string) bool {
	c.cfg.mu.Lock()
	defer c.cfg.mu.Unlock()
	if c.cfg.conf.Notified == nil {
		c.cfg.conf.Notified = map[string]string{}
	}
	if c.cfg.conf.Notified[key] == period {
		return false
	}
	c.cfg.conf.Notified[key] = period
	_ = c.cfg.save()
	return true
}

func pctText(p float64) string { return fmt.Sprintf("%.0f%%", p) }

func usd(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

// CheckAlerts compares spending with budgets, limits and the session alert, and raises what is new.
// changed holds the "source/session" keys the last scan re-read.
func (c *Core) CheckAlerts(changed []string) {
	set := c.AlertSettings()
	now := time.Now()
	for _, st := range c.BudgetStatuses() {
		b := st.Budget
		if !b.Notify || st.Level == "ok" {
			continue
		}
		if !c.once("budget:"+b.ID+":"+st.Level, st.PeriodStart) {
			continue
		}
		title := fmt.Sprintf("%s budget at %s", map[string]string{"day": "Daily", "week": "Weekly", "month": "Monthly"}[periodWord(b.Period)], pctText(st.Pct))
		if b.Name != "" {
			title = fmt.Sprintf("%s: %s of the budget used", b.Name, pctText(st.Pct))
		}
		body := fmt.Sprintf("%s of %s this %s (%s). At this pace: %s.", usd(st.Spent), usd(b.LimitUSD), periodWord(b.Period), st.Scope, usd(st.Projected))
		c.raise(Alert{ID: "budget:" + b.ID, Kind: "budget", Level: st.Level, Title: title, Body: body, SourceID: b.SourceID, ProjectKey: b.ProjectKey})
	}
	for _, sl := range c.Limits() {
		if w := sl.Block; w != nil && w.Limit > 0 && w.Level != "ok" {
			if c.once("block:"+sl.SourceID+":"+w.Level, fmt.Sprint(w.Start)) {
				c.raise(Alert{ID: "block:" + sl.SourceID, Kind: "limit", Level: w.Level, SourceID: sl.SourceID,
					Title: fmt.Sprintf("%s: %s of the 5-hour limit used", sl.Name, pctText(w.Pct)),
					Body:  fmt.Sprintf("%s of %s. The window resets at %s.", usd(w.Spent), usd(w.Limit), time.UnixMilli(w.End).Format("15:04"))})
			}
		}
		if w := sl.Week; w.Limit > 0 && w.Level != "ok" {
			if c.once("week:"+sl.SourceID+":"+w.Level, fmt.Sprint(w.Start)) {
				c.raise(Alert{ID: "week:" + sl.SourceID, Kind: "limit", Level: w.Level, SourceID: sl.SourceID,
					Title: fmt.Sprintf("%s: %s of the weekly limit used", sl.Name, pctText(w.Pct)),
					Body:  fmt.Sprintf("%s of %s this week.", usd(w.Spent), usd(w.Limit))})
			}
		}
	}

	// Sessions crossing the per-session alert. The first check only records a baseline.
	keys := map[string]bool{}
	for _, k := range changed {
		keys[k] = true
	}
	c.alerts.mu.Lock()
	first := !c.alerts.baseline
	if first {
		c.alerts.baseline = true
		c.alerts.since = now.UnixMilli()
		c.alerts.cost = map[string]float64{}
	}
	c.alerts.mu.Unlock()
	type cross struct {
		sum  model.SessionSummary
		prev float64
	}
	var crossed []cross
	c.mu.RLock()
	c.alerts.mu.Lock()
	for sid, m := range c.idx {
		for id, e := range m {
			k := sid + "/" + id
			cost := e.Sum.Usage.CostUSD
			prev, seen := c.alerts.cost[k]
			c.alerts.cost[k] = cost
			if first || set.SessionUSD <= 0 || !keys[k] || cost < set.SessionUSD {
				continue
			}
			if !seen && e.Sum.StartedAt < c.alerts.since {
				continue // an old session read for the first time
			}
			if prev < set.SessionUSD {
				crossed = append(crossed, cross{e.Sum, prev})
			}
		}
	}
	c.alerts.mu.Unlock()
	c.mu.RUnlock()
	for _, x := range crossed {
		c.raise(Alert{ID: "session:" + x.sum.SourceID + "/" + x.sum.ID, Kind: "session", Level: "warn", SourceID: x.sum.SourceID, SessionID: x.sum.ID, ProjectKey: x.sum.ProjectKey,
			Title: fmt.Sprintf("A session passed %s", usd(set.SessionUSD)),
			Body:  fmt.Sprintf("%q in %s is at %s list price.", oneLine(x.sum.Title, 60), projectName(x.sum.ProjectKey), usd(x.sum.Usage.CostUSD))})
	}
}

func periodWord(p string) string {
	switch p {
	case "day":
		return "day"
	case "week":
		return "week"
	}
	return "month"
}
