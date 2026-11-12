// Package gemini reads Gemini CLI history:
// <home>/tmp/<project>/chats/session-*.json(l); projects.json maps path -> name.
package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claudealyzer/internal/adapters"
	"claudealyzer/internal/model"
)

type Adapter struct{}

func init() { adapters.Register(Adapter{}) }

func (Adapter) Agent() model.Agent { return model.Gemini }

func files(home string) []string {
	a, _ := filepath.Glob(filepath.Join(home, "tmp", "*", "chats", "session-*.json"))
	b, _ := filepath.Glob(filepath.Join(home, "tmp", "*", "chats", "session-*.jsonl"))
	return append(a, b...)
}

func (Adapter) Detect(home string) adapters.Detection {
	n := len(files(home))
	if n == 0 {
		return adapters.Detection{Message: "No tmp/*/chats/session-* files found"}
	}
	return adapters.Detection{OK: true, Message: fmt.Sprintf("Found %d Gemini CLI sessions", n)}
}

func (Adapter) ListSessions(home string) ([]adapters.SessionFile, error) {
	var out []adapters.SessionFile
	for _, p := range files(home) {
		st, err := os.Stat(p)
		if err != nil || st.Size() == 0 {
			continue
		}
		id := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(p), ".jsonl"), ".json")
		out = append(out, adapters.SessionFile{ID: id, Path: p, Size: st.Size(), MTime: st.ModTime().UnixMilli()})
	}
	return out, nil
}

type message struct {
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Content   json.RawMessage `json:"content"`
	Thoughts  []struct {
		Subject     string `json:"subject"`
		Description string `json:"description"`
	} `json:"thoughts"`
	Tokens *struct {
		Input    int64 `json:"input"`
		Output   int64 `json:"output"`
		Cached   int64 `json:"cached"`
		Thoughts int64 `json:"thoughts"`
		Tool     int64 `json:"tool"`
	} `json:"tokens"`
	Model     string `json:"model"`
	ToolCalls []struct {
		ID     string          `json:"id"`
		Name   string          `json:"name"`
		Args   json.RawMessage `json:"args"`
		Status string          `json:"status"`
		Time   string          `json:"timestamp"`
		Result []struct {
			FunctionResponse struct {
				Response struct {
					Output json.RawMessage `json:"output"`
					Error  json.RawMessage `json:"error"`
				} `json:"response"`
			} `json:"functionResponse"`
		} `json:"result"`
	} `json:"toolCalls"`
}

type header struct {
	SessionID   string    `json:"sessionId"`
	ProjectHash string    `json:"projectHash"`
	StartTime   string    `json:"startTime"`
	Messages    []message `json:"messages"`
	Set         *struct {
		Messages []message `json:"messages"`
	} `json:"$set"`
}

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
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var out []string
		for _, p := range parts {
			out = append(out, p.Text)
		}
		return strings.Join(out, "\n")
	}
	return string(raw)
}

// readMessages handles both the .json document and the append-only .jsonl log
// (where the same message id is written again as it is updated).
func readMessages(path string) ([]message, error) {
	var order []string
	byID := map[string]message{}
	put := func(m message) {
		if m.ID == "" {
			m.ID = fmt.Sprintf("anon-%d", len(order))
		}
		if _, ok := byID[m.ID]; !ok {
			order = append(order, m.ID)
		}
		byID[m.ID] = m
	}
	if strings.HasSuffix(path, ".json") {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var h header
		if err := json.Unmarshal(b, &h); err != nil {
			return nil, err
		}
		for _, m := range h.Messages {
			put(m)
		}
	} else {
		fh, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer fh.Close()
		err = adapters.Lines(fh, func(raw []byte) {
			var h header
			if json.Unmarshal(raw, &h) != nil {
				return
			}
			if h.Set != nil && h.Set.Messages != nil {
				for _, m := range h.Set.Messages {
					put(m)
				}
				return
			}
			var m message
			if json.Unmarshal(raw, &m) == nil && m.Type != "" {
				put(m)
			}
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]message, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, nil
}

func projectPath(home, dir string) string {
	b, err := os.ReadFile(filepath.Join(home, "projects.json"))
	if err == nil {
		var pj struct {
			Projects map[string]string `json:"projects"`
		}
		if json.Unmarshal(b, &pj) == nil {
			for path, name := range pj.Projects {
				if name == dir {
					return path
				}
			}
		}
	}
	return "gemini:" + dir
}

func (Adapter) LoadSession(ctx context.Context, home string, f adapters.SessionFile) (*model.Session, error) {
	msgs, err := readMessages(f.Path)
	if err != nil {
		return nil, err
	}
	s := &model.Session{}
	s.ID, s.Agent, s.Path = f.ID, model.Gemini, f.Path
	s.AgentVersion = "Gemini CLI"
	s.ProjectKey = projectPath(home, filepath.Base(filepath.Dir(filepath.Dir(f.Path))))
	b := adapters.NewBuilder()
	missing := 0
	for _, m := range msgs {
		ts := adapters.ParseTime(m.Timestamp)
		switch m.Type {
		case "user":
			txt := strings.TrimSpace(textOf(m.Content))
			if txt == "" || strings.HasPrefix(txt, "<session_context>") {
				continue
			}
			b.StartTurn(txt, ts)
		case "gemini", "model":
			for _, th := range m.Thoughts {
				t := strings.TrimSpace(th.Subject + ": " + th.Description)
				b.AddStep(model.Step{Kind: model.StepThinking, Title: adapters.OneLine(t, 200), Input: th.Description, StartedAt: ts}, "")
			}
			if txt := strings.TrimSpace(textOf(m.Content)); txt != "" {
				b.AddStep(model.Step{Kind: model.StepText, Title: adapters.OneLine(txt, 200), Input: txt, StartedAt: ts}, "")
			}
			for _, tc := range m.ToolCalls {
				st := model.Step{Tool: tc.Name, Kind: adapters.NormTool(tc.Name), StartedAt: ts}
				in := map[string]any{}
				_ = json.Unmarshal(tc.Args, &in)
				adapters.Describe(&st, in)
				st.Input, _ = adapters.Cut(adapters.PrettyJSON(tc.Args), adapters.MaxInput)
				if st.Kind == model.StepShell {
					if c, ok := in["command"].(string); ok {
						st.Input = c
					}
				}
				b.AddStep(st, tc.ID)
				out, isErr := "", tc.Status == "error" || tc.Status == "cancelled"
				if len(tc.Result) > 0 {
					r := tc.Result[0].FunctionResponse.Response
					out = textOf(r.Output)
					if len(r.Error) > 0 && string(r.Error) != "null" {
						out = textOf(r.Error)
						isErr = true
					}
				}
				end := adapters.ParseTime(tc.Time)
				b.Result(tc.ID, out, isErr, end)
			}
			if m.Tokens != nil {
				t := m.Tokens
				in := t.Input - t.Cached
				if in < 0 {
					in = 0
				}
				// Thoughts are billed as output.
				b.AddCall(m.Model, ts, model.Usage{Input: in, CacheRead: t.Cached, Output: t.Output + t.Thoughts, Reasoning: t.Thoughts})
			} else {
				missing++
			}
		case "info", "error", "warning":
			txt := strings.TrimSpace(textOf(m.Content))
			if txt != "" {
				b.AddStep(model.Step{Kind: model.StepSystem, Title: adapters.OneLine(txt, 200), Input: txt, StartedAt: ts, IsError: m.Type == "error"}, "")
			}
		}
	}
	b.Finish(nil)
	s.Turns, s.Calls, s.FullOutputs = b.Turns, b.Calls, b.Full
	if missing > 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("%d responses without token data", missing))
	}
	adapters.Finalize(s)
	return s, nil
}
