package core

import (
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"claudealyzer/internal/model"
)

// ---------- full-text search ----------
//
// Search reads transcripts on demand and keeps a compact text copy of each one
// (prompts, answers, commands, file paths and tool titles) in memory, so only the
// first search, and sessions that changed since, pay for parsing.

const (
	maxSearchField   = 8000 // characters kept per prompt or answer
	maxHitsPerSearch = 300
	maxHitsPerGroup  = 6
	snippetRadius    = 70
)

type docField struct {
	kind  string // prompt | answer | command | file | tool
	text  string
	lower string
	turn  int
	at    int64
	step  string
}

type searchDoc struct {
	size, mtime int64
	fields      []docField
}

type searchIndex struct {
	mu   sync.Mutex
	docs map[string]*searchDoc
}

// SearchHit is one matching prompt, answer or step.
type SearchHit struct {
	Kind    string `json:"kind"`
	Turn    int    `json:"turn"`
	StepID  string `json:"stepId"`
	At      int64  `json:"at"`
	Snippet string `json:"snippet"`
	// Byte offsets of the first match inside Snippet, for highlighting.
	MatchStart int `json:"matchStart"`
	MatchEnd   int `json:"matchEnd"`
}

// SearchGroup is one session with its hits.
type SearchGroup struct {
	SourceID   string      `json:"sourceId"`
	SessionID  string      `json:"sessionId"`
	ProjectKey string      `json:"projectKey"`
	Project    string      `json:"project"`
	Title      string      `json:"title"`
	Agent      model.Agent `json:"agent"`
	EndedAt    int64       `json:"endedAt"`
	CostUSD    float64     `json:"costUSD"`
	Total      int         `json:"total"` // every hit in the session; Hits is capped
	Hits       []SearchHit `json:"hits"`
}

type SearchResult struct {
	Query     string        `json:"query"`
	Groups    []SearchGroup `json:"groups"`
	Hits      int           `json:"hits"`
	Searched  int           `json:"searched"`
	Failed    int           `json:"failed"`
	Truncated bool          `json:"truncated"`
	Ms        int64         `json:"ms"`
}

func clipField(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxSearchField {
		return s
	}
	cut := maxSearchField
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func buildDoc(s *model.Session) *searchDoc {
	d := &searchDoc{}
	add := func(kind, text string, turn int, at int64, step string) {
		text = clipField(text)
		if text == "" {
			return
		}
		d.fields = append(d.fields, docField{kind: kind, text: text, lower: strings.ToLower(text), turn: turn, at: at, step: step})
	}
	for ti, t := range s.Turns {
		if t.Prompt != "(session start)" {
			add("prompt", t.Prompt, ti, t.StartedAt, "")
		}
		for _, st := range t.Steps {
			switch st.Kind {
			case model.StepShell:
				add("command", st.Title, ti, st.StartedAt, st.ID)
			case model.StepRead, model.StepEdit:
				if len(st.Files) > 0 {
					add("file", st.Tool+"  "+strings.Join(st.Files, "  "), ti, st.StartedAt, st.ID)
				} else {
					add("file", st.Title, ti, st.StartedAt, st.ID)
				}
			case model.StepSearch, model.StepTool, model.StepAgent:
				add("tool", st.Title, ti, st.StartedAt, st.ID)
			}
		}
		add("answer", t.Answer, ti, t.StartedAt+t.DurationMs, "")
	}
	return d
}

// snippet cuts a window around [i, j) of text and returns it with the match's offsets in it.
func snippet(text string, i, j int) (string, int, int) {
	start, end := i-snippetRadius, j+snippetRadius
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	pre, post := "", ""
	if start > 0 {
		pre = "…"
	}
	if end < len(text) {
		post = "…"
	}
	// Flatten whitespace while keeping the match offsets.
	var b strings.Builder
	ms, me := -1, -1
	space := false
	b.WriteString(pre)
	for k := start; k < end; {
		if k == i {
			ms = b.Len()
		}
		if k == j {
			me = b.Len()
		}
		r, n := utf8.DecodeRuneInString(text[k:])
		if r == '\n' || r == '\t' || r == '\r' || r == ' ' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
		} else {
			b.WriteString(text[k : k+n])
			space = false
		}
		k += n
	}
	if me < 0 {
		me = b.Len()
	}
	b.WriteString(post)
	if ms < 0 {
		ms = 0
	}
	return b.String(), ms, me
}

// terms splits a query into lower-case words; "quoted phrases" stay together.
func terms(q string) []string {
	var out []string
	q = strings.ToLower(strings.TrimSpace(q))
	for q != "" {
		if q[0] == '"' {
			if k := strings.IndexByte(q[1:], '"'); k >= 0 {
				if p := strings.TrimSpace(q[1 : k+1]); p != "" {
					out = append(out, p)
				}
				q = strings.TrimSpace(q[k+2:])
				continue
			}
			q = q[1:]
		}
		k := strings.IndexAny(q, " \t")
		if k < 0 {
			out = append(out, strings.Trim(q, `"`))
			break
		}
		if w := strings.Trim(q[:k], `"`); w != "" {
			out = append(out, w)
		}
		q = strings.TrimSpace(q[k:])
	}
	return out
}

// doc returns the search copy of a session, reading it from disk when it is new or changed.
func (c *Core) doc(sum model.SessionSummary) (*searchDoc, error) {
	key := sum.SourceID + "/" + sum.ID
	c.mu.RLock()
	e := c.idx[sum.SourceID][sum.ID]
	c.mu.RUnlock()
	if e == nil {
		return nil, errNotFound
	}
	c.search.mu.Lock()
	d := c.search.docs[key]
	c.search.mu.Unlock()
	if d != nil && d.size == e.Size && d.mtime == e.MTime {
		return d, nil
	}
	s, err := c.parse(sum.SourceID, sum.ID)
	if err != nil {
		return nil, err
	}
	d = buildDoc(s)
	d.size, d.mtime = e.Size, e.MTime
	c.search.mu.Lock()
	if c.search.docs == nil {
		c.search.docs = map[string]*searchDoc{}
	}
	c.search.docs[key] = d
	c.search.mu.Unlock()
	return d, nil
}

// Search finds prompts, answers, commands, file paths and tool calls that contain every word of
// the query (case-insensitive; "quoted phrases" match as a whole). kinds limits the fields
// ("" = all; comma-separated list of prompt, answer, command, file, tool).
func (c *Core) Search(query, projectKey, sourceID, agent, kinds string, f Filter) *SearchResult {
	t0 := time.Now()
	res := &SearchResult{Query: query}
	ts := terms(query)
	if len(ts) == 0 {
		return res
	}
	want := map[string]bool{}
	for _, k := range strings.Split(kinds, ",") {
		if k = strings.TrimSpace(k); k != "" {
			want[k] = true
		}
	}
	phrase := strings.Join(ts, " ")
	sums := c.scoped(projectKey, sourceID, agent, f)
	sort.Slice(sums, func(i, j int) bool { return sums[i].EndedAt > sums[j].EndedAt })
	res.Searched = len(sums)

	groups := make([]*SearchGroup, len(sums))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(2, runtime.NumCPU()))
	for i, sum := range sums {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, sum model.SessionSummary) {
			defer wg.Done()
			defer func() { <-sem }()
			d, err := c.doc(sum)
			if err != nil {
				mu.Lock()
				res.Failed++
				mu.Unlock()
				return
			}
			var g *SearchGroup
			for _, fl := range d.fields {
				if len(want) > 0 && !want[fl.kind] {
					continue
				}
				first, firstEnd := -1, -1
				ok := true
				for _, t := range ts {
					k := strings.Index(fl.lower, t)
					if k < 0 {
						ok = false
						break
					}
					if first < 0 {
						first, firstEnd = k, k+len(t)
					}
				}
				if !ok {
					continue
				}
				// Highlight the whole query when it appears as one phrase.
				if len(ts) > 1 {
					if k := strings.Index(fl.lower, phrase); k >= 0 {
						first, firstEnd = k, k+len(phrase)
					}
				}
				if g == nil {
					g = &SearchGroup{SourceID: sum.SourceID, SessionID: sum.ID, ProjectKey: sum.ProjectKey, Project: projectName(sum.ProjectKey),
						Title: sum.Title, Agent: sum.Agent, EndedAt: sum.EndedAt, CostUSD: sum.Usage.CostUSD}
				}
				g.Total++
				if len(g.Hits) < maxHitsPerGroup {
					// Lower-casing can change byte lengths for some scripts; fall back to the field start.
					text := fl.text
					if len(fl.lower) != len(text) {
						first, firstEnd = 0, 0
					}
					sn, ms, me := snippet(text, first, firstEnd)
					g.Hits = append(g.Hits, SearchHit{Kind: fl.kind, Turn: fl.turn, StepID: fl.step, At: fl.at, Snippet: sn, MatchStart: ms, MatchEnd: me})
				}
			}
			groups[i] = g
		}(i, sum)
	}
	wg.Wait()
	for _, g := range groups {
		if g == nil {
			continue
		}
		if res.Hits >= maxHitsPerSearch {
			res.Truncated = true
			break
		}
		res.Hits += g.Total
		res.Groups = append(res.Groups, *g)
	}
	res.Ms = time.Since(t0).Milliseconds()
	return res
}
