package codex

import (
	"context"
	"testing"

	"claudealyzer/internal/model"
)

func TestCodexSession(t *testing.T) {
	a := Adapter{}
	files, _ := a.ListSessions("testdata")
	if len(files) != 1 || files[0].ID != "01a136c1-ff64-7341-b76c-92a814652716" {
		t.Fatalf("files = %+v", files)
	}
	s, err := a.LoadSession(context.Background(), "testdata", files[0])
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "List files" || s.ProjectKey != "/tmp/demo" {
		t.Errorf("meta = %q %q", s.Title, s.ProjectKey)
	}
	if len(s.Turns) != 1 || s.Turns[0].Prompt != "List the files" || s.Turns[0].Answer != "There is one file: a.txt" {
		t.Fatalf("turns = %+v", s.Turns)
	}
	st := s.Turns[0].Steps
	if len(st) != 1 || st[0].Kind != model.StepShell || st[0].Title != "ls -la" || st[0].DurationMs != 400 {
		t.Errorf("steps = %+v", st)
	}
	// Duplicate response ids are counted once; cached tokens are a subset of input.
	u := s.Usage
	if u.Calls != 1 || u.Input != 200 || u.CacheRead != 800 || u.Output != 30 {
		t.Errorf("usage = %+v", u)
	}
	if s.RateLimitPct == nil || *s.RateLimitPct != 12.5 {
		t.Errorf("rate limit = %v", s.RateLimitPct)
	}
}
