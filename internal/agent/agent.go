package agent

import (
	"context"
	"fmt"
	"time"

	"sqlagent/internal/exec"
	"sqlagent/internal/llm"
	"sqlagent/internal/tools"
)

// maxNodeSteps bounds the state machine against malformed routing.
const maxNodeSteps = 100

// Agent runs the translation state machine: plan -> act -> guard -> verify,
// with repair looping back into act until the run goes green or exhausts
// its attempts.
type Agent struct {
	llm         *llm.Client
	tools       *tools.Registry
	source      exec.Executor
	target      exec.Executor
	maxAttempts int
	tracesDir   string
	caseID      string
}

// Option configures an Agent.
type Option func(*Agent)

// New creates an Agent with the default options: 4 max attempts, traces
// under ./traces, case id "default".
func New(client *llm.Client, reg *tools.Registry, opts ...Option) *Agent {
	a := &Agent{
		llm:         client,
		tools:       reg,
		maxAttempts: 4,
		tracesDir:   "traces",
		caseID:      "default",
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// WithExecutors sets the source (original dialect) and target (Snowflake
// stand-in) engines used by verifyNode.
func WithExecutors(source, target exec.Executor) Option {
	return func(a *Agent) { a.source, a.target = source, target }
}

// WithMaxAttempts overrides the default of 4.
func WithMaxAttempts(n int) Option {
	return func(a *Agent) { a.maxAttempts = n }
}

// WithTraceDir sets the traces root directory.
func WithTraceDir(dir string) Option {
	return func(a *Agent) { a.tracesDir = dir }
}

// WithCaseID sets the case directory name under the traces root.
func WithCaseID(id string) Option {
	return func(a *Agent) { a.caseID = id }
}

// Run executes the state machine against st, mutating st in place (Status,
// Candidate, Attempt, Trace). The trace is persisted as JSONL at
// traces/<case-id>/<timestamp>.jsonl; the returned RunResult carries the
// final state, the trace path, and the total token usage.
func (a *Agent) Run(ctx context.Context, st *State) (*RunResult, error) {
	if st == nil {
		return nil, fmt.Errorf("agent: nil state")
	}
	if a.llm == nil {
		return nil, fmt.Errorf("agent: no LLM client configured")
	}
	if st.MaxAttempts <= 0 {
		st.MaxAttempts = a.maxAttempts
	}
	if st.Attempt <= 0 {
		st.Attempt = 1
	}
	if st.Status == "" {
		st.Status = StatusPlanning
	}
	st.Trace = nil

	path := tracePath(a.tracesDir, a.caseID, time.Now())
	r := &run{a: a, state: st, path: path}

	// Persist whatever was recorded even when a node errors, so traces stay
	// complete as the resume artifact.
	defer func() { _ = writeTrace(path, st.Trace) }()

	node := NodePlan
	nodes := r.nodes()
	for steps := 0; node != ""; steps++ {
		if steps >= maxNodeSteps {
			return nil, fmt.Errorf("agent: did not terminate within %d node steps", maxNodeSteps)
		}
		fn, ok := nodes[node]
		if !ok {
			return nil, fmt.Errorf("agent: unknown node %q", node)
		}
		next, err := fn(ctx, st)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", node, err)
		}
		node = next
	}

	if err := writeTrace(path, st.Trace); err != nil {
		return nil, err
	}

	var promptTokens, completionTokens int
	for _, s := range st.Trace {
		promptTokens += s.PromptTokens
		completionTokens += s.CompletionTokens
	}
	return &RunResult{
		State:            st,
		TracePath:        path,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}, nil
}
