package pricing

import (
	"testing"
	"time"

	"claudealyzer/internal/model"
)

func TestLookupLongestPrefixAndAliases(t *testing.T) {
	cases := map[string]float64{
		"claude-opus-5-5":           4,
		"claude-opus-5":             5,
		"claude-opus-4-8":           5,
		"anthropic.claude-sonnet-5": 2,
		"sonnet":                    2,
		"gpt-6-luna":                1.25,
	}
	for id, in := range cases {
		r, ok := Lookup(id, 0)
		if !ok || r.Input != in {
			t.Errorf("%s: input = %v (ok %v), want %v", id, r.Input, ok, in)
		}
	}
	if _, ok := Lookup("mystery-model", 0); ok {
		t.Error("unknown model must not be priced")
	}
}

func TestEffectiveDates(t *testing.T) {
	old := Get()
	defer Set(old)
	tbl := old
	tbl.Models = append([]Rate{
		{Match: "claude-opus-5-5", From: "2027-01-01", Input: 2, Output: 10},
	}, old.Models...)
	Set(tbl)
	day := func(s string) int64 { t, _ := time.Parse("2006-01-02", s); return t.UnixMilli() }
	if r, _ := Lookup("claude-opus-5-5", day("2026-11-01")); r.Input != 4 {
		t.Errorf("before change: %v", r.Input)
	}
	if r, _ := Lookup("claude-opus-5-5", day("2027-02-01")); r.Input != 2 {
		t.Errorf("after change: %v", r.Input)
	}
}

func TestApplyUnknownIsUnpriced(t *testing.T) {
	u := model.Usage{Input: 10, Output: 5}
	Apply("mystery", 0, &u)
	if u.CostUSD != 0 || u.UnpricedTokens != 15 {
		t.Errorf("usage = %+v", u)
	}
}
