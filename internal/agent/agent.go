package agent

import (
	"context"
	"fmt"
	"time"

	"sqlagent/internal/exec"
	"sqlagent/internal/llm"
	"sqlagent/internal/metrics"
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

// WithMaxAttempts overrides the default of 4. Values <= 0 keep the default.
func WithMaxAttempts(n int) Option {
	return func(a *Agent) {
		if n > 0 {
			a.maxAttempts = n
		}
	}
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
// Candidate, Attempt, FailureClass, Trace). The trace is persisted as JSONL
// at traces/<case-id>/<timestamp>.jsonl (st.CaseID overrides the agent's
// case id). On node failure a RunResult carrying the partial state and trace
// path is returned alongside the error so harnesses can classify the run.
func (a *Agent) Run(ctx context.Context, st *State) (res *RunResult, err error) {
	if st == nil {
		return nil, fmt.Errorf("agent: nil state")
	}
	defer func() {
		status := st.Status
		if err != nil || status == "" || status == StatusPlanning || status == StatusActing {
			status = "llm_error"
		}
		metrics.ConversionsTotal.WithLabelValues(status).Inc()
		if err == nil {
			metrics.ConversionAttempts.WithLabelValues(status).Observe(float64(st.Attempt))
		}
	}()
	if a.llm == nil {
		return nil, fmt.Errorf("agent: no LLM client configured")
	}
	if st.MaxAttempts <= 0 {
		st.MaxAttempts = a.maxAttempts
	}
	if st.MaxAttempts <= 0 {
		st.MaxAttempts = 4
	}
	if st.Attempt <= 0 {
		st.Attempt = 1
	}
	if st.Status == "" {
		st.Status = StatusPlanning
	}
	st.Trace = nil

	caseID := a.caseID
	if st.CaseID != "" {
		caseID = st.CaseID
	}
	path := tracePath(a.tracesDir, caseID, time.Now())
	r := &run{a: a, state: st, path: path}

	// Persist whatever was recorded even when a node errors, so traces stay
	// complete as the resume artifact.
	defer func() { _ = writeTrace(path, st.Trace) }()

	node := NodePlan
	nodes := r.nodes()
	for steps := 0; node != ""; steps++ {
		if steps >= maxNodeSteps {
			return resultOf(st, path), fmt.Errorf("agent: did not terminate within %d node steps", maxNodeSteps)
		}
		fn, ok := nodes[node]
		if !ok {
			return resultOf(st, path), fmt.Errorf("agent: unknown node %q", node)
		}
		next, err := fn(ctx, st)
		if err != nil {
			return resultOf(st, path), fmt.Errorf("node %s: %w", node, err)
		}
		node = next
	}

	if err := writeTrace(path, st.Trace); err != nil {
		return resultOf(st, path), err
	}
	return resultOf(st, path), nil
}

// resultOf builds a RunResult from a finished (or failed) state, summing
// token totals over the recorded trace.
func resultOf(st *State, path string) *RunResult {
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
	}
}
