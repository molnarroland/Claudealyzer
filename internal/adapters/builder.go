package adapters

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"claudealyzer/internal/model"
	"claudealyzer/internal/pricing"
)

// Payload limits: tool inputs/outputs are cut in the session payload and
// fetched in full only by the step sheet.
const (
	MaxInput  = 4000
	MaxOutput = 6000
	maxLine   = 64 << 20
	maxFull   = 2 << 20
)

// Builder assembles turns and steps from a stream of events.
type Builder struct {
	Turns   []model.Turn
	Calls   []model.Call
	byTool  map[string]toolRef
	// Full outputs of steps whose payload output was truncated.
	Full map[string]string
	stepSeq int
}

type toolRef struct {
	turn int
	step string
}

func NewBuilder() *Builder { return &Builder{byTool: map[string]toolRef{}, Full: map[string]string{}} }

func (b *Builder) cur(ts int64) *model.Turn {
	if len(b.Turns) == 0 {
		b.StartTurn("(session start)", ts)
	}
	return &b.Turns[len(b.Turns)-1]
}

func (b *Builder) StartTurn(prompt string, ts int64) {
	b.Turns = append(b.Turns, model.Turn{Index: len(b.Turns), Prompt: prompt, StartedAt: ts})
}

func (b *Builder) HasTurn() bool { return len(b.Turns) > 0 }

// AddStep appends a step to the current turn; toolID links a later result.
func (b *Builder) AddStep(s model.Step, toolID string) {
	t := b.cur(s.StartedAt)
	b.stepSeq++
	if s.ID == "" {
		s.ID = fmt.Sprintf("s%d", b.stepSeq)
	}
	s.Index = len(t.Steps)
	t.Steps = append(t.Steps, s)
	if toolID != "" {
		b.byTool[toolID] = toolRef{len(b.Turns) - 1, s.ID}
	}
}

// Step returns the step a tool id points at.
func (b *Builder) Step(toolID string) *model.Step {
	ref, ok := b.byTool[toolID]
	if !ok {
		return nil
	}
	steps := b.Turns[ref.turn].Steps
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].ID == ref.step {
			return &steps[i]
		}
	}
	return nil
}

func (b *Builder) TurnOf(toolID string) int {
	if ref, ok := b.byTool[toolID]; ok {
		return ref.turn
	}
	return -1
}

// Result attaches a tool result to its step.
func (b *Builder) Result(toolID, output string, isErr bool, ts int64) *model.Step {
	s := b.Step(toolID)
	if s == nil {
		return nil
	}
	s.ResultTokens = EstTokens(output)
	s.Output, s.OutputFull = Cut(output, MaxOutput)
	if !s.OutputFull {
		if len(output) > maxFull {
			output = output[:maxFull] + "\n… (cut at 2 MB)"
		}
		b.Full[s.ID] = output
	}
	s.IsError = s.IsError || isErr
	if ts > 0 && s.StartedAt > 0 && ts >= s.StartedAt && s.DurationMs == 0 {
		s.DurationMs = ts - s.StartedAt
	}
	return s
}

func (b *Builder) MarkCompacted() {
	if len(b.Turns) > 0 {
		b.Turns[len(b.Turns)-1].Compacted = true
	}
}

// AddCall records one API response (already de-duplicated) on the current turn.
func (b *Builder) AddCall(modelID string, ts int64, u model.Usage) {
	b.cur(ts)
	b.AddCallAt(len(b.Turns)-1, modelID, ts, u)
}

// AddCallAt records an API response on a given turn.
func (b *Builder) AddCallAt(turn int, modelID string, ts int64, u model.Usage) {
	if turn < 0 || turn >= len(b.Turns) {
		b.cur(ts)
		turn = len(b.Turns) - 1
	}
	t := &b.Turns[turn]
	u.Calls = 1
	pricing.Apply(modelID, ts, &u)
	b.Calls = append(b.Calls, model.Call{Model: modelID, Time: ts, Turn: turn, Usage: u})
	t.Usage.Add(u)
	if c := u.Context(); c > t.PeakContext {
		t.PeakContext = c
	}
	if modelID != "" && !contains(t.Models, modelID) {
		t.Models = append(t.Models, modelID)
	}
}

// Finish computes answers and durations. lastTs is the last record time.
func (b *Builder) Finish(endOf func(turn int) int64) {
	for i := range b.Turns {
		t := &b.Turns[i]
		// The last text step is the answer; earlier ones stay as notes.
		for j := len(t.Steps) - 1; j >= 0; j-- {
			if t.Steps[j].Kind == model.StepText {
				t.Answer = t.Steps[j].Input
				t.Steps = append(t.Steps[:j], t.Steps[j+1:]...)
				break
			}
		}
		for j := range t.Steps {
			t.Steps[j].Index = j
		}
		if t.DurationMs == 0 {
			end := int64(0)
			if endOf != nil {
				end = endOf(i)
			}
			for _, s := range t.Steps {
				if e := s.StartedAt + s.DurationMs; e > end {
					end = e
				}
			}
			if end > t.StartedAt && t.StartedAt > 0 {
				t.DurationMs = end - t.StartedAt
			}
		}
	}
}

// ---------- helpers ----------

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func EstTokens(s string) int64 { return int64(len(s)+3) / 4 }

// Cut truncates s to n bytes on a rune boundary; full reports whether nothing was cut.
func Cut(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, true
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…", false
}

// Tail keeps the last n bytes.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return "…\n" + s[i:]
}

func OneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r, _ := Cut(s, n)
	return strings.TrimSuffix(r, "\n…")
}

func ParseTime(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// Lines streams a JSONL reader with a large line limit.
func Lines(r io.Reader, fn func(line []byte)) error {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			buf := append([]byte(nil), line...)
			for err == bufio.ErrBufferFull {
				line, err = br.ReadSlice('\n')
				buf = append(buf, line...)
				if len(buf) > maxLine {
					for err == bufio.ErrBufferFull {
						_, err = br.ReadSlice('\n')
					}
					buf = nil
					break
				}
			}
			if buf != nil {
				fn(buf)
			}
		} else if len(line) > 0 {
			fn(line)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func PrettyJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// ---------- tool classification (shared so insight rules work for every agent) ----------

var (
	reTest  = regexp.MustCompile(`(?i)(\btest\b|\btests\b|vitest|jest|pytest|go test|phpunit|\bpest\b|karma|cypress|playwright test|\bspec\b|cargo test|npm t\b|test:)`)
	reBuild = regexp.MustCompile(`(?i)(\bbuild\b|\btsc\b|webpack|ng build|vite build|cargo build|go build|gradle|mvn |\bmake\b|compile|run-many)`)
	reHeavy = regexp.MustCompile(`(?i)(docker|playwright|webpack|esbuild|rollup|ffmpeg|generate|codegen|@spartan-ng/cli|storybook|lighthouse|--all\b|bundle)`)
)

func CommandCategory(cmd string) string {
	switch {
	case strings.Contains(strings.ToLower(cmd), "playwright"):
		return "heavy"
	case reTest.MatchString(cmd):
		return "test"
	case reBuild.MatchString(cmd):
		return "build"
	case reHeavy.MatchString(cmd):
		return "heavy"
	}
	return ""
}

// NormTool maps every agent's tool names onto shared kinds.
func NormTool(name string) model.StepKind {
	n := strings.ToLower(name)
	switch {
	case n == "bash" || n == "shell" || n == "exec" || n == "exec_command" || n == "run_shell_command" || n == "local_shell" || n == "bashoutput" || n == "powershell":
		return model.StepShell
	case n == "read" || n == "read_file" || n == "read_many_files" || n == "notebookread" || n == "view_image":
		return model.StepRead
	case n == "edit" || n == "write" || n == "multiedit" || n == "notebookedit" || n == "apply_patch" || n == "write_file" || n == "replace" || n == "edit_file":
		return model.StepEdit
	case n == "grep" || n == "glob" || n == "ls" || n == "search_file_content" || n == "list_directory" || n == "toolsearch" || n == "websearch" || n == "web_search" || n == "google_web_search":
		return model.StepSearch
	case n == "agent" || n == "task":
		return model.StepAgent
	}
	return model.StepTool
}

// Describe builds a step's title, file list and category from a tool's input.
func Describe(s *model.Step, input map[string]any) {
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := input[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch s.Kind {
	case model.StepShell:
		cmd := str("command", "cmd")
		if cmd == "" {
			if a, ok := input["command"].([]any); ok {
				parts := []string{}
				for _, p := range a {
					parts = append(parts, fmt.Sprint(p))
				}
				cmd = strings.Join(parts, " ")
			}
		}
		s.Title = OneLine(cmd, 300)
		s.Category = CommandCategory(cmd)
	case model.StepRead:
		p := str("file_path", "path", "absolute_path")
		if p == "" {
			if a, ok := input["paths"].([]any); ok {
				for _, x := range a {
					s.Files = append(s.Files, fmt.Sprint(x))
				}
				s.Title = fmt.Sprintf("%s  ×%d files", s.Tool, len(a))
				return
			}
		}
		if p != "" {
			s.Files = []string{p}
		}
		s.Title = s.Tool + "  " + p
	case model.StepEdit:
		p := str("file_path", "path", "absolute_path")
		if p != "" {
			s.Files = []string{p}
		}
		s.Title = s.Tool + "  " + p
	case model.StepSearch:
		s.Title = s.Tool + "  " + OneLine(str("pattern", "query", "path", "dir_path"), 200)
		if p := str("path"); p != "" && s.Tool != "LS" {
			s.Title += "  in " + filepath.Base(p)
		}
	case model.StepAgent:
		s.Title = "Agent  " + str("description", "prompt")
	default:
		b, _ := json.Marshal(input)
		s.Title = s.Tool + "  " + OneLine(string(b), 200)
	}
}

func SortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
