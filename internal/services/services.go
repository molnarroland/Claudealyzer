// Package services exposes core to the Angular frontend as Wails v3 services.
// Keep this layer thin: every Wails-specific call lives here or in main.go.
package services

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"claudealyzer/internal/adapters"
	"claudealyzer/internal/core"
	"claudealyzer/internal/insight"
	"claudealyzer/internal/model"
)

// ---------- SourceService ----------

type SourceService struct{ Core *core.Core }

func (s *SourceService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	go func() {
		s.Core.Scan(ctx)
		s.Core.Watch(ctx, 15*time.Second)
	}()
	return nil
}

func (s *SourceService) List() []core.SourceInfo        { return s.Core.Sources() }
func (s *SourceService) Detect() []core.DetectedSource { return s.Core.Detect() }
func (s *SourceService) Check(agent model.Agent, dir string) adapters.Detection {
	return s.Core.CheckFolder(agent, dir)
}

func (s *SourceService) Add(name string, agent model.Agent, dir string) (core.Source, error) {
	src, err := s.Core.AddSource(name, agent, dir)
	if err == nil {
		go s.Core.Scan(context.Background())
	}
	return src, err
}

func (s *SourceService) Update(src core.Source) error { return s.Core.UpdateSource(src) }

func (s *SourceService) Remove(id string) error {
	err := s.Core.RemoveSource(id)
	if err == nil {
		go s.Core.Scan(context.Background())
	}
	return err
}

func (s *SourceService) Rescan() { go s.Core.Scan(context.Background()) }

func (s *SourceService) Status() core.ScanStatus { return s.Core.Status() }

// PickFolder opens the native folder picker. Returns "" when cancelled.
func (s *SourceService) PickFolder() (string, error) {
	app := application.Get()
	return app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		ShowHiddenFiles(true).
		SetTitle("Choose the agent's history folder").
		PromptForSingleSelection()
}

// ---------- HistoryService ----------

type HistoryService struct{ Core *core.Core }

func (h *HistoryService) Projects(sourceID, agent string) []core.Project {
	return h.Core.Projects(sourceID, agent)
}

func (h *HistoryService) Sessions(projectKey, sourceID, agent, sortBy string) []core.SessionRow {
	return h.Core.Sessions(projectKey, sourceID, agent, sortBy)
}

func (h *HistoryService) Session(sourceID, id string) (*core.SessionDetail, error) {
	return h.Core.Session(sourceID, id)
}

func (h *HistoryService) Subagent(sourceID, sessionID, agentID string) (*model.Subagent, error) {
	return h.Core.Subagent(sourceID, sessionID, agentID)
}

func (h *HistoryService) Step(sourceID, sessionID, subagentID, stepID string) (*core.StepDetail, error) {
	return h.Core.StepDetail(sourceID, sessionID, subagentID, stepID)
}

// ---------- UsageService ----------

type UsageService struct{ Core *core.Core }

func (u *UsageService) Overview(projectKey, sourceID, agent string, days int) *core.Overview {
	return u.Core.Overview(projectKey, sourceID, agent, days)
}

// Calendar: month is YYYY-MM ("" = current month); projectKey "" = whole source.
func (u *UsageService) Calendar(projectKey, sourceID, agent, month string) *core.Calendar {
	return u.Core.Calendar(projectKey, sourceID, agent, month)
}

// Day: date is YYYY-MM-DD.
func (u *UsageService) Day(projectKey, sourceID, agent, date string) *core.DayDetail {
	return u.Core.Day(projectKey, sourceID, agent, date)
}

func (u *UsageService) CostReport(sourceID, sessionID string, factor float64) (*core.CostReport, error) {
	return u.Core.CostReport(sourceID, sessionID, factor)
}

// ---------- InsightService ----------

type InsightService struct{ Core *core.Core }

func (i *InsightService) Analysis(sourceID, sessionID, subagentID string) (*core.Analysis, error) {
	return i.Core.Analysis(sourceID, sessionID, subagentID)
}

func (i *InsightService) Thresholds() insight.Thresholds { return i.Core.Thresholds() }

func (i *InsightService) DefaultThresholds() insight.Thresholds { return insight.DefaultThresholds() }

func (i *InsightService) SaveThresholds(t insight.Thresholds) error {
	err := i.Core.SaveThresholds(t)
	if err == nil {
		go i.Core.Scan(context.Background())
	}
	return err
}

// ---------- PricingService ----------

type PricingService struct{ Core *core.Core }

func (p *PricingService) Get() core.PricingInfo { return p.Core.Pricing() }

func (p *PricingService) Save(info core.PricingInfo) error {
	err := p.Core.SavePricing(info)
	if err == nil {
		go p.Core.Scan(context.Background())
	}
	return err
}

func (p *PricingService) Reset() error {
	err := p.Core.ResetPricing()
	if err == nil {
		go p.Core.Scan(context.Background())
	}
	return err
}
