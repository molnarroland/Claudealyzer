// Package pricing prices token usage from a versioned, editable table.
package pricing

import (
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"claudealyzer/internal/model"
)

//go:embed default_pricing.json
var defaultJSON []byte

// Rate is USD per million tokens, valid from From (YYYY-MM-DD) until a later row
// with the same Match takes over.
type Rate struct {
	Match        string  `json:"match"`
	Provider     string  `json:"provider"`
	From         string  `json:"from"`
	Input        float64 `json:"input"`
	CacheWrite5m float64 `json:"cacheWrite5m"`
	CacheWrite1h float64 `json:"cacheWrite1h"`
	CacheRead    float64 `json:"cacheRead"`
	Output       float64 `json:"output"`
	Source       string  `json:"source"`
}

type Table struct {
	Note    string            `json:"note"`
	Models  []Rate            `json:"models"`
	Aliases map[string]string `json:"aliases"`
	Cheaper map[string]string `json:"cheaper"`
}

// Mode decides which rate applies to a historical call.
const (
	ModeOnDay = "onday" // rate valid on the day of the request (default)
	ModeToday = "today" // today's rate for everything
)

var (
	mu      sync.RWMutex
	current Table
	mode    = ModeOnDay
)

func init() {
	if err := json.Unmarshal(defaultJSON, &current); err != nil {
		panic(err)
	}
}

func Default() Table {
	var t Table
	_ = json.Unmarshal(defaultJSON, &t)
	return t
}

func Get() Table {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

func Set(t Table) {
	mu.Lock()
	current = t
	mu.Unlock()
}

func SetMode(m string) {
	mu.Lock()
	if m == ModeToday {
		mode = ModeToday
	} else {
		mode = ModeOnDay
	}
	mu.Unlock()
}

func Mode() string { mu.RLock(); defer mu.RUnlock(); return mode }

// Hash identifies the active pricing so cached summaries can be invalidated.
func Hash() string {
	mu.RLock()
	b, _ := json.Marshal(struct {
		T Table
		M string
	}{current, mode})
	mu.RUnlock()
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:8])
}

// Lookup finds the rate for a model at a time (Unix ms). Longest Match prefix
// wins; among rows with that prefix, the latest From not after the day.
func Lookup(modelID string, at int64) (Rate, bool) {
	mu.RLock()
	defer mu.RUnlock()
	return lookup(current, mode, modelID, at)
}

func lookup(t Table, m string, modelID string, at int64) (Rate, bool) {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if a, ok := t.Aliases[id]; ok {
		id = a
	}
	// Strip provider prefixes and date suffixes ("anthropic.claude-…", "…-20250929").
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	id = strings.TrimPrefix(id, "anthropic.")
	day := time.UnixMilli(at).UTC().Format("2006-01-02")
	if m == ModeToday || at == 0 {
		day = "9999-12-31"
	}
	bestLen := -1
	var cands []Rate
	for _, r := range t.Models {
		p := strings.ToLower(r.Match)
		if !strings.HasPrefix(id, p) {
			continue
		}
		if len(p) > bestLen {
			bestLen = len(p)
			cands = cands[:0]
		}
		if len(p) == bestLen {
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		return Rate{}, false
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].From < cands[j].From })
	chosen := cands[0]
	for _, r := range cands {
		if r.From <= day {
			chosen = r
		}
	}
	return chosen, true
}

// Apply fills the cost fields of u for one call of modelID at time at.
func Apply(modelID string, at int64, u *model.Usage) {
	r, ok := Lookup(modelID, at)
	ApplyRate(r, ok, u)
}

func ApplyRate(r Rate, ok bool, u *model.Usage) {
	u.CostUSD, u.CostInput, u.CostCacheWrite, u.CostCacheRead, u.CostOutput = 0, 0, 0, 0, 0
	u.UnpricedTokens = 0
	if !ok {
		u.UnpricedTokens = u.Total()
		return
	}
	cw5 := r.CacheWrite5m
	if cw5 == 0 {
		cw5 = r.Input
	}
	cw1 := r.CacheWrite1h
	if cw1 == 0 {
		cw1 = cw5
	}
	u.CostInput = float64(u.Input) * r.Input / 1e6
	u.CostCacheWrite = (float64(u.CacheWrite5m)*cw5 + float64(u.CacheWrite1h)*cw1) / 1e6
	u.CostCacheRead = float64(u.CacheRead) * r.CacheRead / 1e6
	u.CostOutput = float64(u.Output) * r.Output / 1e6
	u.CostUSD = u.CostInput + u.CostCacheWrite + u.CostCacheRead + u.CostOutput
}

// Reprice computes the list price of calls with a custom rate function.
func Reprice(calls []model.Call, rate func(c model.Call) (Rate, bool)) model.Usage {
	var total model.Usage
	for _, c := range calls {
		u := c.Usage
		r, ok := rate(c)
		ApplyRate(r, ok, &u)
		total.Add(u)
	}
	return total
}
