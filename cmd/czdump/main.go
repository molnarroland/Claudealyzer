// czdump parses one session file and prints a summary (development aid).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claudealyzer/internal/adapters"
	_ "claudealyzer/internal/adapters/claude"
	_ "claudealyzer/internal/adapters/codex"
	_ "claudealyzer/internal/adapters/gemini"
	"claudealyzer/internal/model"
)

func main() {
	agent, path := model.Agent(os.Args[1]), os.Args[2]
	a, _ := adapters.Get(agent)
	st, _ := os.Stat(path)
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	t0 := time.Now()
	s, err := a.LoadSession(context.Background(), "", adapters.SessionFile{ID: id, Path: path, Size: st.Size()})
	if err != nil {
		panic(err)
	}
	fmt.Printf("parsed in %v\n", time.Since(t0))
	fmt.Printf("title=%q project=%s turns=%d subagents=%d models=%v\n", s.Title, s.ProjectKey, s.TurnCount, s.SubagentCount, s.Models)
	u := s.Usage
	fmt.Printf("calls=%d in=%d cw5=%d cw1=%d cr=%d out=%d cost=$%.2f sub=$%.2f peak=%d unpriced=%d\n", u.Calls, u.Input, u.CacheWrite5m, u.CacheWrite1h, u.CacheRead, u.Output, u.CostUSD, s.SubagentUsage.CostUSD, s.PeakContext, u.UnpricedTokens)
	if s.ReportedCost != nil {
		fmt.Printf("reported=$%.2f diff=%+.1f%%\n", *s.ReportedCost, (u.CostUSD / *s.ReportedCost - 1) * 100)
	}
	for i, t := range s.Turns {
		if i > 4 {
			break
		}
		fmt.Printf("  turn %d %q steps=%d dur=%ds cost=$%.2f peak=%d answer=%q\n", i, adapters.OneLine(t.Prompt, 60), len(t.Steps), t.DurationMs/1000, t.Usage.CostUSD, t.PeakContext, adapters.OneLine(t.Answer, 60))
		for j, st := range t.Steps {
			if j > 5 {
				break
			}
			fmt.Printf("     %s %q %dms tok=%d cat=%s sub=%s\n", st.Kind, adapters.OneLine(st.Title, 70), st.DurationMs, st.ResultTokens, st.Category, st.SubagentID)
		}
	}
}
