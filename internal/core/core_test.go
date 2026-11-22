package core

import (
	"reflect"
	"testing"
	"time"

	"claudealyzer/internal/model"
)

func TestBlocks(t *testing.T) {
	h := int64(1_800_000_000 / 3600 * 3600) // an hour start
	hourly := map[int64]float64{
		h:           1, // block 1 opens
		h + 3600:    2,
		h + 4*3600:  3, // still block 1 (ends at h+5h)
		h + 5*3600:  4, // block 2 opens
		h + 12*3600: 5, // block 3 opens after a gap
		h + 13*3600: 0, // no spend: ignored
	}
	got := blocks(hourly)
	want := []BlockPoint{{h * 1000, 6}, {(h + 5*3600) * 1000, 4}, {(h + 12*3600) * 1000, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks = %+v, want %+v", got, want)
	}
}

func TestWeekStart(t *testing.T) {
	now := time.Date(2026, 11, 20, 15, 0, 0, 0, time.Local) // a Friday
	if got := weekStart(1, now); got.Format(dayFmt) != "2026-11-16" {
		t.Errorf("Monday reset: %s", got)
	}
	if got := weekStart(5, now); got.Format(dayFmt) != "2026-11-20" {
		t.Errorf("Friday reset: %s", got)
	}
	if got := weekStart(7, now); got.Format(dayFmt) != "2026-11-15" {
		t.Errorf("Sunday reset: %s", got)
	}
	if got := weekStart(0, now); now.Sub(got) != 7*24*time.Hour {
		t.Errorf("rolling: %s", got)
	}
}

func TestFilter(t *testing.T) {
	s := &model.SessionSummary{Models: []string{"claude-opus-4-5-20251101"}, Days: map[string]model.DayStat{"2026-11-10": {}, "2026-11-12": {}}}
	cases := []struct {
		f    Filter
		want bool
	}{
		{Filter{}, true},
		{Filter{Model: "claude-opus"}, true},
		{Filter{Model: "claude-sonnet"}, false},
		{Filter{From: "2026-11-11", To: "2026-11-11"}, false},
		{Filter{From: "2026-11-12"}, true},
		{Filter{To: "2026-11-09"}, false},
	}
	for _, c := range cases {
		if got := c.f.match(s); got != c.want {
			t.Errorf("%+v: got %v", c.f, got)
		}
	}
}

func TestTermsAndSnippet(t *testing.T) {
	if got := terms(`Fix "unit tests"  Go`); !reflect.DeepEqual(got, []string{"fix", "unit tests", "go"}) {
		t.Errorf("terms = %q", got)
	}
	text := "first line\n\nthe migration failed because of a lock"
	i := len("first line\n\nthe ")
	sn, ms, me := snippet(text, i, i+len("migration"))
	if sn[ms:me] != "migration" {
		t.Errorf("snippet %q [%d:%d] = %q", sn, ms, me, sn[ms:me])
	}
}

func TestWeekOf(t *testing.T) {
	if got := weekOf("2026-11-22"); got != "2026-11-16" { // Sunday -> Monday before
		t.Errorf("weekOf = %s", got)
	}
}
