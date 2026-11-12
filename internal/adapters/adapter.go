// Package adapters turns each agent's on-disk history into the shared model.
package adapters

import (
	"context"

	"claudealyzer/internal/model"
)

// SessionFile is one session on disk, cheap to list (path + size + mtime).
type SessionFile struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
	// Extra fingerprint for sessions spread over several files (subagents).
	Extra string `json:"extra"`
}

type Detection struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type Adapter interface {
	Agent() model.Agent
	Detect(home string) Detection
	ListSessions(home string) ([]SessionFile, error)
	LoadSession(ctx context.Context, home string, f SessionFile) (*model.Session, error)
}

var registry = map[model.Agent]Adapter{}

func Register(a Adapter) { registry[a.Agent()] = a }

func Get(a model.Agent) (Adapter, bool) {
	ad, ok := registry[a]
	return ad, ok
}

func All() []Adapter {
	out := []Adapter{}
	for _, k := range []model.Agent{model.Claude, model.Codex, model.Gemini, model.OpenCode} {
		if a, ok := registry[k]; ok {
			out = append(out, a)
		}
	}
	return out
}
