package claude

import (
	"context"
	"math"
	"testing"

	"claudealyzer/internal/adapters"
	"claudealyzer/internal/model"
)

func load(t *testing.T) *model.Session {
	t.Helper()
	a := Adapter{}
	files, err := a.ListSessions("testdata")
	if err != nil || len(files) != 1 {
		t.Fatalf("ListSessions = %v, %v", files, err)
	}
	s, err := a.LoadSession(context.Background(), "testdata", files[0])
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTurnsAndSteps(t *testing.T) {
	s := load(t)
	if s.Title != "Fix failing tests" || s.ProjectKey != "/tmp/demo" || s.GitBranch != "main" {
		t.Errorf("meta = %q %q %q", s.Title, s.ProjectKey, s.GitBranch)
	}
	if len(s.Turns) != 1 {
		t.Fatalf("turns = %d, want 1 (meta prompt hidden)", len(s.Turns))
	}
	tr := s.Turns[0]
	if tr.Prompt != "Run the tests and fix them" || tr.Answer != "Fixed: the test passes now." {
		t.Errorf("prompt/answer = %q / %q", tr.Prompt, tr.Answer)
	}
	if len(tr.Steps) != 3 {
		t.Fatalf("steps = %d, want thinking + bash + agent", len(tr.Steps))
	}
	bash := tr.Steps[1]
	if bash.Kind != model.StepShell || bash.Category != "test" || bash.DurationMs != 150000 || !bash.IsError {
		t.Errorf("bash step = %+v", bash)
	}
	if tr.Steps[2].SubagentID != "abc123" {
		t.Errorf("agent step not linked: %+v", tr.Steps[2])
	}
}

func TestUsageCountedOncePerMessage(t *testing.T) {
	s := load(t)
	own := s.Usage
	own.CostUSD -= s.SubagentUsage.CostUSD
	// msg_1 appears on two lines; its final line (50 output) wins.
	if got := s.Usage.Output - s.SubagentUsage.Output; got != 50+40+20 {
		t.Errorf("own output = %d, want 110", got)
	}
	if s.Usage.Calls != 4 {
		t.Errorf("calls = %d, want 3 main + 1 subagent", s.Usage.Calls)
	}
	if s.Usage.CacheWrite1h != 1000 || s.Usage.CacheWrite5m != 300 {
		t.Errorf("cache writes = %d/%d", s.Usage.CacheWrite1h, s.Usage.CacheWrite5m)
	}
	if s.ReportedCost == nil || *s.ReportedCost != 0.05 {
		t.Errorf("reported = %v", s.ReportedCost)
	}
	if s.PeakContext != 6301 {
		t.Errorf("peak = %d", s.PeakContext)
	}
	// Opus 5.5: in 4, 5m 5, 1h 8, read 0.2, out 20 ($/MTok).
	want := (4*4 + 300*5 + 1000*8 + 17200*0.2 + 110*20) / 1e6
	if math.Abs(own.CostUSD-want) > 1e-9 {
		t.Errorf("own cost = %v, want %v", own.CostUSD, want)
	}
}

func TestSubagent(t *testing.T) {
	s := load(t)
	if len(s.Subagents) != 1 {
		t.Fatalf("subagents = %d", len(s.Subagents))
	}
	a := s.Subagents[0]
	if a.Type != "general-purpose" || a.ParentTurn != 0 || a.Model != "claude-haiku-5-5" || len(a.Turns) != 1 {
		t.Errorf("subagent = %+v", a)
	}
	if s.Turns[0].SubagentUsage.Calls != 1 {
		t.Errorf("subagent usage not rolled into parent turn")
	}
}

func TestQuickTypeSkipsBlobs(t *testing.T) {
	if typ, ok := quickType([]byte(`{"type":"api-request-blob","x":1}`)); !ok || keep[typ] {
		t.Errorf("quickType = %q %v", typ, ok)
	}
	_ = adapters.Cut
}
