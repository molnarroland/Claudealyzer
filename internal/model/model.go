// Package model holds the agent-neutral shape every adapter produces.
// Times are Unix milliseconds so they cross the Wails bridge as plain numbers.
package model

type Agent string

const (
	Claude   Agent = "claude"
	Codex    Agent = "codex"
	Gemini   Agent = "gemini"
	OpenCode Agent = "opencode"
)

// Usage is one usage shape for every agent. Input is uncached input only.
// Output includes reasoning/thinking tokens; Reasoning is the subset.
type Usage struct {
	Input        int64 `json:"input"`
	CacheWrite5m int64 `json:"cacheWrite5m"`
	CacheWrite1h int64 `json:"cacheWrite1h"`
	CacheRead    int64 `json:"cacheRead"`
	Output       int64 `json:"output"`
	Reasoning    int64 `json:"reasoning"`
	Calls        int   `json:"calls"`

	// List price (API-equivalent) and its split by token type.
	CostUSD        float64 `json:"costUSD"`
	CostInput      float64 `json:"costInput"`
	CostCacheWrite float64 `json:"costCacheWrite"`
	CostCacheRead  float64 `json:"costCacheRead"`
	CostOutput     float64 `json:"costOutput"`
	// Tokens from models with no known price ("price unknown", never $0).
	UnpricedTokens int64 `json:"unpricedTokens"`
}

func (u *Usage) Add(o Usage) {
	u.Input += o.Input
	u.CacheWrite5m += o.CacheWrite5m
	u.CacheWrite1h += o.CacheWrite1h
	u.CacheRead += o.CacheRead
	u.Output += o.Output
	u.Reasoning += o.Reasoning
	u.Calls += o.Calls
	u.CostUSD += o.CostUSD
	u.CostInput += o.CostInput
	u.CostCacheWrite += o.CostCacheWrite
	u.CostCacheRead += o.CostCacheRead
	u.CostOutput += o.CostOutput
	u.UnpricedTokens += o.UnpricedTokens
}

func (u Usage) CacheWrite() int64 { return u.CacheWrite5m + u.CacheWrite1h }

// Context is the prompt size: everything sent in.
func (u Usage) Context() int64 { return u.Input + u.CacheWrite() + u.CacheRead }

func (u Usage) Total() int64 { return u.Context() + u.Output }

// Call is one API response, counted once.
type Call struct {
	Model string `json:"model"`
	Time  int64  `json:"time"`
	Turn  int    `json:"turn"`
	Usage Usage  `json:"usage"`
}

type StepKind string

const (
	StepThinking StepKind = "thinking"
	StepText     StepKind = "text"
	StepShell    StepKind = "shell"
	StepRead     StepKind = "read"
	StepEdit     StepKind = "edit"
	StepSearch   StepKind = "search"
	StepAgent    StepKind = "agent"
	StepTool     StepKind = "tool"
	StepSystem   StepKind = "system"
)

type Step struct {
	ID         string   `json:"id"`
	Index      int      `json:"index"`
	Kind       StepKind `json:"kind"`
	Tool       string   `json:"tool"`
	Title      string   `json:"title"`
	Input      string   `json:"input"`
	Output     string   `json:"output"`
	OutputFull bool     `json:"outputFull"` // false when Output was truncated
	StartedAt  int64    `json:"startedAt"`
	DurationMs int64    `json:"durationMs"`
	IsError    bool     `json:"isError"`
	ExitCode   *int     `json:"exitCode"`
	// Approximate tokens the result added to context.
	ResultTokens int64    `json:"resultTokens"`
	SubagentID   string   `json:"subagentId"`
	Category     string   `json:"category"` // test | build | heavy | ""
	Files        []string `json:"files"`
}

type Turn struct {
	Index         int     `json:"index"`
	Prompt        string  `json:"prompt"`
	StartedAt     int64   `json:"startedAt"`
	DurationMs    int64   `json:"durationMs"`
	Steps         []Step  `json:"steps"`
	Answer        string  `json:"answer"`
	Usage         Usage   `json:"usage"`         // this turn's own calls
	SubagentUsage Usage   `json:"subagentUsage"` // subagents started in this turn
	PeakContext   int64   `json:"peakContext"`
	Compacted     bool    `json:"compacted"`
	Models        []string `json:"models"`
}

type Subagent struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Description  string `json:"description"`
	Model        string `json:"model"`
	ParentStepID string `json:"parentStepId"`
	ParentTurn   int    `json:"parentTurn"`
	Usage        Usage  `json:"usage"`
	PeakContext  int64  `json:"peakContext"`
	TurnCount    int    `json:"turnCount"`
	Turns        []Turn `json:"turns,omitempty"`
	Calls        []Call `json:"-"`
	FullOutputs  map[string]string `json:"-"`
}

type SessionSummary struct {
	ID            string             `json:"id"`
	SourceID      string             `json:"sourceId"`
	Agent         Agent              `json:"agent"`
	ProjectKey    string             `json:"projectKey"`
	Title         string             `json:"title"`
	StartedAt     int64              `json:"startedAt"`
	EndedAt       int64              `json:"endedAt"`
	GitBranch     string             `json:"gitBranch"`
	Models        []string           `json:"models"`
	MainModel     string             `json:"mainModel"`
	TurnCount     int                `json:"turnCount"`
	SubagentCount int                `json:"subagentCount"`
	Usage         Usage              `json:"usage"` // includes subagents
	ByModel       map[string]Usage   `json:"byModel"`
	SubagentUsage Usage              `json:"subagentUsage"`
	PeakContext   int64              `json:"peakContext"`
	ReportedCost  *float64           `json:"reportedCost"`
	AgentVersion  string             `json:"agentVersion"`
	Daily         map[string]float64 `json:"daily"` // YYYY-MM-DD -> list price
	Days          map[string]DayStat `json:"days"`  // YYYY-MM-DD -> prompts and usage (subagents included)
	FindingsHigh  int                `json:"findingsHigh"`
	FindingsMed   int                `json:"findingsMed"`
	RateLimitPct  *float64           `json:"rateLimitPct"`
	Warnings      []string           `json:"warnings"`
	Path          string             `json:"path"`
	NoUsageData   bool               `json:"noUsageData"`
}

// DayStat is one session's activity on one local calendar day.
type DayStat struct {
	Prompts int   `json:"prompts"`
	Usage   Usage `json:"usage"`
}

type Session struct {
	SessionSummary
	Turns     []Turn     `json:"turns"`
	Subagents []Subagent `json:"subagents"`
	Calls     []Call     `json:"-"`
	// Untruncated tool outputs keyed by step id (only those that were cut).
	FullOutputs map[string]string `json:"-"`
}
