package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"claudealyzer/internal/model"
)

// One session: a prompt, an assistant message with reasoning, a bash call, a task call that starts
// a child session, two API steps, and the answer. The child session reads one file.

const (
	root  = "ses_root"
	child = "ses_child"
)

type fx struct {
	id, session, role string
	created           int64
	data              map[string]any
	parts             []map[string]any
}

func fixture() (sessions []map[string]any, msgs []fx) {
	sessions = []map[string]any{
		{"id": root, "directory": "/tmp/demo", "title": "New session - 2026-11-20T10:00:00.000Z", "version": "1.2.0", "time": map[string]any{"created": 1000, "updated": 9000}},
		{"id": child, "parentID": root, "directory": "/tmp/demo", "title": "Look at the config (@general subagent)", "version": "1.2.0", "time": map[string]any{"created": 3000, "updated": 5000}},
	}
	tok := func(in, out, reason, read, write int) map[string]any {
		return map[string]any{"input": in, "output": out, "reasoning": reason, "cache": map[string]any{"read": read, "write": write}}
	}
	msgs = []fx{
		{id: "msg_01", session: root, role: "user", created: 1000,
			data:  map[string]any{"role": "user", "time": map[string]any{"created": 1000}},
			parts: []map[string]any{{"id": "prt_01", "type": "text", "text": "Run the tests and check the config"}}},
		{id: "msg_02", session: root, role: "assistant", created: 1100,
			data: map[string]any{"role": "assistant", "time": map[string]any{"created": 1100, "completed": 8000}, "modelID": "claude-sonnet-4-5", "providerID": "anthropic",
				"cost": 0.05, "tokens": tok(10, 40, 5, 2000, 300), "path": map[string]any{"cwd": "/tmp/demo", "root": "/tmp/demo"}},
			parts: []map[string]any{
				{"id": "prt_02", "type": "step-start"},
				{"id": "prt_03", "type": "reasoning", "text": "I should run the tests first.", "time": map[string]any{"start": 1200, "end": 1300}},
				{"id": "prt_04", "type": "tool", "callID": "call_1", "tool": "bash", "state": map[string]any{"status": "completed", "input": map[string]any{"command": "go test ./..."},
					"output": "ok  demo 0.1s", "title": "go test ./...", "time": map[string]any{"start": 1400, "end": 2400}}},
				{"id": "prt_05", "type": "step-finish", "tokens": tok(10, 20, 5, 1000, 300), "cost": 0.02, "time": map[string]any{"end": 2500}},
				{"id": "prt_06", "type": "step-start"},
				{"id": "prt_07", "type": "tool", "callID": "call_2", "tool": "task", "state": map[string]any{"status": "completed",
					"input":  map[string]any{"description": "Look at the config", "prompt": "Read config.yml", "subagent_type": "general"},
					"output": "Config is fine.", "metadata": map[string]any{"sessionId": child}, "time": map[string]any{"start": 2600, "end": 5000}}},
				{"id": "prt_08", "type": "text", "text": "Tests pass and the config is fine."},
				{"id": "prt_09", "type": "step-finish", "tokens": tok(0, 20, 0, 1000, 0), "cost": 0.03, "time": map[string]any{"end": 8000}},
			}},
		{id: "msg_03", session: child, role: "user", created: 3000,
			data:  map[string]any{"role": "user", "time": map[string]any{"created": 3000}},
			parts: []map[string]any{{"id": "prt_10", "type": "text", "text": "Read config.yml"}}},
		{id: "msg_04", session: child, role: "assistant", created: 3100,
			data: map[string]any{"role": "assistant", "time": map[string]any{"created": 3100, "completed": 4900}, "modelID": "claude-haiku-4-5", "providerID": "anthropic",
				"cost": 0.01, "tokens": tok(50, 10, 0, 500, 100)},
			parts: []map[string]any{
				{"id": "prt_11", "type": "tool", "callID": "call_3", "tool": "read", "state": map[string]any{"status": "completed", "input": map[string]any{"filePath": "/tmp/demo/config.yml"},
					"output": "a: 1", "time": map[string]any{"start": 3200, "end": 3300}}},
				{"id": "prt_12", "type": "text", "text": "Config is fine."},
			}},
	}
	return
}

func check(t *testing.T, s *model.Session) {
	t.Helper()
	if s.ProjectKey != "/tmp/demo" {
		t.Errorf("project = %q", s.ProjectKey)
	}
	if s.Title != "Run the tests and check the config" {
		t.Errorf("title = %q (placeholder title should give way to the prompt)", s.Title)
	}
	if len(s.Turns) != 1 || s.Turns[0].Answer != "Tests pass and the config is fine." {
		t.Fatalf("turns = %+v", s.Turns)
	}
	steps := s.Turns[0].Steps
	if len(steps) != 3 || steps[0].Kind != model.StepThinking || steps[1].Kind != model.StepShell || steps[1].Category != "test" || steps[2].Kind != model.StepAgent {
		t.Fatalf("steps = %+v", steps)
	}
	if steps[1].DurationMs != 1000 || steps[1].Output != "ok  demo 0.1s" {
		t.Errorf("bash step = %+v", steps[1])
	}
	if steps[2].SubagentID != child {
		t.Errorf("task step not linked to the child session: %+v", steps[2])
	}
	// Two step-finish calls on the root; one message-level call in the child.
	if s.Usage.Calls != 3 {
		t.Errorf("calls = %d", s.Usage.Calls)
	}
	own := s.Usage
	if own.Input != 60 || own.CacheRead != 2500 || own.CacheWrite5m != 400 || own.Output != 55 || own.Reasoning != 5 {
		t.Errorf("usage = %+v", own)
	}
	if len(s.Subagents) != 1 {
		t.Fatalf("subagents = %+v", s.Subagents)
	}
	a := s.Subagents[0]
	if a.ParentTurn != 0 || a.Description != "Look at the config" || a.Model != "claude-haiku-4-5" || a.Usage.Calls != 1 {
		t.Errorf("subagent = %+v", a)
	}
	if len(a.Turns) != 1 || len(a.Turns[0].Steps) != 1 || a.Turns[0].Steps[0].Files[0] != "/tmp/demo/config.yml" {
		t.Errorf("subagent turns = %+v", a.Turns)
	}
	if s.ReportedCost == nil || *s.ReportedCost < 0.059 || *s.ReportedCost > 0.061 {
		t.Errorf("reported cost = %v", s.ReportedCost)
	}
	if s.EditedFiles == nil || s.Hourly == nil {
		t.Errorf("summary maps missing")
	}
}

func load(t *testing.T, home string) *model.Session {
	t.Helper()
	a := Adapter{}
	if d := a.Detect(home); !d.OK {
		t.Fatalf("detect: %+v", d)
	}
	files, err := a.ListSessions(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].ID != root {
		t.Fatalf("files = %+v (child sessions must not be listed)", files)
	}
	s, err := a.LoadSession(context.Background(), home, files[0])
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyStorage(t *testing.T) {
	home := t.TempDir()
	sessions, msgs := fixture()
	for _, s := range sessions {
		writeJSON(t, filepath.Join(home, "storage", "session", "proj1", s["id"].(string)+".json"), s)
	}
	for _, m := range msgs {
		d := map[string]any{"id": m.id, "sessionID": m.session}
		for k, v := range m.data {
			d[k] = v
		}
		writeJSON(t, filepath.Join(home, "storage", "message", m.session, m.id+".json"), d)
		for _, p := range m.parts {
			writeJSON(t, filepath.Join(home, "storage", "part", m.id, p["id"].(string)+".json"), p)
		}
	}
	check(t, load(t, home))
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func dataOf(v map[string]any, drop ...string) string {
	c := map[string]any{}
	for k, x := range v {
		c[k] = x
	}
	for _, k := range drop {
		delete(c, k)
	}
	b, _ := json.Marshal(c)
	return string(b)
}

func newDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	home := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(home, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return home, db
}

func TestSQLiteV1(t *testing.T) {
	home, db := newDB(t)
	exec(t, db, `CREATE TABLE session (id text PRIMARY KEY, project_id text, parent_id text, slug text, directory text, title text, version text, time_created integer, time_updated integer)`)
	exec(t, db, `CREATE TABLE message (id text PRIMARY KEY, session_id text, time_created integer, time_updated integer, data text)`)
	exec(t, db, `CREATE TABLE part (id text PRIMARY KEY, message_id text, session_id text, time_created integer, time_updated integer, data text)`)
	sessions, msgs := fixture()
	for _, s := range sessions {
		tm := s["time"].(map[string]any)
		parent, _ := s["parentID"].(string)
		exec(t, db, `INSERT INTO session VALUES (?, 'p', NULLIF(?, ''), 'slug', ?, ?, ?, ?, ?)`, s["id"], parent, s["directory"], s["title"], s["version"], tm["created"], tm["updated"])
	}
	for _, m := range msgs {
		exec(t, db, `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, m.id, m.session, m.created, m.created, dataOf(m.data))
		for i, p := range m.parts {
			exec(t, db, `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, p["id"], m.id, m.session, m.created+int64(i), m.created, dataOf(p, "id"))
		}
	}
	check(t, load(t, home))
}

func TestSQLiteV2(t *testing.T) {
	home, db := newDB(t)
	exec(t, db, `CREATE TABLE session_v2 (id text PRIMARY KEY, project_id text NOT NULL, parent_id text, slug text NOT NULL, directory text NOT NULL, title text, version text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL)`)
	exec(t, db, `CREATE TABLE session_message (id text PRIMARY KEY, session_id text NOT NULL, type text NOT NULL, seq integer NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL)`)
	exec(t, db, `CREATE TABLE session_part (message_id text NOT NULL, session_id text NOT NULL, ordinal integer NOT NULL, type text NOT NULL, call_id text, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL)`)
	sessions, msgs := fixture()
	for _, s := range sessions {
		tm := s["time"].(map[string]any)
		parent, _ := s["parentID"].(string)
		exec(t, db, `INSERT INTO session_v2 VALUES (?, 'p', NULLIF(?, ''), 'slug', ?, ?, ?, ?, ?)`, s["id"], parent, s["directory"], s["title"], s["version"], tm["created"], tm["updated"])
	}
	for seq, m := range msgs {
		// The newer layout keeps the role in the type column; leave it out of the body.
		exec(t, db, `INSERT INTO session_message VALUES (?, ?, ?, ?, ?, ?, ?)`, m.id, m.session, m.role, seq, m.created, m.created, dataOf(m.data, "role"))
		for i, p := range m.parts {
			exec(t, db, `INSERT INTO session_part VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.id, m.session, i, p["type"], p["callID"], m.created+int64(i), m.created, dataOf(p, "id", "type"))
		}
	}
	check(t, load(t, home))
}
