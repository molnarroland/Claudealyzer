// Package services exposes core to the Angular frontend as Wails v3 services.
// Keep this layer thin: every Wails-specific call lives here or in main.go.
package services

import (
	"context"
	"os"
	"path/filepath"
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

func (s *SourceService) List() []core.SourceInfo       { return s.Core.Sources() }
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

func (h *HistoryService) Projects(sourceID, agent string, f core.Filter) []core.Project {
	return h.Core.Projects(sourceID, agent, f)
}

func (h *HistoryService) Sessions(projectKey, sourceID, agent, sortBy string, f core.Filter) []core.SessionRow {
	return h.Core.Sessions(projectKey, sourceID, agent, sortBy, f)
}

// Models lists the models seen in a source, for the model filter.
func (h *HistoryService) Models(sourceID, agent string) []core.ModelInfo {
	return h.Core.Models(sourceID, agent)
}

// Search finds text in prompts, answers, commands, file paths and tool calls.
// kinds: "" for all, or a comma-separated list of prompt, answer, command, file, tool.
func (h *HistoryService) Search(query, projectKey, sourceID, agent, kinds string, f core.Filter) *core.SearchResult {
	return h.Core.Search(query, projectKey, sourceID, agent, kinds, f)
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

func (u *UsageService) Overview(projectKey, sourceID, agent string, days int, f core.Filter) *core.Overview {
	return u.Core.Overview(projectKey, sourceID, agent, days, f)
}

// Calendar: month is YYYY-MM ("" = current month); projectKey "" = whole source.
func (u *UsageService) Calendar(projectKey, sourceID, agent, month string, f core.Filter) *core.Calendar {
	return u.Core.Calendar(projectKey, sourceID, agent, month, f)
}

// Day: date is YYYY-MM-DD.
func (u *UsageService) Day(projectKey, sourceID, agent, date string, f core.Filter) *core.DayDetail {
	return u.Core.Day(projectKey, sourceID, agent, date, f)
}

// Project: one project's dashboard. days limits it (0 = all time).
func (u *UsageService) Project(projectKey, sourceID, agent string, days int, f core.Filter) (*core.ProjectReport, error) {
	return u.Core.ProjectReport(projectKey, sourceID, agent, days, f)
}

func (u *UsageService) CompareSessions(aSource, aID, bSource, bID string) (*core.Comparison, error) {
	return u.Core.CompareSessions(aSource, aID, bSource, bID)
}

// ComparePeriods: dates are YYYY-MM-DD, both ends inclusive.
func (u *UsageService) ComparePeriods(projectKey, sourceID, agent, model, aFrom, aTo, bFrom, bTo string) (*core.Comparison, error) {
	return u.Core.ComparePeriods(projectKey, sourceID, agent, model, aFrom, aTo, bFrom, bTo)
}

func (u *UsageService) CostReport(sourceID, sessionID string, factor float64) (*core.CostReport, error) {
	return u.Core.CostReport(sourceID, sessionID, factor)
}

// ---------- InsightService ----------

type InsightService struct{ Core *core.Core }

func (i *InsightService) Analysis(sourceID, sessionID, subagentID string) (*core.Analysis, error) {
	return i.Core.Analysis(sourceID, sessionID, subagentID)
}

// Trends: per week, how often each rule fired over the last `weeks` weeks.
func (i *InsightService) Trends(projectKey, sourceID, agent string, weeks int, f core.Filter) *core.TrendReport {
	return i.Core.InsightTrends(projectKey, sourceID, agent, weeks, f)
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

// ---------- BudgetService ----------

type BudgetService struct {
	Core *core.Core
	// Authorize asks the OS for permission to post notifications (nil when unavailable).
	Authorize func() (bool, error)
}

func (b *BudgetService) Budgets() []core.Budget { return b.Core.Budgets() }

func (b *BudgetService) SaveBudgets(bs []core.Budget) error {
	err := b.Core.SaveBudgets(bs)
	if err == nil {
		go b.Core.CheckAlerts(nil)
	}
	return err
}

func (b *BudgetService) Status() []core.BudgetStatus { return b.Core.BudgetStatuses() }

func (b *BudgetService) Limits() []core.SourceLimits { return b.Core.Limits() }

func (b *BudgetService) AlertSettings() core.AlertSettings { return b.Core.AlertSettings() }

// SaveAlertSettings stores the settings; turning notifications on asks the OS for permission.
// It returns whether notifications may be shown.
func (b *BudgetService) SaveAlertSettings(a core.AlertSettings) (bool, error) {
	allowed := false
	if a.Notify && b.Authorize != nil {
		ok, err := b.Authorize()
		allowed = ok && err == nil
	}
	if err := b.Core.SaveAlertSettings(a); err != nil {
		return false, err
	}
	go b.Core.CheckAlerts(nil)
	return allowed, nil
}

func (b *BudgetService) RecentAlerts() []core.Alert { return b.Core.RecentAlerts() }

// ---------- ExportService ----------

// ExportService builds exports and asks where to save them. Each method returns the saved
// path, or "" when the user cancelled.
type ExportService struct{ Core *core.Core }

func save(name, label, ext string, data []byte) (string, error) {
	app := application.Get()
	path, err := app.Dialog.SaveFile().
		SetFilename(name+"."+ext).
		AddFilter(label, "*."+ext).
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil || path == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += "." + ext
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// SessionsCSV: the sessions of a scope (what Overview lists).
func (e *ExportService) SessionsCSV(projectKey, sourceID, agent string, days int, f core.Filter) (string, error) {
	return save(e.Core.ExportName("sessions", "", ""), "CSV", "csv", e.Core.SessionsCSV(projectKey, sourceID, agent, days, f))
}

// CalendarCSV: one row per day of a month (YYYY-MM), or every active day when month is "".
func (e *ExportService) CalendarCSV(projectKey, sourceID, agent, month string, f core.Filter) (string, error) {
	kind := "days"
	if month != "" {
		kind = "days-" + month
	}
	return save(e.Core.ExportName(kind, "", ""), "CSV", "csv", e.Core.CalendarCSV(projectKey, sourceID, agent, month, f))
}

func (e *ExportService) SessionMarkdown(sourceID, id string, withSteps bool) (string, error) {
	b, err := e.Core.SessionMarkdown(sourceID, id, withSteps)
	if err != nil {
		return "", err
	}
	return save(e.Core.ExportName("session", sourceID, id), "Markdown", "md", b)
}

func (e *ExportService) SessionJSON(sourceID, id string) (string, error) {
	b, err := e.Core.SessionJSON(sourceID, id)
	if err != nil {
		return "", err
	}
	return save(e.Core.ExportName("session", sourceID, id), "JSON", "json", b)
}

// SessionMarkdownText returns the Markdown without saving it (for the clipboard).
func (e *ExportService) SessionMarkdownText(sourceID, id string, withSteps bool) (string, error) {
	b, err := e.Core.SessionMarkdown(sourceID, id, withSteps)
	return string(b), err
}
