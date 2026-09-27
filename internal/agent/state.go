// Package agent implements a hand-rolled LangGraph-style state machine that
// translates a source-dialect SELECT into Snowflake SQL and verifies the
// result by differential execution.
//
//	plan -> act -> guard -> verify -> (green | repair -> act)
//
// Every step is recorded in State.Trace and persisted as JSONL under
// traces/<case-id>/<timestamp>.jsonl so a run can be inspected or resumed.
package agent

import (
	"encoding/json"

	"sqlagent/internal/schema"
)

// Status values carried in State.Status and Step.Status.
const (
	StatusPlanning  = "planning"
	StatusActing    = "acting"
	StatusVerifying = "verifying"
	StatusRepairing = "repairing"
	StatusGreen     = "green"
	StatusExhausted = "exhausted"
)

// Node names.
const (
	NodePlan   = "plan"
	NodeAct    = "act"
	NodeGuard  = "guard"
	NodeVerify = "verify"
	NodeRepair = "repair"
)

// FailureClass values recorded on failed runs.
const (
	FailureGuardViolation = "guard_violation"
	FailureExecError      = "exec_error"
	FailureResultMismatch = "result_mismatch"
	FailureExhausted      = "exhausted"
	FailureLLMError       = "llm_error"
)

// State is the full mutable state of one conversion run.
type State struct {
	SourceSQL     string
	SourceDialect string
	Schema        schema.Schema
	Plan          string
	Candidate     string
	Attempt       int
	MaxAttempts   int // default 4, configurable
	Trace         []Step
	Status        string // planning|acting|verifying|repairing|green|exhausted
	// FailureClass classifies a failed run for the evaluation harness:
	// guard_violation | exec_error | result_mismatch | exhausted | llm_error.
	FailureClass string
	// CaseID overrides the agent's case id for the trace path of this run.
	CaseID string
}

// Step is one recorded node execution; it is the unit of the JSONL trace.
type Step struct {
	Node             string           `json:"node"`
	Status           string           `json:"status"`
	Attempt          int              `json:"attempt"`
	LatencyMs        int64            `json:"latency_ms"`
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	Violations       int              `json:"violations,omitempty"`
	FailureClass     string           `json:"failure_class,omitempty"`
	ToolCalls        []ToolCallRecord `json:"tool_calls,omitempty"`
	Candidate        string           `json:"candidate,omitempty"`
	Detail           string           `json:"detail,omitempty"`
}

// ToolCallRecord captures one tool invocation made during a step, including
// the raw arguments and the result fed back to the model.
type ToolCallRecord struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Result    string          `json:"result,omitempty"`
}

// RunResult is what Run returns alongside the (mutated) State.
type RunResult struct {
	State            *State
	TracePath        string
	PromptTokens     int
	CompletionTokens int
}
