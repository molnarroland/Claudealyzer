package insight

import (
	"testing"

	"claudealyzer/internal/model"
)

func TestSlowRepeatedTestsAndGrouping(t *testing.T) {
	step := func(id, title string, ms int64) model.Step {
		return model.Step{ID: id, Kind: model.StepShell, Title: title, Category: "test", DurationMs: ms}
	}
	turns := []model.Turn{
		{Index: 0, DurationMs: 600000, Usage: model.Usage{CostUSD: 2}, PeakContext: 200000,
			Steps: []model.Step{step("a", "npm test", 200000), step("b", "npm test", 200000)}},
		{Index: 1, DurationMs: 100000, Usage: model.Usage{CostUSD: 1}, PeakContext: 180000},
	}
	fs := TurnFindings(turns, DefaultThresholds(), 0, "")
	rules := map[string]int{}
	for _, f := range fs {
		rules[f.Rule]++
	}
	if rules["slow-runs"] != 1 || rules["repeat"] != 1 || rules["context"] != 2 {
		t.Fatalf("rules = %v", rules)
	}
	g := Group(fs)
	n := 0
	for _, f := range g {
		if f.Rule == "context" {
			n++
			if f.Turn != 0 {
				t.Errorf("grouped context finding should point at the worst turn, got %d", f.Turn)
			}
		}
	}
	if n != 1 {
		t.Errorf("context findings after grouping = %d", n)
	}
	if g[0].Severity != "high" {
		t.Errorf("most severe first, got %s", g[0].Severity)
	}
}
