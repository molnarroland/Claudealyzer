// Package claude reads Claude Code history: <home>/projects/<dir>/<session>.jsonl
// plus <session>/subagents/agent-<id>.jsonl (+ .meta.json).
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"claudealyzer/internal/adapters"
	"claudealyzer/internal/model"
)

type Adapter struct{}

func init() { adapters.Register(Adapter{}) }

func (Adapter) Agent() model.Agent { return model.Claude }

func (Adapter) Detect(home string) adapters.Detection {
	files, _ := filepath.Glob(filepath.Join(home, "projects", "*", "*.jsonl"))
	if len(files) == 0 {
		return adapters.Detection{Message: "No projects/*/*.jsonl found"}
	}
	return adapters.Detection{OK: true, Message: fmt.Sprintf("Found projects/ with %d sessions", len(files))}
}

func (Adapter) ListSessions(home string) ([]adapters.SessionFile, error) {
	files, err := filepath.Glob(filepath.Join(home, "projects", "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	out := make([]adapters.SessionFile, 0, len(files))
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || st.Size() == 0 {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		extra := ""
		subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(f, ".jsonl"), "subagents", "agent-*.jsonl"))
		if len(subs) > 0 {
			var sz, mt int64
			for _, s := range subs {
				if si, err := os.Stat(s); err == nil {
					sz += si.Size()
					if m := si.ModTime().UnixMilli(); m > mt {
						mt = m
					}
				}
			}
			extra = fmt.Sprintf("%d:%d:%d", len(subs), sz, mt)
		}
		out = append(out, adapters.SessionFile{ID: id, Path: f, Size: st.Size(), MTime: st.ModTime().UnixMilli(), Extra: extra})
	}
	return out, nil
}

// ---------- records ----------

type usage struct {
	InputTokens   int64 `json:"input_tokens"`
	CacheCreation int64 `json:"cache_creation_input_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
	OutputTokens  int64 `json:"output_tokens"`
	OutputDetails struct {
		Thinking int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
	CacheSplit *struct {
		M5 int64 `json:"ephemeral_5m_input_tokens"`
		H1 int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func (u usage) toModel() model.Usage {
	m := model.Usage{Input: u.InputTokens, CacheRead: u.CacheRead, Output: u.OutputTokens, Reasoning: u.OutputDetails.Thinking}
	if u.CacheSplit != nil && u.CacheSplit.M5+u.CacheSplit.H1 > 0 {
		m.CacheWrite5m, m.CacheWrite1h = u.CacheSplit.M5, u.CacheSplit.H1
	} else {
		m.CacheWrite5m = u.CacheCreation
	}
	return m
}

type message struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type record struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Timestamp        string          `json:"timestamp"`
	Cwd              string          `json:"cwd"`
	GitBranch        string          `json:"gitBranch"`
	Version          string          `json:"version"`
	Message          *message        `json:"message"`
	ToolUseResult    json.RawMessage `json:"toolUseResult"`
	AiTitle          string          `json:"aiTitle"`
	TotalCostUSD     *float64        `json:"totalCostUSD"`
	DurationMs       int64           `json:"durationMs"`
	Content          json.RawMessage `json:"content"`
	AgentID          string          `json:"agentId"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

var keep = map[string]bool{"user": true, "assistant": true, "system": true, "ai-title": true, "cost-state": true}

var typePrefix = []byte(`{"type":"`)

// quickType reads the type when it is the first key, to skip big records cheaply.
func quickType(line []byte) (string, bool) {
	if !bytes.HasPrefix(line, typePrefix) {
		return "", false
	}
	rest := line[len(typePrefix):]
	if i := bytes.IndexByte(rest, '"'); i > 0 {
		return string(rest[:i]), true
	}
	return "", false
}

// ---------- parse ----------

type parsed struct {
	b          *adapters.Builder
	cwd        string
	branch     string
	version    string
	title      string
	reported   *float64
	agentLinks map[string]string // tool_use id -> agentId
	warnings   int
}

var (
	reCmdName = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	reCmdArgs = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
)

func parseFile(path string) (*parsed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p := &parsed{b: adapters.NewBuilder(), agentLinks: map[string]string{}}
	b := p.b
	type pending struct {
		turn  int
		model string
		ts    int64
		u     model.Usage
	}
	callOrder := []string{}
	calls := map[string]*pending{}
	turnEnd := map[int]int64{}

	err = adapters.Lines(f, func(line []byte) {
		if t, ok := quickType(line); ok && !keep[t] {
			return
		}
		var r record
		if json.Unmarshal(line, &r) != nil {
			p.warnings++
			return
		}
		ts := adapters.ParseTime(r.Timestamp)
		if r.Cwd != "" && p.cwd == "" {
			p.cwd = r.Cwd
		}
		if r.GitBranch != "" && r.GitBranch != "HEAD" {
			p.branch = r.GitBranch
		}
		if r.Version != "" {
			p.version = r.Version
		}
		if ts > 0 && b.HasTurn() {
			i := len(b.Turns) - 1
			if ts > turnEnd[i] {
				turnEnd[i] = ts
			}
		}
		switch r.Type {
		case "ai-title":
			if r.AiTitle != "" {
				p.title = r.AiTitle
			}
		case "cost-state":
			if r.TotalCostUSD != nil {
				v := *r.TotalCostUSD
				p.reported = &v
			}
		case "system":
			switch r.Subtype {
			case "compact_boundary":
				b.MarkCompacted()
				b.AddStep(model.Step{Kind: model.StepSystem, Title: "Context compacted", StartedAt: ts}, "")
			case "turn_duration":
				if b.HasTurn() && r.DurationMs > 0 {
					b.Turns[len(b.Turns)-1].DurationMs = r.DurationMs
				}
			}
		case "user":
			if r.Message == nil {
				return
			}
			handleUser(p, &r, ts)
		case "assistant":
			if r.Message == nil {
				return
			}
			m := r.Message
			if m.Usage != nil && m.Model != "<synthetic>" {
				key := m.ID
				if key == "" {
					key = fmt.Sprintf("line-%d", len(callOrder))
				}
				if c, ok := calls[key]; ok {
					u := m.Usage.toModel()
					// Later lines of the same response carry the final counts.
					if u.Output >= c.u.Output {
						c.u = u
					}
				} else {
					if !b.HasTurn() {
						b.StartTurn("(session start)", ts)
					}
					calls[key] = &pending{turn: len(b.Turns) - 1, model: m.Model, ts: ts, u: m.Usage.toModel()}
					callOrder = append(callOrder, key)
				}
			}
			var blocks []block
			if json.Unmarshal(m.Content, &blocks) != nil {
				return
			}
			for _, bl := range blocks {
				switch bl.Type {
				case "thinking", "redacted_thinking":
					txt := strings.TrimSpace(bl.Thinking)
					if txt == "" {
						continue // signature-only block: nothing to show
					}
					title := "Thinking"
					if txt != "" {
						title = adapters.OneLine(txt, 200)
					}
					b.AddStep(model.Step{Kind: model.StepThinking, Title: title, Input: cut(txt, adapters.MaxInput), StartedAt: ts}, "")
				case "text":
					if strings.TrimSpace(bl.Text) == "" {
						continue
					}
					b.AddStep(model.Step{Kind: model.StepText, Title: adapters.OneLine(bl.Text, 200), Input: bl.Text, StartedAt: ts}, "")
				case "tool_use", "server_tool_use":
					s := model.Step{Tool: bl.Name, Kind: adapters.NormTool(bl.Name), StartedAt: ts}
					var in map[string]any
					_ = json.Unmarshal(bl.Input, &in)
					adapters.Describe(&s, in)
					s.Input, _ = adapters.Cut(adapters.PrettyJSON(bl.Input), adapters.MaxInput)
					if s.Kind == model.StepShell {
						if c, ok := in["command"].(string); ok {
							s.Input, _ = adapters.Cut(c, adapters.MaxInput)
						}
					}
					b.AddStep(s, bl.ID)
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}
	for _, k := range callOrder {
		c := calls[k]
		b.AddCallAt(c.turn, c.model, c.ts, c.u)
	}
	b.Finish(func(i int) int64 { return turnEnd[i] })
	return p, nil
}

func cut(s string, n int) string { r, _ := adapters.Cut(s, n); return r }

func handleUser(p *parsed, r *record, ts int64) {
	b := p.b
	var text string
	var blocks []block
	if len(r.Message.Content) > 0 && r.Message.Content[0] == '"' {
		_ = json.Unmarshal(r.Message.Content, &text)
	} else if json.Unmarshal(r.Message.Content, &blocks) == nil {
		var parts []string
		for _, bl := range blocks {
			switch bl.Type {
			case "tool_result":
				handleResult(p, r, bl, ts)
			case "text":
				parts = append(parts, bl.Text)
			case "image":
				parts = append(parts, "[image]")
			}
		}
		text = strings.Join(parts, "\n")
	}
	text = strings.TrimSpace(text)
	if text == "" || r.IsMeta {
		return
	}
	switch {
	case r.IsCompactSummary:
		b.MarkCompacted()
		b.AddStep(model.Step{Kind: model.StepSystem, Title: "Conversation compacted; continuing from a summary", Input: cut(text, adapters.MaxInput), StartedAt: ts}, "")
		return
	case strings.HasPrefix(text, "<local-command"), strings.HasPrefix(text, "Caveat:"):
		return
	case strings.HasPrefix(text, "<command-name>"):
		name := firstGroup(reCmdName, text)
		args := strings.TrimSpace(firstGroup(reCmdArgs, text))
		prompt := strings.TrimSpace(name + " " + args)
		b.StartTurn(prompt, ts)
		return
	case strings.HasPrefix(text, "<task-notification"), strings.HasPrefix(text, "<system-reminder"), strings.HasPrefix(text, "[Request interrupted"):
		b.AddStep(model.Step{Kind: model.StepSystem, Title: adapters.OneLine(stripTags(text), 200), Input: cut(text, adapters.MaxInput), StartedAt: ts}, "")
		return
	}
	b.StartTurn(text, ts)
}

func handleResult(p *parsed, r *record, bl block, ts int64) {
	out := contentText(bl.Content)
	var tur map[string]json.RawMessage
	if len(r.ToolUseResult) > 0 && r.ToolUseResult[0] == '{' {
		_ = json.Unmarshal(r.ToolUseResult, &tur)
	}
	if tur != nil {
		var so, se string
		_ = json.Unmarshal(tur["stdout"], &so)
		_ = json.Unmarshal(tur["stderr"], &se)
		if so != "" || se != "" {
			out = strings.TrimRight(so, "\n")
			if se != "" {
				out += "\n[stderr]\n" + se
			}
		}
	}
	s := p.b.Result(bl.ToolUseID, out, bl.IsError, ts)
	if s == nil || tur == nil {
		return
	}
	if raw, ok := tur["agentId"]; ok {
		var id string
		if json.Unmarshal(raw, &id) == nil && id != "" {
			s.SubagentID = id
			p.agentLinks[bl.ToolUseID] = id
		}
	}
	for _, k := range []string{"exitCode", "returnCode", "code"} {
		if raw, ok := tur[k]; ok {
			var c int
			if json.Unmarshal(raw, &c) == nil {
				s.ExitCode = &c
				if c != 0 {
					s.IsError = true
				}
			}
		}
	}
	if raw, ok := tur["interrupted"]; ok && string(raw) == "true" {
		s.IsError = true
	}
	if raw, ok := tur["durationMs"]; ok {
		var d int64
		if json.Unmarshal(raw, &d) == nil && d > 0 {
			s.DurationMs = d
		}
	}
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var parts []block
	if json.Unmarshal(raw, &parts) != nil {
		return string(raw)
	}
	var out []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, p.Text)
		case "image":
			out = append(out, "[image]")
		}
	}
	return strings.Join(out, "\n")
}

func firstGroup(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

var reTag = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string { return reTag.ReplaceAllString(s, " ") }

// ---------- load ----------

type meta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"`
	Model       string `json:"model"`
}

func (Adapter) LoadSession(ctx context.Context, home string, f adapters.SessionFile) (*model.Session, error) {
	p, err := parseFile(f.Path)
	if err != nil {
		return nil, err
	}
	s := &model.Session{}
	s.ID = f.ID
	s.Agent = model.Claude
	s.Path = f.Path
	s.Title = p.title
	s.GitBranch = p.branch
	s.AgentVersion = "Claude Code " + p.version
	s.ReportedCost = p.reported
	s.ProjectKey = p.cwd
	if s.ProjectKey == "" {
		s.ProjectKey = "/" + strings.ReplaceAll(strings.TrimPrefix(filepath.Base(filepath.Dir(f.Path)), "-"), "-", "/")
	}
	s.Turns = p.b.Turns
	s.Calls = p.b.Calls
	s.FullOutputs = p.b.Full
	if p.warnings > 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("%d unreadable lines skipped", p.warnings))
	}

	// Subagents.
	toolOf := map[string]string{}
	for tool, id := range p.agentLinks {
		toolOf[id] = tool
	}
	subDir := filepath.Join(strings.TrimSuffix(f.Path, ".jsonl"), "subagents")
	files, _ := filepath.Glob(filepath.Join(subDir, "agent-*.jsonl"))
	for _, sf := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(sf), "agent-"), ".jsonl")
		sp, err := parseFile(sf)
		if err != nil {
			continue
		}
		var m meta
		if b, err := os.ReadFile(strings.TrimSuffix(sf, ".jsonl") + ".meta.json"); err == nil {
			_ = json.Unmarshal(b, &m)
		}
		a := model.Subagent{ID: id, Type: m.AgentType, Description: m.Description, Model: m.Model, Turns: sp.b.Turns, Calls: sp.b.Calls, ParentTurn: -1, FullOutputs: sp.b.Full}
		if a.Type == "" {
			a.Type = "subagent"
		}
		tool := m.ToolUseID
		if tool == "" {
			tool = toolOf[id]
		}
		if tool != "" {
			a.ParentTurn = p.b.TurnOf(tool)
			if st := p.b.Step(tool); st != nil {
				a.ParentStepID = st.ID
				st.SubagentID = id
				if a.Description == "" {
					a.Description = strings.TrimPrefix(st.Title, "Agent  ")
				}
			}
		}
		if a.Description == "" && len(a.Turns) > 0 {
			a.Description = adapters.OneLine(a.Turns[0].Prompt, 60)
		}
		s.Subagents = append(s.Subagents, a)
	}
	// Steps were edited through pointers before Finish copied them; refresh links.
	for i := range s.Subagents {
		a := s.Subagents[i]
		if a.ParentTurn >= 0 && a.ParentTurn < len(s.Turns) {
			for j := range s.Turns[a.ParentTurn].Steps {
				if s.Turns[a.ParentTurn].Steps[j].ID == a.ParentStepID {
					s.Turns[a.ParentTurn].Steps[j].SubagentID = a.ID
				}
			}
		}
	}
	adapters.Finalize(s)
	return s, nil
}
