package gemini

import (
	"context"
	"testing"

	"claudealyzer/internal/model"
)

func TestGeminiSession(t *testing.T) {
	a := Adapter{}
	files, _ := a.ListSessions("testdata")
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	s, err := a.LoadSession(context.Background(), "testdata", files[0])
	if err != nil {
		t.Fatal(err)
	}
	if s.ProjectKey != "/tmp/demo" {
		t.Errorf("project = %q (projects.json not applied)", s.ProjectKey)
	}
	if len(s.Turns) != 1 || s.Turns[0].Answer != "All tests pass." {
		t.Fatalf("turns = %+v", s.Turns)
	}
	steps := s.Turns[0].Steps
	if len(steps) != 2 || steps[1].Kind != model.StepShell || steps[1].Category != "test" {
		t.Errorf("steps = %+v", steps)
	}
	// Message g1 is written twice (update) but counted once; thoughts are billed as output.
	u := s.Usage
	if u.Calls != 2 || u.Input != 800 || u.CacheRead != 1400 || u.Output != 60 || u.Reasoning != 30 {
		t.Errorf("usage = %+v", u)
	}
}
