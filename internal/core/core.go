package core

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"claudealyzer/internal/adapters"
	_ "claudealyzer/internal/adapters/claude"
	_ "claudealyzer/internal/adapters/codex"
	_ "claudealyzer/internal/adapters/gemini"
	_ "claudealyzer/internal/adapters/opencode"
	"claudealyzer/internal/insight"
	"claudealyzer/internal/model"
	"claudealyzer/internal/pricing"
)

const indexVersion = 6

type entry struct {
	Size  int64                `json:"size"`
	MTime int64                `json:"mtime"`
	Extra string               `json:"extra"`
	Sum   model.SessionSummary `json:"sum"`
	Err   string               `json:"err,omitempty"`
}

type indexFile struct {
	Version int                          `json:"version"`
	Hash    string                       `json:"hash"`
	Sources map[string]map[string]*entry `json:"sources"`
}

type loaded struct {
	key      string
	session  *model.Session
	findings []insight.Finding
}

// Emitter sends events to the UI ("scan:progress", "index:updated", "session:updated").
type Emitter func(name string, data any)

type Core struct {
	cfg    *configStore
	mu     sync.RWMutex
	idx    map[string]map[string]*entry
	cache  []loaded // most recently used first
	emit   Emitter
	scanMu sync.Mutex
	status ScanStatus
	search searchIndex
	alerts alertState
	notify Notifier
}

var errNotFound = errors.New("session not found")

type ScanStatus struct {
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Source  string `json:"source"`
	Last    int64  `json:"last"`
}

func New(emit Emitter) *Core {
	if emit == nil {
		emit = func(string, any) {}
	}
	c := &Core{cfg: loadConfig(), idx: map[string]map[string]*entry{}, emit: emit}
	c.loadIndex()
	if !c.cfg.conf.Initialized {
		for _, d := range c.Detect() {
			if d.Supported && d.OK && !d.Added {
				_, _ = c.cfg.addSource(Source{Name: d.Name, Agent: d.Agent, HomeDir: d.Dir})
			}
		}
		c.cfg.conf.Initialized = true
		_ = c.cfg.save()
	}
	return c
}

func (c *Core) hash() string {
	b, _ := json.Marshal(c.cfg.conf.Thresholds)
	h := sha1.Sum(append(b, pricing.Hash()...))
	return hex.EncodeToString(h[:8])
}

func (c *Core) loadIndex() {
	b, err := os.ReadFile(filepath.Join(cacheDir(), "index.json"))
	if err != nil {
		return
	}
	var f indexFile
	if json.Unmarshal(b, &f) != nil || f.Version != indexVersion || f.Hash != c.hash() || f.Sources == nil {
		return
	}
	c.idx = f.Sources
}

func (c *Core) saveIndex() {
	c.mu.RLock()
	b, err := json.Marshal(indexFile{Version: indexVersion, Hash: c.hash(), Sources: c.idx})
	c.mu.RUnlock()
	if err != nil {
		return
	}
	_ = os.MkdirAll(cacheDir(), 0o755)
	tmp := filepath.Join(cacheDir(), "index.json.tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(cacheDir(), "index.json"))
	}
}

// invalidate drops cached summaries (pricing or thresholds changed).
func (c *Core) invalidate() {
	c.mu.Lock()
	c.idx = map[string]map[string]*entry{}
	c.cache = nil
	c.mu.Unlock()
}

func (c *Core) sources() []Source {
	c.cfg.mu.Lock()
	defer c.cfg.mu.Unlock()
	return append([]Source(nil), c.cfg.conf.Sources...)
}

func (c *Core) source(id string) (Source, bool) {
	for _, s := range c.sources() {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

func (c *Core) Status() ScanStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// Scan refreshes summaries of every source; only changed files are parsed.
// It returns the keys ("source/session") that changed.
func (c *Core) Scan(ctx context.Context) []string {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()
	type job struct {
		src Source
		ad  adapters.Adapter
		f   adapters.SessionFile
	}
	var jobs []job
	present := map[string]map[string]bool{}
	for _, src := range c.sources() {
		ad, ok := adapters.Get(src.Agent)
		if !ok {
			continue
		}
		files, err := ad.ListSessions(src.HomeDir)
		if err != nil {
			continue
		}
		present[src.ID] = map[string]bool{}
		c.mu.RLock()
		m := c.idx[src.ID]
		for _, f := range files {
			present[src.ID][f.ID] = true
			if e, ok := m[f.ID]; ok && e.Size == f.Size && e.MTime == f.MTime && e.Extra == f.Extra {
				continue
			}
			jobs = append(jobs, job{src, ad, f})
		}
		c.mu.RUnlock()
	}
	// Drop removed sources and deleted sessions.
	c.mu.Lock()
	for sid, m := range c.idx {
		p, ok := present[sid]
		if !ok {
			delete(c.idx, sid)
			continue
		}
		for id := range m {
			if !p[id] {
				delete(m, id)
			}
		}
	}
	c.status = ScanStatus{Running: len(jobs) > 0, Total: len(jobs)}
	c.mu.Unlock()
	if len(jobs) == 0 {
		return nil
	}
	// Big files first so the pool stays busy.
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].f.Size > jobs[j].f.Size })
	th := c.cfg.conf.Thresholds
	ch := make(chan job)
	var wg sync.WaitGroup
	var done int
	var changed []string
	var dmu sync.Mutex
	lastEmit := time.Now()
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				e := &entry{Size: j.f.Size, MTime: j.f.MTime, Extra: j.f.Extra}
				s, err := j.ad.LoadSession(ctx, j.src.HomeDir, j.f)
				if err != nil {
					e.Err = err.Error()
					e.Sum = model.SessionSummary{ID: j.f.ID, SourceID: j.src.ID, Agent: j.src.Agent, Path: j.f.Path, Title: "Unreadable session", Warnings: []string{err.Error()}}
				} else {
					s.SourceID = j.src.ID
					fs := insight.Session(s, th)
					s.FindingsHigh, s.FindingsMed = insight.Count(insight.Group(fs))
					s.Rules = insight.Rules(fs)
					e.Sum = s.SessionSummary
				}
				c.mu.Lock()
				if c.idx[j.src.ID] == nil {
					c.idx[j.src.ID] = map[string]*entry{}
				}
				c.idx[j.src.ID][j.f.ID] = e
				c.dropCached(j.src.ID + "/" + j.f.ID)
				c.mu.Unlock()
				dmu.Lock()
				done++
				changed = append(changed, j.src.ID+"/"+j.f.ID)
				if time.Since(lastEmit) > 250*time.Millisecond || done == len(jobs) {
					lastEmit = time.Now()
					st := ScanStatus{Running: done < len(jobs), Done: done, Total: len(jobs), Source: j.src.Name}
					c.mu.Lock()
					c.status = st
					c.mu.Unlock()
					c.emit("scan:progress", st)
				}
				dmu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		ch <- j
	}
	close(ch)
	wg.Wait()
	c.mu.Lock()
	c.status = ScanStatus{Done: done, Total: len(jobs), Last: time.Now().UnixMilli()}
	c.mu.Unlock()
	c.saveIndex()
	c.emit("index:updated", c.Status())
	return changed
}

// Watch rescans periodically and reports changed sessions (live sessions update).
func (c *Core) Watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	c.CheckAlerts(nil)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ch := c.Scan(ctx)
			if len(ch) > 0 {
				c.emit("session:updated", ch)
			}
			c.CheckAlerts(ch)
		}
	}
}

func (c *Core) dropCached(key string) {
	for i, l := range c.cache {
		if l.key == key {
			c.cache = append(c.cache[:i], c.cache[i+1:]...)
			return
		}
	}
}

// Load returns the full session (cached, most recent 6).
func (c *Core) Load(sourceID, id string) (*model.Session, []insight.Finding, error) {
	key := sourceID + "/" + id
	c.mu.Lock()
	for i, l := range c.cache {
		if l.key == key {
			c.cache = append([]loaded{l}, append(c.cache[:i], c.cache[i+1:]...)...)
			c.mu.Unlock()
			return l.session, l.findings, nil
		}
	}
	c.mu.Unlock()
	s, err := c.parse(sourceID, id)
	if err != nil {
		return nil, nil, err
	}
	fs := insight.Session(s, c.cfg.conf.Thresholds)
	s.FindingsHigh, s.FindingsMed = insight.Count(insight.Group(fs))
	s.Rules = insight.Rules(fs)
	c.mu.Lock()
	c.cache = append([]loaded{{key, s, fs}}, c.cache...)
	if len(c.cache) > 6 {
		c.cache = c.cache[:6]
	}
	c.mu.Unlock()
	return s, fs, nil
}

// parse reads a session from disk without touching the cache.
func (c *Core) parse(sourceID, id string) (*model.Session, error) {
	c.mu.RLock()
	e := c.idx[sourceID][id]
	c.mu.RUnlock()
	src, ok := c.source(sourceID)
	if !ok || e == nil {
		return nil, errNotFound
	}
	ad, ok := adapters.Get(src.Agent)
	if !ok {
		return nil, fmt.Errorf("no adapter for %s", src.Agent)
	}
	s, err := ad.LoadSession(context.Background(), src.HomeDir, adapters.SessionFile{ID: id, Path: e.Sum.Path, Size: e.Size, MTime: e.MTime})
	if err != nil {
		return nil, err
	}
	s.SourceID = sourceID
	return s, nil
}

func (c *Core) summaries(sourceID string, agent string, f Filter) []model.SessionSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()
	all := sourceID == "" || sourceID == "all"
	var out []model.SessionSummary
	// With every source merged, the same session can exist in two folders
	// (for example a copied profile). Keep the most complete copy once.
	seen := map[string]int{}
	for sid, m := range c.idx {
		if !all && sid != sourceID {
			continue
		}
		for _, e := range m {
			if agent != "" && agent != "all" && string(e.Sum.Agent) != agent {
				continue
			}
			if e.Sum.TurnCount == 0 && e.Err == "" {
				continue // empty sessions (e.g. only /clear)
			}
			if !f.match(&e.Sum) {
				continue
			}
			if all {
				key := string(e.Sum.Agent) + "/" + e.Sum.ID
				if i, ok := seen[key]; ok {
					cur := out[i]
					if e.Sum.EndedAt > cur.EndedAt || (e.Sum.EndedAt == cur.EndedAt && e.Sum.Usage.Calls > cur.Usage.Calls) {
						out[i] = e.Sum
					}
					continue
				}
				seen[key] = len(out)
			}
			out = append(out, e.Sum)
		}
	}
	return out
}

func dayOf(ms int64) string { return time.UnixMilli(ms).Local().Format("2006-01-02") }

func projectName(key string) string {
	if strings.HasPrefix(key, "gemini:") {
		return strings.TrimPrefix(key, "gemini:")
	}
	b := filepath.Base(key)
	if b == "." || b == "/" || b == "" {
		return key
	}
	return b
}
