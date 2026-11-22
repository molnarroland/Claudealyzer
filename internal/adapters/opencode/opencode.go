// Package opencode reads OpenCode history. Two layouts exist:
//
//   - opencode.db (SQLite): session/message/part tables, or session_v2/session_message/session_part
//     in newer versions. Message and part bodies are JSON in a "data" column.
//   - storage/ (older versions): storage/session/<project>/<id>.json, storage/message/<session>/<id>.json
//     and storage/part/<message>/<id>.json.
//
// The database is opened read-only. Child sessions (started by the task tool) become subagents.
package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"claudealyzer/internal/adapters"
	"claudealyzer/internal/model"
)

type Adapter struct{}

func init() { adapters.Register(Adapter{}) }

func (Adapter) Agent() model.Agent { return model.OpenCode }

func dbPath(home string) string { return filepath.Join(home, "opencode.db") }

func storageDir(home string) string { return filepath.Join(home, "storage") }

func (Adapter) Detect(home string) adapters.Detection {
	n := 0
	if st, err := os.Stat(dbPath(home)); err == nil && !st.IsDir() {
		if db, err := openDB(dbPath(home)); err == nil {
			ss, _ := db.sessions()
			db.close()
			n += len(ss)
		}
	}
	ls, _ := legacySessions(home)
	n += len(ls)
	if n == 0 {
		if _, err := os.Stat(dbPath(home)); err == nil {
			return adapters.Detection{Message: "opencode.db has no sessions yet"}
		}
		return adapters.Detection{Message: "No opencode.db or storage/session found"}
	}
	return adapters.Detection{OK: true, Message: fmt.Sprintf("Found %d OpenCode sessions", n)}
}

// ---------- shared shapes ----------

// session is one OpenCode session, from either layout.
type session struct {
	ID       string
	ParentID string
	Dir      string
	Title    string
	Version  string
	Created  int64
	Updated  int64
	Messages int
}

type tokens struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

func (t *tokens) empty() bool {
	return t == nil || t.Input+t.Output+t.Reasoning+t.Cache.Read+t.Cache.Write == 0
}

type msgTime struct {
	Created   int64 `json:"created"`
	Completed int64 `json:"completed"`
}

// message is the JSON body of a message (legacy file, or the "data" column).
type message struct {
	ID         string          `json:"id"`
	Role       string          `json:"role"`
	Time       msgTime         `json:"time"`
	ModelID    string          `json:"modelID"`
	ProviderID string          `json:"providerID"`
	Model      json.RawMessage `json:"model"`
	Cost       float64         `json:"cost"`
	Tokens     *tokens         `json:"tokens"`
	Error      json.RawMessage `json:"error"`
	Summary    json.RawMessage `json:"summary"`
	Path       struct {
		Cwd  string `json:"cwd"`
		Root string `json:"root"`
	} `json:"path"`
	parts []part
}

func (m *message) model() string {
	if m.ModelID != "" {
		return m.ModelID
	}
	if len(m.Model) == 0 {
		return ""
	}
	if m.Model[0] == '"' {
		var s string
		_ = json.Unmarshal(m.Model, &s)
		return s
	}
	var o struct {
		ModelID string `json:"modelID"`
		ID      string `json:"id"`
	}
	_ = json.Unmarshal(m.Model, &o)
	if o.ModelID != "" {
		return o.ModelID
	}
	return o.ID
}

type toolState struct {
	Status   string          `json:"status"`
	Input    json.RawMessage `json:"input"`
	Output   json.RawMessage `json:"output"`
	Title    string          `json:"title"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
	Time     struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	} `json:"time"`
}

// part is the JSON body of a message part.
type part struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Synthetic bool            `json:"synthetic"`
	Ignored   bool            `json:"ignored"`
	Tool      string          `json:"tool"`
	Name      string          `json:"name"`
	CallID    string          `json:"callID"`
	State     *toolState      `json:"state"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Tokens    *tokens         `json:"tokens"`
	Cost      float64         `json:"cost"`
	Time      struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	} `json:"time"`
	created int64
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var o struct {
		Message string `json:"message"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &o) == nil {
		if o.Data.Message != "" {
			return o.Data.Message
		}
		if o.Message != "" {
			return o.Message
		}
	}
	return string(raw)
}

// childSession finds the session a task tool started (state.metadata.sessionId).
func childSession(p part) string {
	if p.State == nil || len(p.State.Metadata) == 0 {
		return ""
	}
	var md struct {
		SessionID  string `json:"sessionId"`
		SessionID2 string `json:"sessionID"`
	}
	_ = json.Unmarshal(p.State.Metadata, &md)
	if md.SessionID != "" {
		return md.SessionID
	}
	return md.SessionID2
}

// ---------- listing ----------

// ListSessions lists top-level sessions; a session's fingerprint includes its children,
// so a running subagent updates its parent.
func (Adapter) ListSessions(home string) ([]adapters.SessionFile, error) {
	var out []adapters.SessionFile
	if _, err := os.Stat(dbPath(home)); err == nil {
		db, err := openDB(dbPath(home))
		if err == nil {
			ss, err := db.sessions()
			db.close()
			if err == nil {
				out = append(out, files(ss, dbPath(home))...)
			}
		}
	}
	ls, _ := legacySessions(home)
	seen := map[string]bool{}
	for _, f := range out {
		seen[f.ID] = true
	}
	for _, f := range files(ls, storageDir(home)) {
		if !seen[f.ID] {
			out = append(out, f)
		}
	}
	return out, nil
}

func files(ss []session, path string) []adapters.SessionFile {
	byID := map[string]*adapters.SessionFile{}
	var order []string
	for _, s := range ss {
		if s.ParentID != "" {
			continue
		}
		byID[s.ID] = &adapters.SessionFile{ID: s.ID, Path: path, Size: int64(s.Messages), MTime: s.Updated}
		order = append(order, s.ID)
	}
	parent := map[string]string{}
	for _, s := range ss {
		parent[s.ID] = s.ParentID
	}
	root := func(id string) string {
		for i := 0; i < 20 && parent[id] != ""; i++ {
			id = parent[id]
		}
		return id
	}
	for _, s := range ss {
		if s.ParentID == "" {
			continue
		}
		f := byID[root(s.ID)]
		if f == nil {
			continue
		}
		f.Size += int64(s.Messages)
		if s.Updated > f.MTime {
			f.MTime = s.Updated
		}
		f.Extra = fmt.Sprintf("%s|%s:%d", f.Extra, s.ID, s.Updated)
	}
	out := make([]adapters.SessionFile, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

// ---------- loading ----------

type store interface {
	sessions() ([]session, error)
	messages(sessionID string) ([]message, error)
	close()
}

func (Adapter) LoadSession(ctx context.Context, home string, f adapters.SessionFile) (*model.Session, error) {
	var st store
	if filepath.Base(f.Path) == "opencode.db" {
		db, err := openDB(f.Path)
		if err != nil {
			return nil, err
		}
		st = db
	} else {
		st = legacy{home: home}
	}
	defer st.close()
	all, err := st.sessions()
	if err != nil {
		return nil, err
	}
	var root *session
	children := map[string][]session{}
	for i := range all {
		if all[i].ID == f.ID {
			root = &all[i]
		}
		if all[i].ParentID != "" {
			children[all[i].ParentID] = append(children[all[i].ParentID], all[i])
		}
	}
	if root == nil {
		return nil, fmt.Errorf("session %s not found", f.ID)
	}
	msgs, err := st.messages(root.ID)
	if err != nil {
		return nil, err
	}
	s := &model.Session{}
	s.ID, s.Agent, s.Path = root.ID, model.OpenCode, f.Path
	s.Title = strings.TrimSpace(root.Title)
	if strings.HasPrefix(s.Title, "New session - ") || strings.HasPrefix(s.Title, "Child session - ") {
		s.Title = "" // OpenCode's placeholder; Finalize uses the first prompt instead
	}
	s.AgentVersion = "OpenCode"
	if root.Version != "" {
		s.AgentVersion += " " + root.Version
	}
	s.ProjectKey = root.Dir
	b := adapters.NewBuilder()
	links := map[string]string{} // child session id -> tool call id
	reported, missing := build(b, msgs, links)
	b.Finish(nil)
	s.Turns, s.Calls, s.FullOutputs = b.Turns, b.Calls, b.Full
	if s.ProjectKey == "" {
		for _, m := range msgs {
			if m.Path.Root != "" {
				s.ProjectKey = m.Path.Root
				break
			}
			if m.Path.Cwd != "" {
				s.ProjectKey = m.Path.Cwd
			}
		}
	}

	// Child sessions, depth-first, become subagents linked to the task call that started them.
	var walk func(parentID string, depth int)
	walk = func(parentID string, depth int) {
		if depth > 5 {
			return
		}
		for _, ch := range children[parentID] {
			if ctx.Err() != nil {
				return
			}
			cm, err := st.messages(ch.ID)
			if err != nil {
				s.Warnings = append(s.Warnings, "subagent "+ch.ID+": "+err.Error())
				continue
			}
			cb := adapters.NewBuilder()
			r, miss := build(cb, cm, links)
			reported += r
			missing += miss
			cb.Finish(nil)
			a := model.Subagent{ID: ch.ID, Type: "task", Description: strings.TrimSpace(ch.Title), ParentTurn: -1,
				Turns: cb.Turns, Calls: cb.Calls, FullOutputs: cb.Full}
			for _, t := range cb.Turns {
				if a.Description == "" || strings.HasPrefix(a.Description, "Child session - ") {
					a.Description = adapters.OneLine(t.Prompt, 80)
				}
				break
			}
			if call, ok := links[ch.ID]; ok {
				if stp := b.Step(call); stp != nil {
					stp.SubagentID = ch.ID
					a.ParentStepID = stp.ID
					a.ParentTurn = b.TurnOf(call)
					if desc := strings.TrimPrefix(stp.Title, "Agent  "); desc != "" && desc != stp.Title {
						a.Description = desc
					}
				}
			}
			s.Subagents = append(s.Subagents, a)
			walk(ch.ID, depth+1)
		}
	}
	walk(root.ID, 0)
	if reported > 0 {
		v := reported
		s.ReportedCost = &v
	}
	if missing > 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("%d responses without token data", missing))
	}
	adapters.Finalize(s)
	return s, nil
}

var fileKeys = map[string]string{"filePath": "file_path", "filepath": "file_path"}

// build turns OpenCode messages into turns, steps and calls. It returns OpenCode's own cost total
// and the number of assistant messages without usage.
func build(b *adapters.Builder, msgs []message, links map[string]string) (float64, int) {
	reported := 0.0
	missing := 0
	for _, m := range msgs {
		ts := m.Time.Created
		switch m.Role {
		case "user":
			var txt []string
			for _, p := range m.parts {
				if p.Type == "text" && !p.Synthetic && !p.Ignored && strings.TrimSpace(p.Text) != "" {
					txt = append(txt, strings.TrimSpace(p.Text))
				}
			}
			if len(txt) == 0 {
				// Only synthetic parts (for example a compaction request): keep it in the current turn.
				for _, p := range m.parts {
					if p.Type == "compaction" {
						b.MarkCompacted()
					}
				}
				continue
			}
			b.StartTurn(strings.Join(txt, "\n\n"), ts)
		case "assistant":
			if isSummary(m.Summary) {
				b.MarkCompacted()
			}
			reported += m.Cost
			steps := 0
			for _, p := range m.parts {
				pts := p.created
				if p.Time.Start > 0 {
					pts = p.Time.Start
				}
				if pts == 0 {
					pts = ts
				}
				switch p.Type {
				case "reasoning":
					if t := strings.TrimSpace(p.Text); t != "" {
						b.AddStep(model.Step{Kind: model.StepThinking, Title: adapters.OneLine(t, 200), Input: t, StartedAt: pts}, "")
					}
				case "text":
					if t := strings.TrimSpace(p.Text); t != "" && !p.Synthetic {
						b.AddStep(model.Step{Kind: model.StepText, Title: adapters.OneLine(t, 200), Input: t, StartedAt: pts}, "")
					}
				case "tool":
					addTool(b, p, pts, links)
				case "compaction":
					b.MarkCompacted()
				case "step-finish":
					if !p.Tokens.empty() {
						end := p.Time.End
						if end == 0 {
							end = pts
						}
						b.AddCall(m.model(), end, usage(p.Tokens))
						steps++
					}
				}
			}
			if steps == 0 {
				if m.Tokens.empty() {
					if m.Time.Completed > 0 && len(rawText(m.Error)) == 0 {
						missing++
					}
					continue
				}
				end := m.Time.Completed
				if end == 0 {
					end = ts
				}
				b.AddCall(m.model(), end, usage(m.Tokens))
			}
			if errText := rawText(m.Error); errText != "" {
				b.AddStep(model.Step{Kind: model.StepSystem, Title: adapters.OneLine(errText, 200), Input: errText, StartedAt: ts, IsError: true}, "")
			}
		}
	}
	return reported, missing
}

func isSummary(raw json.RawMessage) bool {
	return string(raw) == "true"
}

// usage maps OpenCode tokens onto the shared shape: input excludes cache; output includes reasoning.
func usage(t *tokens) model.Usage {
	return model.Usage{Input: t.Input, CacheRead: t.Cache.Read, CacheWrite5m: t.Cache.Write, Output: t.Output + t.Reasoning, Reasoning: t.Reasoning}
}

func addTool(b *adapters.Builder, p part, ts int64, links map[string]string) {
	name := p.Tool
	if name == "" {
		name = p.Name
	}
	st := model.Step{Tool: name, Kind: adapters.NormTool(name), StartedAt: ts}
	if name == "list" {
		st.Kind = model.StepSearch
	}
	raw := p.Input
	var state toolState
	if p.State != nil {
		state = *p.State
		if len(state.Input) > 0 {
			raw = state.Input
		}
	}
	in := map[string]any{}
	_ = json.Unmarshal(raw, &in)
	for k, v := range fileKeys {
		if x, ok := in[k]; ok {
			in[v] = x
		}
	}
	adapters.Describe(&st, in)
	if st.Kind == model.StepTool && state.Title != "" {
		st.Title = name + "  " + adapters.OneLine(state.Title, 200)
	}
	st.Input, _ = adapters.Cut(adapters.PrettyJSON(raw), adapters.MaxInput)
	if st.Kind == model.StepShell {
		if c, ok := in["command"].(string); ok {
			st.Input = c
		}
	}
	if state.Time.Start > 0 {
		st.StartedAt = state.Time.Start
	}
	id := p.CallID
	if id == "" {
		id = p.ID
	}
	if id == "" {
		id = fmt.Sprintf("oc-%d-%s", ts, name)
	}
	b.AddStep(st, id)
	out := rawText(state.Output)
	if out == "" {
		out = rawText(p.Output)
	}
	isErr := state.Status == "error"
	if e := rawText(state.Error); e != "" {
		isErr = true
		if out == "" {
			out = e
		}
	}
	b.Result(id, out, isErr, state.Time.End)
	if child := childSession(p); child != "" {
		links[child] = id
	}
}

// ---------- legacy storage ----------

type legacy struct{ home string }

func (legacy) close() {}

func legacySessions(home string) ([]session, error) { return legacy{home}.sessions() }

func (l legacy) sessions() ([]session, error) {
	dir := filepath.Join(storageDir(l.home), "session")
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "*.json"))
	var out []session
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var raw struct {
			ID        string `json:"id"`
			ParentID  string `json:"parentID"`
			Directory string `json:"directory"`
			Title     string `json:"title"`
			Version   string `json:"version"`
			Time      struct {
				Created int64 `json:"created"`
				Updated int64 `json:"updated"`
			} `json:"time"`
		}
		if json.Unmarshal(b, &raw) != nil || raw.ID == "" {
			continue
		}
		s := session{ID: raw.ID, ParentID: raw.ParentID, Dir: raw.Directory, Title: raw.Title, Version: raw.Version, Created: raw.Time.Created, Updated: raw.Time.Updated}
		// Message files change as the session runs: fold their count and latest mtime into the fingerprint.
		ms, _ := os.ReadDir(filepath.Join(storageDir(l.home), "message", raw.ID))
		s.Messages = len(ms)
		for _, e := range ms {
			if info, err := e.Info(); err == nil && info.ModTime().UnixMilli() > s.Updated {
				s.Updated = info.ModTime().UnixMilli()
			}
		}
		out = append(out, s)
	}
	return out, nil
}

func (l legacy) messages(sessionID string) ([]message, error) {
	paths, _ := filepath.Glob(filepath.Join(storageDir(l.home), "message", sessionID, "*.json"))
	var out []message
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m message
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.ID == "" {
			m.ID = strings.TrimSuffix(filepath.Base(p), ".json")
		}
		pp, _ := filepath.Glob(filepath.Join(storageDir(l.home), "part", m.ID, "*.json"))
		for _, q := range pp {
			pb, err := os.ReadFile(q)
			if err != nil {
				continue
			}
			var x part
			if json.Unmarshal(pb, &x) == nil {
				if x.ID == "" {
					x.ID = strings.TrimSuffix(filepath.Base(q), ".json")
				}
				m.parts = append(m.parts, x)
			}
		}
		// Part ids sort in creation order (OpenCode ids are time-ordered).
		sort.SliceStable(m.parts, func(i, j int) bool { return m.parts[i].ID < m.parts[j].ID })
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Time.Created != out[j].Time.Created {
			return out[i].Time.Created < out[j].Time.Created
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
