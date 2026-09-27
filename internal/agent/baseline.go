package agent

import (
	"context"
	"fmt"
	"time"

	"sqlagent/internal/exec"
	"sqlagent/internal/llm"
)

// RunBaseline is the naive-prompting mode: a single LLM call with no tools,
// no guard, and no repair loop. The candidate is still judged by the same
// differential execution oracle as the agentic loop, so pass/fail stays
// comparable across modes.
func (a *Agent) RunBaseline(ctx context.Context, st *State) (*RunResult, error) {
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
	st.Status = StatusActing
	st.Trace = nil

	caseID := a.caseID
	if st.CaseID != "" {
		caseID = st.CaseID
	}
	path := tracePath(a.tracesDir, caseID, time.Now())
	defer func() { _ = writeTrace(path, st.Trace) }()

	start := time.Now()
	resp, err := a.llm.Chat(ctx, []llm.Message{
		{Role: "system", Content: baselineSystemMessage()},
		{Role: "user", Content: baselineUserMessage(st)},
	}, nil)
	if err != nil {
		var usage llm.Usage
		if resp != nil {
			usage = resp.Usage
		}
		st.Trace = append(st.Trace, Step{
			Node:             NodeAct,
			Status:           st.Status,
			Attempt:          st.Attempt,
			LatencyMs:        time.Since(start).Milliseconds(),
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			Detail:           "baseline LLM call failed",
		})
		return resultOf(st, path), err
	}

	st.Candidate = extractSQL(resp.Content)
	st.Trace = append(st.Trace, Step{
		Node:             NodeAct,
		Status:           st.Status,
		Attempt:          st.Attempt,
		LatencyMs:        time.Since(start).Milliseconds(),
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		Candidate:        st.Candidate,
		Detail:           "baseline: single LLM call, no tools, no guard, no repair",
	})

	if st.Candidate == "" {
		st.Status = StatusExhausted
		st.FailureClass = FailureExhausted
		return resultOf(st, path), nil
	}

	// Same oracle as verifyNode: differential execution, never the expected
	// translation text.
	if st.Status != StatusGreen {
		st.Status = StatusVerifying
	}
	vstart := time.Now()
	vstep := Step{Node: NodeVerify, Status: st.Status, Attempt: st.Attempt}

	if a.source == nil || a.target == nil {
		st.Status = StatusExhausted
		st.FailureClass = FailureExecError
		vstep.FailureClass = st.FailureClass
		vstep.Status = st.Status
		vstep.Detail = "verification requires source and target executors"
		st.Trace = append(st.Trace, vstep)
		return resultOf(st, path), nil
	}

	srcRes, err := a.source.Run(ctx, st.SourceSQL)
	if err != nil {
		st.Status = StatusExhausted
		st.FailureClass = FailureExecError
		vstep.FailureClass = st.FailureClass
		vstep.Status = st.Status
		vstep.Detail = "source engine error: " + err.Error()
		vstep.LatencyMs = time.Since(vstart).Milliseconds()
		st.Trace = append(st.Trace, vstep)
		return resultOf(st, path), nil
	}
	tgtRes, err := a.target.Run(ctx, st.Candidate)
	if err != nil {
		st.Status = StatusExhausted
		st.FailureClass = FailureExecError
		vstep.FailureClass = st.FailureClass
		vstep.Status = st.Status
		vstep.Detail = "target engine error: " + err.Error()
		vstep.LatencyMs = time.Since(vstart).Milliseconds()
		st.Trace = append(st.Trace, vstep)
		return resultOf(st, path), nil
	}

	report := exec.Diff(srcRes, tgtRes)
	vstep.Detail = report.String()
	vstep.LatencyMs = time.Since(vstart).Milliseconds()

	if report.Equal {
		st.Status = StatusGreen
		st.FailureClass = ""
		vstep.Status = st.Status
		st.Trace = append(st.Trace, vstep)
		return resultOf(st, path), nil
	}

	st.Status = StatusExhausted
	st.FailureClass = FailureResultMismatch
	vstep.FailureClass = st.FailureClass
	vstep.Status = st.Status
	st.Trace = append(st.Trace, vstep)
	return resultOf(st, path), nil
}

func baselineSystemMessage() string {
	return `You are a SQL translation assistant. Convert the given source-dialect
SELECT statement into a single Snowflake SQL statement that returns the same
result set. Respond with ONLY the SQL, no explanations.`
}

func baselineUserMessage(st *State) string {
	return fmt.Sprintf(
		"Translate this %s SELECT to Snowflake SQL.\n\nSource SQL:\n%s",
		st.SourceDialect, st.SourceSQL)
}
