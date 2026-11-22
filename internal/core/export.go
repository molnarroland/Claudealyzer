package core

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"claudealyzer/internal/adapters"
	"claudealyzer/internal/model"
)

// ---------- export ----------
//
// Exports are built here as bytes; the Wails service asks where to save them.

func f2(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
func i2(v int64) string   { return strconv.FormatInt(v, 10) }

func isoTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04:05")
}

func writeCSV(header []string, rows [][]string) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(header)
	_ = w.WriteAll(rows)
	w.Flush()
	return buf.Bytes()
}

// SessionsCSV exports the sessions of a scope, one row each (the Overview table).
func (c *Core) SessionsCSV(projectKey, sourceID, agent string, days int, f Filter) []byte {
	o := c.Overview(projectKey, sourceID, agent, days, f)
	header := []string{"session_id", "title", "agent", "source", "project", "project_path", "git_branch", "started", "ended",
		"turns", "subagents", "main_model", "models", "input_tokens", "cache_write_tokens", "cache_read_tokens", "output_tokens",
		"reasoning_tokens", "api_calls", "peak_context", "list_price_usd", "your_cost_usd", "subagent_usd", "findings_high", "findings_medium", "unpriced_tokens"}
	var rows [][]string
	for _, r := range o.Rows {
		u := r.Usage
		rows = append(rows, []string{r.ID, r.Title, string(r.Agent), r.SourceName, projectName(r.ProjectKey), ShortHome(r.ProjectKey), r.GitBranch,
			isoTime(r.StartedAt), isoTime(r.EndedAt), strconv.Itoa(r.TurnCount), strconv.Itoa(r.SubagentCount), r.MainModel, strings.Join(r.Models, " "),
			i2(u.Input), i2(u.CacheWrite()), i2(u.CacheRead), i2(u.Output), i2(u.Reasoning), strconv.Itoa(u.Calls), i2(r.PeakContext),
			f2(u.CostUSD), f2(r.EffectiveUSD), f2(r.SubagentUsage.CostUSD), strconv.Itoa(r.FindingsHigh), strconv.Itoa(r.FindingsMed), i2(u.UnpricedTokens)})
	}
	return writeCSV(header, rows)
}

// CalendarCSV exports one row per day. month is YYYY-MM, or "" for every day with activity.
func (c *Core) CalendarCSV(projectKey, sourceID, agent, month string, f Filter) []byte {
	header := []string{"date", "sessions", "prompts", "api_calls", "input_tokens", "cache_write_tokens", "cache_read_tokens", "output_tokens",
		"total_tokens", "list_price_usd", "your_cost_usd", "claude_usd", "codex_usd", "gemini_usd", "opencode_usd"}
	var days []CalendarDay
	if month != "" {
		days = c.Calendar(projectKey, sourceID, agent, month, f).Days
	} else {
		first := c.Calendar(projectKey, sourceID, agent, "", f)
		if first.First != "" {
			start, _ := time.ParseInLocation("2006-01", first.First, time.Local)
			for m := start; m.Format("2006-01") <= first.Last; m = m.AddDate(0, 1, 0) {
				for _, d := range c.Calendar(projectKey, sourceID, agent, m.Format("2006-01"), f).Days {
					if d.Sessions > 0 {
						days = append(days, d)
					}
				}
			}
		}
	}
	var rows [][]string
	for _, d := range days {
		u := d.Usage
		rows = append(rows, []string{d.Date, strconv.Itoa(d.Sessions), strconv.Itoa(d.Prompts), strconv.Itoa(u.Calls), i2(u.Input), i2(u.CacheWrite()),
			i2(u.CacheRead), i2(u.Output), i2(u.Total()), f2(u.CostUSD), f2(d.Effective),
			f2(d.ByAgent["claude"]), f2(d.ByAgent["codex"]), f2(d.ByAgent["gemini"]), f2(d.ByAgent["opencode"])})
	}
	return writeCSV(header, rows)
}

// fullSession loads a session with every subagent transcript and untruncated tool outputs.
func (c *Core) fullSession(sourceID, id string) (*model.Session, error) {
	s, _, err := c.Load(sourceID, id)
	return s, err
}

// SessionJSON exports the parsed session (turns, steps, subagents, usage) as JSON.
func (c *Core) SessionJSON(sourceID, id string) ([]byte, error) {
	s, err := c.fullSession(sourceID, id)
	if err != nil {
		return nil, err
	}
	src, _ := c.source(sourceID)
	out := struct {
		ExportedBy string         `json:"exportedBy"`
		ExportedAt string         `json:"exportedAt"`
		Source     string         `json:"source"`
		Project    string         `json:"project"`
		Effective  Effective      `json:"effective"`
		Session    *model.Session `json:"session"`
		Calls      []model.Call   `json:"calls"`
	}{"Claudealyzer", time.Now().Format(time.RFC3339), src.Name, ShortHome(s.ProjectKey), c.effective(sourceID, s.Usage.CostUSD, s.StartedAt), s, s.Calls}
	return json.MarshalIndent(out, "", "  ")
}

func mdEscapeInline(s string) string {
	return strings.NewReplacer("`", "'", "\n", " ").Replace(s)
}

func fmtDur(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func fmtTok(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return strconv.FormatInt(n, 10)
}

func writeTurnsMD(b *strings.Builder, turns []model.Turn, level string, withSteps bool) {
	for i, t := range turns {
		fmt.Fprintf(b, "%s Turn %d", level, i+1)
		if t.StartedAt > 0 {
			fmt.Fprintf(b, " · %s", isoTime(t.StartedAt))
		}
		b.WriteString("\n\n")
		meta := []string{}
		if d := fmtDur(t.DurationMs); d != "" {
			meta = append(meta, "worked "+d)
		}
		u := t.Usage
		u.Add(t.SubagentUsage)
		meta = append(meta, fmt.Sprintf("$%.2f list price", u.CostUSD), "peak context "+fmtTok(t.PeakContext))
		if t.Compacted {
			meta = append(meta, "compacted")
		}
		fmt.Fprintf(b, "*%s*\n\n", strings.Join(meta, " · "))
		if p := strings.TrimSpace(t.Prompt); p != "" && p != "(session start)" {
			b.WriteString("**Prompt**\n\n")
			for _, line := range strings.Split(p, "\n") {
				b.WriteString("> " + line + "\n")
			}
			b.WriteString("\n")
		}
		if withSteps && len(t.Steps) > 0 {
			fmt.Fprintf(b, "<details><summary>%d steps</summary>\n\n", len(t.Steps))
			for _, st := range t.Steps {
				line := fmt.Sprintf("- `%s` %s", st.Kind, mdEscapeInline(adapters.OneLine(st.Title, 200)))
				if d := fmtDur(st.DurationMs); d != "" {
					line += " (" + d + ")"
				}
				if st.IsError {
					line += " **error**"
				}
				b.WriteString(line + "\n")
			}
			b.WriteString("\n</details>\n\n")
		}
		if a := strings.TrimSpace(t.Answer); a != "" {
			b.WriteString("**Answer**\n\n" + a + "\n\n")
		}
	}
}

// SessionMarkdown exports a readable transcript: prompts, answers and (optionally) the steps in between.
func (c *Core) SessionMarkdown(sourceID, id string, withSteps bool) ([]byte, error) {
	s, err := c.fullSession(sourceID, id)
	if err != nil {
		return nil, err
	}
	src, _ := c.source(sourceID)
	eff := c.effective(sourceID, s.Usage.CostUSD, s.StartedAt)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", strings.TrimSpace(s.Title))
	rows := [][2]string{
		{"Project", "`" + ShortHome(s.ProjectKey) + "`"},
		{"Agent", fmt.Sprintf("%s (%s)", s.Agent, src.Name)},
		{"Session ID", "`" + s.ID + "`"},
		{"Started", isoTime(s.StartedAt)},
		{"Ended", isoTime(s.EndedAt)},
		{"Model", s.MainModel},
		{"Turns", strconv.Itoa(s.TurnCount)},
		{"Tokens", fmt.Sprintf("%s total · %s cache read · %s output", fmtTok(s.Usage.Total()), fmtTok(s.Usage.CacheRead), fmtTok(s.Usage.Output))},
		{"List price", fmt.Sprintf("$%.2f", s.Usage.CostUSD)},
		{"Your cost", fmt.Sprintf("$%.2f (%s)", eff.Effective, eff.Label)},
	}
	if s.GitBranch != "" {
		rows = append(rows[:2], append([][2]string{{"Branch", "`" + s.GitBranch + "`"}}, rows[2:]...)...)
	}
	if s.SubagentCount > 0 {
		rows = append(rows, [2]string{"Subagents", fmt.Sprintf("%d · $%.2f", s.SubagentCount, s.SubagentUsage.CostUSD)})
	}
	b.WriteString("| | |\n|---|---|\n")
	for _, r := range rows {
		if r[1] != "" {
			fmt.Fprintf(&b, "| %s | %s |\n", r[0], strings.ReplaceAll(r[1], "|", "\\|"))
		}
	}
	b.WriteString("\n")
	writeTurnsMD(&b, s.Turns, "##", withSteps)
	if len(s.Subagents) > 0 {
		b.WriteString("## Subagents\n\n")
		for _, a := range s.Subagents {
			fmt.Fprintf(&b, "### %s\n\n*%s · %s · started in turn %d · $%.2f*\n\n", strings.TrimSpace(firstNonEmpty(a.Description, a.Type, a.ID)), a.Type, a.Model, a.ParentTurn+1, a.Usage.CostUSD)
			if withSteps {
				writeTurnsMD(&b, a.Turns, "####", true)
			}
		}
	}
	fmt.Fprintf(&b, "---\n\n*Exported by Claudealyzer on %s. Costs are estimates from public list prices.*\n", time.Now().Format("2006-01-02"))
	return []byte(b.String()), nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// ExportName suggests a file name (without extension) for an export.
func (c *Core) ExportName(kind, sourceID, id string) string {
	base := "claudealyzer-" + kind
	if id != "" {
		if s, ok := c.summary(sourceID, id); ok {
			if t := slug(oneLine(s.Title, 50)); t != "" {
				base = t
			}
		}
	}
	return base + "-" + time.Now().Format("2006-01-02")
}
