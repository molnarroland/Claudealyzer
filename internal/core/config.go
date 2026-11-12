// Package core owns sources, the session index and every query the UI needs.
// Wails services in internal/services are thin wrappers over it.
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"claudealyzer/internal/insight"
	"claudealyzer/internal/model"
	"claudealyzer/internal/pricing"
)

// Billing describes how a source is paid for.
type Billing struct {
	Kind        string  `json:"kind"` // unset | api | subscription | team | free
	Fee         float64 `json:"fee"`  // monthly plan fee (subscription/team)
	RenewDay    int     `json:"renewDay"`
	DiscountPct float64 `json:"discountPct"` // API key discount
	Plan        string  `json:"plan"`        // free-text label, e.g. "Max"
}

type Source struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Agent   model.Agent `json:"agent"`
	HomeDir string      `json:"homeDir"`
	Billing Billing     `json:"billing"`
}

type Config struct {
	Sources     []Source           `json:"sources"`
	Thresholds  insight.Thresholds `json:"thresholds"`
	PricingMode string             `json:"pricingMode"`
	Initialized bool               `json:"initialized"`
}

type configStore struct {
	mu   sync.Mutex
	dir  string
	conf Config
}

func appDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "claudealyzer")
}

func cacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "claudealyzer")
}

func loadConfig() *configStore {
	cs := &configStore{dir: appDir()}
	cs.conf.Thresholds = insight.DefaultThresholds()
	if b, err := os.ReadFile(filepath.Join(cs.dir, "config.json")); err == nil {
		_ = json.Unmarshal(b, &cs.conf)
	}
	if cs.conf.Thresholds.ContextTokens == 0 {
		cs.conf.Thresholds = insight.DefaultThresholds()
	}
	if b, err := os.ReadFile(filepath.Join(cs.dir, "pricing.json")); err == nil {
		var t pricing.Table
		if json.Unmarshal(b, &t) == nil && len(t.Models) > 0 {
			pricing.Set(t)
		}
	}
	pricing.SetMode(cs.conf.PricingMode)
	return cs
}

func (cs *configStore) save() error {
	if err := os.MkdirAll(cs.dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cs.conf, "", "  ")
	return os.WriteFile(filepath.Join(cs.dir, "config.json"), b, 0o644)
}

func savePricing(t pricing.Table) error {
	dir := appDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	return os.WriteFile(filepath.Join(dir, "pricing.json"), b, 0o644)
}

func ExpandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, strings.TrimPrefix(p, "~"))
	}
	return p
}

func ShortHome(p string) string {
	h, _ := os.UserHomeDir()
	if h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

func slug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func (cs *configStore) addSource(src Source) (Source, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	src.HomeDir = ExpandHome(src.HomeDir)
	if src.HomeDir == "" {
		return src, errors.New("folder is required")
	}
	if st, err := os.Stat(src.HomeDir); err != nil || !st.IsDir() {
		return src, fmt.Errorf("folder %s does not exist", src.HomeDir)
	}
	for _, s := range cs.conf.Sources {
		if s.HomeDir == src.HomeDir && s.Agent == src.Agent {
			return src, fmt.Errorf("%s is already a source", ShortHome(src.HomeDir))
		}
	}
	if src.Name == "" {
		src.Name = strings.TrimPrefix(filepath.Base(src.HomeDir), ".")
	}
	base := slug(string(src.Agent) + "-" + src.Name)
	id := base
	for i := 2; cs.find(id) >= 0; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	src.ID = id
	if src.Billing.Kind == "" {
		src.Billing.Kind = "unset"
	}
	cs.conf.Sources = append(cs.conf.Sources, src)
	return src, cs.save()
}

func (cs *configStore) find(id string) int {
	for i, s := range cs.conf.Sources {
		if s.ID == id {
			return i
		}
	}
	return -1
}
