// Package codex reads Codex CLI / Desktop history:
// <home>/sessions/YYYY/MM/DD/rollout-*.jsonl, titles from session_index.jsonl.
package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"claudealyzer/internal/adapters"
	"claudealyzer/internal/model"
)

type Adapter struct{}

func init() { adapters.Register(Adapter{}) }

func (Adapter) Agent() model.Agent { return model.Codex }

func list(home string) []string {
	var out []string
	_ = filepath.WalkDir(filepath.Join(home, "sessions"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func (Adapter) Detect(home string) adapters.Detection {
	n := len(list(home))
	if n == 0 {
		return adapters.Detection{Message: "No sessions/**/rollout-*.jsonl found"}
	}
	return adapters.Detection{OK: true, Message: fmt.Sprintf("Found %d Codex sessions", n)}
}

var reID = regexp.MustCompile(`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)

func (Adapter) ListSessions(home string) ([]adapters.SessionFile, error) {
	var out []adapters.SessionFile
	for _, p := range list(home) {
		st, err := os.Stat(p)
		if err != nil || st.Size() == 0 {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if m := reID.FindStringSubmatch(p); m != nil {
			id = m[1]
		}
		out = append(out, adapters.SessionFile{ID: id, Path: p, Size: st.Size(), MTime: st.ModTime().UnixMilli()})
	}
	return out, nil
}

type line struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type payload struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	CallID    string          `json:"call_id"`
	Output    json.RawMessage `json:"output"`
	Summary   []struct {
		Text string `json:"text"`
	} `json:"summary"`
	// session_meta / turn_context
	ID         string `json:"id"`
	Cwd        string `json:"cwd"`
	Model      string `json:"model"`
	CliVersion string `json:"cli_version"`
	Originator string `json:"originator"`
	// token_usage_record
	ResponseID string `json:"response_id"`
	Usage      *usage `json:"usage"`
	// event_msg
	LastAgentMessage string `json:"last_agent_message"`
	Message          string `json:"message"`
	RateLimits       *struct {
		Primary *struct {
			UsedPercent float64 `json:"used_percent"`
		} `json:"primary"`
	} `json:"rate_limits"`
}

type usage struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
	Reasoning  int64 `json:"reasoning_output_tokens"`
}

var (
	reCmd  = regexp.MustCompile(`cmd\s*:\s*"((?:[^"\\]|\\.)*)"`)
	reWall = regexp.MustCompile(`Wall time:? ([\d.]+) seconds`)
	reExit = regexp.MustCompile(`(?:Process exited with code|exit code:?) (\d+)`)
)

func textOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		var obj struct {
			Output string `json:"output"`
		}
		if json.Unmarshal(raw, &obj) == nil && obj.Output != "" {
			return obj.Output
		}
		return string(raw)
	}
	var out []string
	for _, p := range parts {
		if p.Text != "" {
			out = append(out, p.Text)
		} else if strings.Contains(p.Type, "image") {
			out = append(out, "[image]")
		}
	}
	return strings.Join(out, "\n")
}

func (Adapter) LoadSession(ctx context.Context, home string, f adapters.SessionFile) (*model.Session, error) {
	fh, err := os.Open(f.Path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	b := adapters.NewBuilder()
	s := &model.Session{}
	s.ID, s.Agent, s.Path = f.ID, model.Codex, f.Path
	curModel := ""
	seen := map[string]bool{}
	needPrompt := false
	turnEnd := map[int]int64{}
	bad := 0

	err = adapters.Lines(fh, func(raw []byte) {
		var l line
		if json.Unmarshal(raw, &l) != nil {
			bad++
			return
		}
		ts := adapters.ParseTime(l.Timestamp)
		if b.HasTurn() && ts > turnEnd[len(b.Turns)-1] {
			turnEnd[len(b.Turns)-1] = ts
		}
		var p payload
		_ = json.Unmarshal(l.Payload, &p)
		switch l.Type {
		case "session_meta":
			if p.Cwd != "" {
				s.ProjectKey = p.Cwd
			}
			v := p.CliVersion
			if v == "" {
				v = p.Originator
			}
			s.AgentVersion = strings.TrimSpace("Codex " + v)
		case "turn_context":
			if p.Model != "" {
				curModel = p.Model
			}
			if s.ProjectKey == "" && p.Cwd != "" {
				s.ProjectKey = p.Cwd
			}
		case "token_usage_record":
			if p.Usage == nil || seen[p.ResponseID] {
				return
			}
			seen[p.ResponseID] = true
			u := p.Usage
			// OpenAI convention: cached (and cache-write) tokens are part of input.
			in := u.Input - u.Cached - u.CacheWrite
			if in < 0 {
				in = 0
			}
			b.AddCall(curModel, ts, model.Usage{Input: in, CacheRead: u.Cached, CacheWrite5m: u.CacheWrite, Output: u.Output, Reasoning: u.Reasoning})
		case "event_msg":
			switch p.Type {
			case "task_started":
				b.StartTurn("", ts)
				needPrompt = true
			case "task_complete":
				if b.HasTurn() && p.LastAgentMessage != "" {
					t := &b.Turns[len(b.Turns)-1]
					dup := false
					for j := len(t.Steps) - 1; j >= 0; j-- {
						if t.Steps[j].Kind == model.StepText {
							dup = strings.TrimSpace(t.Steps[j].Input) == strings.TrimSpace(p.LastAgentMessage)
							break
						}
					}
					if !dup {
						b.AddStep(model.Step{Kind: model.StepText, Title: adapters.OneLine(p.LastAgentMessage, 200), Input: p.LastAgentMessage, StartedAt: ts}, "")
					}
				}
			case "token_count":
				if p.RateLimits != nil && p.RateLimits.Primary != nil {
					v := p.RateLimits.Primary.UsedPercent
					s.RateLimitPct = &v
				}
			case "context_compacted":
				b.MarkCompacted()
			}
		case "compacted":
			b.MarkCompacted()
		case "response_item":
			switch p.Type {
			case "message":
				txt := strings.TrimSpace(textOf(p.Content))
				if txt == "" {
					return
				}
				switch p.Role {
				case "user":
					if strings.HasPrefix(txt, "<") {
						return // injected context
					}
					if needPrompt && b.HasTurn() {
						b.Turns[len(b.Turns)-1].Prompt = txt
						needPrompt = false
					} else if !b.HasTurn() || b.Turns[len(b.Turns)-1].Prompt != txt {
						b.StartTurn(txt, ts)
					}
				case "assistant":
					b.AddStep(model.Step{Kind: model.StepText, Title: adapters.OneLine(txt, 200), Input: txt, StartedAt: ts}, "")
				}
			case "reasoning":
				var parts []string
				for _, x := range p.Summary {
					parts = append(parts, x.Text)
				}
				txt := strings.TrimSpace(strings.Join(parts, "\n"))
				if txt == "" {
					return
				}
				b.AddStep(model.Step{Kind: model.StepThinking, Title: adapters.OneLine(txt, 200), Input: txt, StartedAt: ts}, "")
			case "function_call", "custom_tool_call", "local_shell_call":
				name := p.Name
				arg := p.Arguments
				if arg == "" {
					arg = p.Input
				}
				st := model.Step{Tool: name, StartedAt: ts}
				in := map[string]any{}
				_ = json.Unmarshal([]byte(arg), &in)
				// The desktop app wraps commands in a JS "exec" cell.
				if name == "exec" {
					if m := reCmd.FindStringSubmatch(arg); m != nil {
						cmd, err := strconv.Unquote(`"` + m[1] + `"`)
						if err != nil {
							cmd = m[1]
						}
						in = map[string]any{"command": cmd}
						name = "exec_command"
					}
				}
				if c, ok := in["cmd"]; ok {
					in["command"] = c
				}
				st.Kind = adapters.NormTool(name)
				if name == "apply_patch" {
					st.Kind = model.StepEdit
					if files := patchFiles(arg); len(files) > 0 {
						st.Files = files
						st.Title = fmt.Sprintf("apply_patch  %s", strings.Join(files, ", "))
					}
				}
				if st.Title == "" {
					adapters.Describe(&st, in)
				}
				if st.Kind == model.StepShell {
					st.Input, _ = adapters.Cut(fmt.Sprint(in["command"]), adapters.MaxInput)
				} else {
					st.Input, _ = adapters.Cut(arg, adapters.MaxInput)
				}
				b.AddStep(st, p.CallID)
			case "function_call_output", "custom_tool_call_output", "local_shell_call_output":
				out := textOf(p.Output)
				step := b.Result(p.CallID, out, false, ts)
				if step == nil {
					return
				}
				if m := reWall.FindStringSubmatch(out); m != nil {
					if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
						step.DurationMs = int64(v * 1000)
					}
				}
				if m := reExit.FindStringSubmatch(out); m != nil {
					c, _ := strconv.Atoi(m[1])
					step.ExitCode = &c
					step.IsError = c != 0
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}
	b.Finish(func(i int) int64 { return turnEnd[i] })
	s.Turns, s.Calls, s.FullOutputs = b.Turns, b.Calls, b.Full
	s.Title = indexTitle(home, f.ID)
	if bad > 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("%d unreadable lines skipped", bad))
	}
	adapters.Finalize(s)
	return s, nil
}

var rePatchFile = regexp.MustCompile(`\*\*\* (?:Update|Add|Delete) File: (.+)`)

func patchFiles(s string) []string {
	var out []string
	for _, m := range rePatchFile.FindAllStringSubmatch(s, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func indexTitle(home, id string) string {
	if home == "" {
		return ""
	}
	fh, err := os.Open(filepath.Join(home, "session_index.jsonl"))
	if err != nil {
		return ""
	}
	defer fh.Close()
	title := ""
	_ = adapters.Lines(fh, func(raw []byte) {
		var r struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(raw, &r) == nil && r.ID == id && r.Name != "" {
			title = r.Name
		}
	})
	return title
}
