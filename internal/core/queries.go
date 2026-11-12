package core

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"claudealyzer/internal/adapters"
	"claudealyzer/internal/insight"
	"claudealyzer/internal/model"
	"claudealyzer/internal/pricing"
)

// ---------- sources ----------

type DetectedSource struct {
	Name      string      `json:"name"`
	Agent     model.Agent `json:"agent"`
	Dir       string      `json:"dir"`
	OK        bool        `json:"ok"`
	Message   string      `json:"message"`
	Supported bool        `json:"supported"`
	Added     bool        `json:"added"`
}

// Detect looks for agent homes in the usual places.
func (c *Core) Detect() []DetectedSource {
	home, _ := os.UserHomeDir()
	var cands []DetectedSource
	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && strings.HasPrefix(n, ".claude") && !strings.Contains(n, " ") {
			if _, err := os.Stat(filepath.Join(home, n, "projects")); err == nil {
				cands = append(cands, DetectedSource{Name: strings.TrimPrefix(n, "."), Agent: model.Claude, Dir: filepath.Join(home, n)})
			}
		}
	}
	cands = append(cands,
		DetectedSource{Name: "codex", Agent: model.Codex, Dir: filepath.Join(home, ".codex")},
		DetectedSource{Name: "gemini", Agent: model.Gemini, Dir: filepath.Join(home, ".gemini")},
		DetectedSource{Name: "opencode", Agent: model.OpenCode, Dir: filepath.Join(home, ".local", "share", "opencode")},
	)
	added := map[string]bool{}
	for _, s := range c.sources() {
		added[string(s.Agent)+s.HomeDir] = true
	}
	var out []DetectedSource
	for _, d := range cands {
		if _, err := os.Stat(d.Dir); err != nil {
			continue
		}
		d.Added = added[string(d.Agent)+d.Dir]
		if ad, ok := adapters.Get(d.Agent); ok {
			d.Supported = true
			det := ad.Detect(d.Dir)
			d.OK, d.Message = det.OK, det.Message
		} else {
			d.Message = "Found, but this agent is not supported yet"
		}
		out = append(out, d)
	}
	return out
}

func (c *Core) CheckFolder(agent model.Agent, dir string) adapters.Detection {
	ad, ok := adapters.Get(agent)
	if !ok {
		return adapters.Detection{Message: "This agent is not supported yet"}
	}
	return ad.Detect(ExpandHome(dir))
}

type SourceInfo struct {
	Source
	DirShort      string  `json:"dirShort"`
	Sessions      int     `json:"sessions"`
	CostUSD       float64 `json:"costUSD"`
	Warnings      int     `json:"warnings"`
	Version       string  `json:"version"`
	Detection     string  `json:"detection"`
	OK            bool    `json:"ok"`
	Supported     bool    `json:"supported"`
}

func (c *Core) Sources() []SourceInfo {
	var out []SourceInfo
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, s := range c.sources() {
		si := SourceInfo{Source: s, DirShort: ShortHome(s.HomeDir)}
		if ad, ok := adapters.Get(s.Agent); ok {
			si.Supported = true
			d := ad.Detect(s.HomeDir)
			si.OK, si.Detection = d.OK, d.Message
		}
		var last int64
		for _, e := range c.idx[s.ID] {
			if e.Sum.TurnCount == 0 {
				continue
			}
			si.Sessions++
			si.CostUSD += e.Sum.Usage.CostUSD
			if len(e.Sum.Warnings) > 0 {
				si.Warnings++
			}
			if e.Sum.StartedAt > last && e.Sum.AgentVersion != "" {
				last, si.Version = e.Sum.StartedAt, e.Sum.AgentVersion
			}
		}
		out = append(out, si)
	}
	return out
}

func (c *Core) AddSource(name string, agent model.Agent, dir string) (Source, error) {
	if _, ok := adapters.Get(agent); !ok {
		return Source{}, errors.New("this agent is not supported yet")
	}
	return c.cfg.addSource(Source{Name: name, Agent: agent, HomeDir: dir})
}

func (c *Core) UpdateSource(s Source) error {
	c.cfg.mu.Lock()
	defer c.cfg.mu.Unlock()
	i := c.cfg.find(s.ID)
	if i < 0 {
		return errors.New("source not found")
	}
	cur := &c.cfg.conf.Sources[i]
	if s.Name != "" {
		cur.Name = s.Name
	}
	cur.Billing = s.Billing
	return c.cfg.save()
}

func (c *Core) RemoveSource(id string) error {
	c.cfg.mu.Lock()
	i := c.cfg.find(id)
	if i < 0 {
		c.cfg.mu.Unlock()
		return errors.New("source not found")
	}
	c.cfg.conf.Sources = append(c.cfg.conf.Sources[:i], c.cfg.conf.Sources[i+1:]...)
	err := c.cfg.save()
	c.cfg.mu.Unlock()
	c.mu.Lock()
	delete(c.idx, id)
	c.mu.Unlock()
	return err
}

// ---------- settings ----------

func (c *Core) Thresholds() insight.Thresholds { return c.cfg.conf.Thresholds }

func (c *Core) SaveThresholds(t insight.Thresholds) error {
	c.cfg.mu.Lock()
	c.cfg.conf.Thresholds = t
	err := c.cfg.save()
	c.cfg.mu.Unlock()
	c.invalidate()
	return err
}

type PricingInfo struct {
	Table pricing.Table `json:"table"`
	Mode  string        `json:"mode"`
}

func (c *Core) Pricing() PricingInfo { return PricingInfo{Table: pricing.Get(), Mode: pricing.Mode()} }

func (c *Core) SavePricing(p PricingInfo) error {
	pricing.Set(p.Table)
	pricing.SetMode(p.Mode)
	c.cfg.mu.Lock()
	c.cfg.conf.PricingMode = pricing.Mode()
	err := c.cfg.save()
	c.cfg.mu.Unlock()
	if err == nil {
		err = savePricing(p.Table)
	}
	c.invalidate()
	return err
}

func (c *Core) ResetPricing() error {
	return c.SavePricing(PricingInfo{Table: pricing.Default(), Mode: pricing.ModeOnDay})
}

// ---------- projects and sessions ----------

type Project struct {
	Key          string        `json:"key"`
	Name         string        `json:"name"`
	Path         string        `json:"path"`
	Agents       []model.Agent `json:"agents"`
	SessionCount int           `json:"sessionCount"`
	CostUSD      float64       `json:"costUSD"`
	LastActive   int64         `json:"lastActive"`
	Unpriced     bool          `json:"unpriced"`
}

func (c *Core) Projects(sourceID, agent string) []Project {
	m := map[string]*Project{}
	for _, s := range c.summaries(sourceID, agent) {
		p := m[s.ProjectKey]
		if p == nil {
			p = &Project{Key: s.ProjectKey, Name: projectName(s.ProjectKey), Path: ShortHome(s.ProjectKey)}
			m[s.ProjectKey] = p
		}
		p.SessionCount++
		p.CostUSD += s.Usage.CostUSD
		p.Unpriced = p.Unpriced || s.Usage.UnpricedTokens > 0
		if s.EndedAt > p.LastActive {
			p.LastActive = s.EndedAt
		}
		has := false
		for _, a := range p.Agents {
			has = has || a == s.Agent
		}
		if !has {
			p.Agents = append(p.Agents, s.Agent)
		}
	}
	out := make([]Project, 0, len(m))
	for _, p := range m {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActive > out[j].LastActive })
	return out
}

type SessionRow struct {
	model.SessionSummary
	SourceName   string  `json:"sourceName"`
	EffectiveUSD float64 `json:"effectiveUSD"`
}

// Sessions lists the sessions of a project ("" = all projects). sort: recent | cost.
func (c *Core) Sessions(projectKey, sourceID, agent, sortBy string) []SessionRow {
	names := map[string]string{}
	for _, s := range c.sources() {
		names[s.ID] = s.Name
	}
	var out []SessionRow
	for _, s := range c.summaries(sourceID, agent) {
		if projectKey != "" && s.ProjectKey != projectKey {
			continue
		}
		eff := c.effective(s.SourceID, s.Usage.CostUSD, s.StartedAt)
		out = append(out, SessionRow{SessionSummary: s, SourceName: names[s.SourceID], EffectiveUSD: eff.Effective})
	}
	sort.Slice(out, func(i, j int) bool {
		if sortBy == "cost" {
			return out[i].Usage.CostUSD > out[j].Usage.CostUSD
		}
		return out[i].EndedAt > out[j].EndedAt
	})
	return out
}

type SessionDetail struct {
	Session    *model.Session    `json:"session"`
	Findings   []insight.Finding `json:"findings"`     // grouped, session scope
	All        []insight.Finding `json:"allFindings"`  // ungrouped, for turn scope
	SourceName string            `json:"sourceName"`
	Billing    Billing           `json:"billing"`
	Effective  Effective         `json:"effective"`
}

func (c *Core) Session(sourceID, id string) (*SessionDetail, error) {
	s, fs, err := c.Load(sourceID, id)
	if err != nil {
		return nil, err
	}
	src, _ := c.source(sourceID)
	// Ship subagent transcripts only on request: keep the payload small.
	cp := *s
	cp.Subagents = make([]model.Subagent, len(s.Subagents))
	for i, a := range s.Subagents {
		a.Turns = nil
		cp.Subagents[i] = a
	}
	return &SessionDetail{Session: &cp, Findings: insight.Group(fs), All: fs, SourceName: src.Name, Billing: src.Billing, Effective: c.effective(sourceID, s.Usage.CostUSD, s.StartedAt)}, nil
}

func (c *Core) Subagent(sourceID, sessionID, agentID string) (*model.Subagent, error) {
	s, _, err := c.Load(sourceID, sessionID)
	if err != nil {
		return nil, err
	}
	for i := range s.Subagents {
		if s.Subagents[i].ID == agentID {
			return &s.Subagents[i], nil
		}
	}
	return nil, errors.New("subagent not found")
}
