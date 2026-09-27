package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sqlagent/internal/exec"
	"sqlagent/internal/guard"
	"sqlagent/internal/llm"
	"sqlagent/internal/schema"
	"sqlagent/internal/tools"
)

// maxToolRounds caps how many chat rounds actNode will spend on tool calls
// before giving up on this candidate.
const maxToolRounds = 8

// maxDiffLines caps how many diff mismatches are fed back to the model.
const maxDiffLines = 5

// run carries the per-run scratch state shared by the nodes: the accumulated
// message history for actNode and the most recent failure feedback for
// repairNode. Nodes are methods on run, matching the required signature
// func(ctx, *State) (nextNode string, err error).
type run struct {
	a        *Agent
	state    *State
	path     string
	messages []llm.Message
	failure  string
}

func (r *run) record(s Step) {
	r.state.Trace = append(r.state.Trace, s)
}

func (r *run) nodes() map[string]func(context.Context, *State) (string, error) {
	return map[string]func(context.Context, *State) (string, error){
		NodePlan:   r.planNode,
		NodeAct:    r.actNode,
		NodeGuard:  r.guardNode,
		NodeVerify: r.verifyNode,
		NodeRepair: r.repairNode,
	}
}

// --- planNode ------------------------------------------------------------

// planNode asks the LLM for a short structured translation plan (JSON with
// constructs_to_translate and tables_needed) given the source SQL and the
// available tables. Plan failures abort the run; the plan itself is advisory.
func (r *run) planNode(ctx context.Context, st *State) (string, error) {
	st.Status = StatusPlanning
	start := time.Now()

	resp, err := r.a.llm.Chat(ctx, []llm.Message{
		{Role: "system", Content: "You are a SQL dialect translation planner. Respond with a single JSON object and nothing else."},
		{Role: "user", Content: planPrompt(st.SourceDialect, st.SourceSQL, tableNames(&st.Schema))},
	}, nil)
	if err != nil {
		return "", err
	}
	st.Plan = renderPlan(resp.Content)

	r.record(Step{
		Node:             NodePlan,
		Status:           st.Status,
		Attempt:          st.Attempt,
		LatencyMs:        time.Since(start).Milliseconds(),
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		Detail:           st.Plan,
	})
	return NodeAct, nil
}

// --- actNode ---------------------------------------------------------------

// actNode chats with the model, exposing the tool registry as OpenAI
// function-calling tools. It loops tool-call rounds (capped at maxToolRounds)
// until the model returns final target SQL, appending tool results to the
// message history verbatim.
func (r *run) actNode(ctx context.Context, st *State) (string, error) {
	st.Status = StatusActing
	start := time.Now()
	step := Step{Node: NodeAct, Status: st.Status, Attempt: st.Attempt}

	// The first act round carries the source SQL, plan, and schema tables.
	if len(r.messages) == 0 {
		r.messages = append(r.messages, systemMessage())
	}
	if len(r.messages) == 1 {
		r.messages = append(r.messages, userMessage(st))
	}

	for round := 0; round < maxToolRounds; round++ {
		resp, err := r.a.llm.Chat(ctx, r.messages, toolDefs(r.a.tools))
		if err != nil {
			return "", err
		}
		step.PromptTokens += resp.Usage.PromptTokens
		step.CompletionTokens += resp.Usage.CompletionTokens
		r.messages = append(r.messages, llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			candidate := extractSQL(resp.Content)
			if candidate == "" {
				r.failure = "model returned neither SQL nor tool calls"
				st.FailureClass = FailureExhausted
				step.FailureClass = st.FailureClass
				step.Detail = r.failure
				step.LatencyMs = time.Since(start).Milliseconds()
				r.record(step)
				return NodeRepair, nil
			}
			st.Candidate = candidate
			step.Candidate = candidate
			step.LatencyMs = time.Since(start).Milliseconds()
			r.record(step)
			return NodeGuard, nil
		}

		for _, tc := range resp.ToolCalls {
			rec := ToolCallRecord{
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			}
			result, terr := r.a.tools.Invoke(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			if terr != nil {
				result = "ERROR: " + terr.Error()
			}
			rec.Result = result
			step.ToolCalls = append(step.ToolCalls, rec)
			// Tool results are appended verbatim, exactly as the model will
			// see them on the next round.
			r.messages = append(r.messages, llm.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}

	r.failure = fmt.Sprintf("model did not produce final SQL within %d tool-call rounds", maxToolRounds)
	st.FailureClass = FailureExhausted
	step.FailureClass = st.FailureClass
	step.Detail = r.failure
	step.LatencyMs = time.Since(start).Milliseconds()
	r.record(step)
	return NodeRepair, nil
}

// --- guardNode --------------------------------------------------------------

// guardNode runs the groundedness guard. Any violation means the candidate
// is not executed; the run routes straight to repairNode with the violation
// and its suggestion as feedback. This is the cheap failure path.
func (r *run) guardNode(_ context.Context, st *State) (string, error) {
	st.Status = StatusVerifying
	start := time.Now()
	step := Step{Node: NodeGuard, Status: st.Status, Attempt: st.Attempt}

	viols, err := guard.CheckGrounded(st.Candidate, &st.Schema)
	step.Violations = len(viols)
	if err == nil && len(viols) == 0 {
		step.Detail = "candidate is grounded in the schema"
		step.LatencyMs = time.Since(start).Milliseconds()
		r.record(step)
		return NodeVerify, nil
	}

	if err != nil {
		r.failure = "guard could not parse candidate SQL: " + err.Error()
		st.FailureClass = FailureExecError
	} else {
		msgs := make([]string, len(viols))
		for i, v := range viols {
			msgs[i] = v.String()
		}
		r.failure = strings.Join(msgs, "; ")
		st.FailureClass = FailureGuardViolation
	}
	step.FailureClass = st.FailureClass
	step.Detail = r.failure
	step.LatencyMs = time.Since(start).Milliseconds()
	r.record(step)
	return NodeRepair, nil
}

// --- verifyNode --------------------------------------------------------------

// verifyNode differentially executes the source SQL on the source engine and
// the candidate on the target engine. A clean diff means green; an execution
// error or any mismatch routes to repairNode.
func (r *run) verifyNode(ctx context.Context, st *State) (string, error) {
	st.Status = StatusVerifying
	start := time.Now()
	step := Step{Node: NodeVerify, Status: st.Status, Attempt: st.Attempt}

	var failMsg, failClass string
	switch {
	case r.a.source == nil || r.a.target == nil:
		failMsg, failClass = "verification requires source and target executors", FailureExecError
	default:
		srcRes, err := r.a.source.Run(ctx, st.SourceSQL)
		if err != nil {
			failMsg, failClass = "source engine error: "+err.Error(), FailureExecError
			break
		}
		tgtRes, err := r.a.target.Run(ctx, st.Candidate)
		if err != nil {
			failMsg, failClass = "target engine error: "+err.Error(), FailureExecError
			break
		}
		report := exec.Diff(srcRes, tgtRes)
		step.Detail = report.String()
		if report.Equal {
			st.Status = StatusGreen
			st.FailureClass = ""
			step.Status = st.Status
			step.LatencyMs = time.Since(start).Milliseconds()
			r.record(step)
			return "", nil
		}
		failMsg, failClass = diffFailure(report), FailureResultMismatch
	}

	r.failure = failMsg
	st.FailureClass = failClass
	step.FailureClass = failClass
	step.Detail = failMsg
	step.LatencyMs = time.Since(start).Milliseconds()
	r.record(step)
	return NodeRepair, nil
}

// diffFailure reduces a DiffReport to the exact feedback the model needs:
// the first few mismatches, or the schema/row-count difference.
func diffFailure(report exec.DiffReport) string {
	if len(report.Mismatches) > 0 {
		n := min(maxDiffLines, len(report.Mismatches))
		return "diff mismatches: " + strings.Join(report.Mismatches[:n], " | ")
	}
	if report.SchemaDiff != "" {
		return "schema mismatch: " + report.SchemaDiff
	}
	return fmt.Sprintf("row count differs: left=%d right=%d", report.RowCountLeft, report.RowCountRight)
}

// --- repairNode ---------------------------------------------------------------

// repairNode either gives up (Attempt >= MaxAttempts -> exhausted) or builds
// a repair message holding the previous candidate and the exact failure,
// increments Attempt, and routes back to actNode.
func (r *run) repairNode(_ context.Context, st *State) (string, error) {
	if st.Attempt >= st.MaxAttempts {
		st.Status = StatusExhausted
		if st.FailureClass == "" {
			st.FailureClass = FailureExhausted
		}
		r.record(Step{
			Node:         NodeRepair,
			Status:       st.Status,
			Attempt:      st.Attempt,
			FailureClass: st.FailureClass,
			Detail:       fmt.Sprintf("max attempts (%d) reached; last failure: %s", st.MaxAttempts, r.failure),
		})
		return "", nil
	}

	st.Status = StatusRepairing
	st.Attempt++

	feedback := r.failure
	if feedback == "" {
		feedback = "unknown failure"
	}
	r.messages = append(r.messages, llm.Message{
		Role:    "user",
		Content: repairMessage(st.Candidate, feedback),
	})
	r.record(Step{
		Node:    NodeRepair,
		Status:  st.Status,
		Attempt: st.Attempt,
		Detail:  feedback,
	})
	return NodeAct, nil
}

// --- prompt and message helpers ------------------------------------------------

func systemMessage() llm.Message {
	return llm.Message{Role: "system", Content: `You are a SQL translation agent. Convert the source-dialect SELECT into Snowflake SQL.

Rules:
- The target SQL must return the same result set as the source SQL.
- Use the provided tools: check the schema with schema_lookup, resolve dialect
  differences with dialect_ref, validate candidates with dry_run, and verify
  equivalence with execute_and_diff.
- When you are done, respond with ONLY the final Snowflake SQL statement
  (you may wrap it in a markdown code block). Do not add explanations.`}
}

func userMessage(st *State) llm.Message {
	return llm.Message{Role: "user", Content: fmt.Sprintf(
		"Translate this %s SELECT to Snowflake SQL.\n\nPlan: %s\n\nSchema tables: %s\n\nSource SQL:\n```sql\n%s\n```",
		st.SourceDialect, st.Plan, tableNames(&st.Schema), st.SourceSQL)}
}

func repairMessage(candidate, feedback string) string {
	return fmt.Sprintf(
		"Your previous candidate SQL was rejected during verification.\n\nFailure: %s\n\nPrevious candidate:\n```sql\n%s\n```\n\nProduce a corrected Snowflake SQL now. Use tools if needed.",
		feedback, candidate)
}

func planPrompt(dialect, sql, tables string) string {
	return fmt.Sprintf(`Plan the translation of this %s SELECT into Snowflake SQL.

Available tables: %s

Source SQL:
%s

Respond with a single JSON object: {"constructs_to_translate": ["..."], "tables_needed": ["..."]}`, dialect, tables, sql)
}

// renderPlan turns the model's planning response into the short structured
// plan stored in State.Plan. Unparseable JSON is kept as raw text.
func renderPlan(content string) string {
	var p struct {
		Constructs []string `json:"constructs_to_translate"`
		Tables     []string `json:"tables_needed"`
	}
	if err := json.Unmarshal([]byte(extractJSON(content)), &p); err == nil {
		return fmt.Sprintf("constructs: %s; tables: %s",
			strings.Join(p.Constructs, ", "), strings.Join(p.Tables, ", "))
	}
	return strings.TrimSpace(content)
}

func tableNames(sch *schema.Schema) string {
	names := make([]string, 0, len(sch.Tables))
	for _, t := range sch.Tables {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func toolDefs(reg *tools.Registry) []llm.ToolDef {
	if reg == nil {
		return nil
	}
	var defs []llm.ToolDef
	for _, t := range reg.All() {
		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.JSONSchema(),
			},
		})
	}
	return defs
}

// extractSQL strips markdown code fences so a fenced response still yields a
// clean statement for the guard and the target engine.
func extractSQL(content string) string {
	return stripFences(content)
}

// extractJSON extracts the outermost JSON object from a model response.
func extractJSON(content string) string {
	content = stripFences(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return content
	}
	return content[start : end+1]
}

func stripFences(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		rest := strings.TrimPrefix(trimmed, "```")
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			rest = rest[i+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		return strings.TrimSpace(rest)
	}
	return trimmed
}
