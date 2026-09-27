package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sqlagent/internal/exec"
	"sqlagent/internal/llm"
	"sqlagent/internal/schema"
	"sqlagent/internal/tools"
)

// Every test in this package runs against httptest servers bound to
// 127.0.0.1 and an embedded DuckDB; no external network access is made.

// testSchema mirrors the TPCH-lite seed (db/seed.sql).
func testSchema() *schema.Schema {
	return &schema.Schema{Tables: []schema.Table{
		{Name: "region", Columns: cols(
			"r_regionkey", "integer", "r_name", "character varying", "r_comment", "character varying")},
		{Name: "nation", Columns: cols(
			"n_nationkey", "integer", "n_name", "character varying", "n_regionkey", "integer", "n_comment", "character varying")},
		{Name: "customer", Columns: cols(
			"c_custkey", "integer", "c_name", "character varying", "c_address", "character varying",
			"c_nationkey", "integer", "c_phone", "character varying", "c_acctbal", "numeric",
			"c_mktsegment", "character varying", "c_comment", "character varying")},
		{Name: "orders", Columns: cols(
			"o_orderkey", "integer", "o_custkey", "integer", "o_orderstatus", "character varying",
			"o_totalprice", "numeric", "o_orderdate", "date", "o_orderpriority", "character varying",
			"o_clerk", "character varying", "o_shippriority", "integer", "o_comment", "character varying")},
		{Name: "lineitem", Columns: cols(
			"l_orderkey", "integer", "l_partkey", "integer", "l_suppkey", "integer", "l_linenumber", "integer",
			"l_quantity", "numeric", "l_extendedprice", "numeric", "l_discount", "numeric", "l_tax", "numeric",
			"l_returnflag", "character varying", "l_linestatus", "character varying", "l_shipdate", "date",
			"l_commitdate", "date", "l_receiptdate", "date", "l_shipinstruct", "character varying",
			"l_shipmode", "character varying", "l_comment", "character varying")},
	}}
}

func cols(pairs ...string) []schema.Column {
	out := make([]schema.Column, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, schema.Column{Name: pairs[i], Type: pairs[i+1]})
	}
	return out
}

// seededDuckDB is a process-wide seeded in-memory DuckDB shared by tests.
var (
	duckDBOnce sync.Once
	duckDBExec *exec.DuckDBExecutor
	duckDBErr  error
)

func seededDuckDB(t *testing.T) *exec.DuckDBExecutor {
	t.Helper()
	duckDBOnce.Do(func() {
		duckDBExec, duckDBErr = exec.NewDuckDBExecutor("")
		if duckDBErr != nil {
			return
		}
		script, err := os.ReadFile(filepath.Join("..", "..", "db", "seed_duckdb.sql"))
		if err != nil {
			duckDBErr = err
			return
		}
		duckDBErr = duckDBExec.LoadSQL(string(script))
	})
	if duckDBErr != nil {
		t.Fatalf("seed duckdb: %v", duckDBErr)
	}
	return duckDBExec
}

// --- scripted fake LLM server ------------------------------------------------

// fakeResp mirrors the wire shape the llm client decodes.
type fakeResp struct {
	Choices []struct {
		Message struct {
			Role      string         `json:"role"`
			Content   string         `json:"content"`
			ToolCalls []fakeToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type fakeToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type fakeRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role       string         `json:"role"`
		Content    string         `json:"content"`
		ToolCallID string         `json:"tool_call_id"`
		ToolCalls  []fakeToolCall `json:"tool_calls"`
	} `json:"messages"`
	Tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
	Temperature float64 `json:"temperature"`
}

// fakeLLM is a scripted responder: requests without tools get the plan
// response (or noToolsResp when set); requests with tools consume
// actResponses in order (repeating the last one when the script runs out).
type fakeLLM struct {
	mu           sync.Mutex
	requests     []fakeRequest
	actResponses []fakeResp
	noToolsResp  *fakeResp
	planCalls    int
	actCalls     int
}

func (f *fakeLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req fakeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	f.mu.Lock()
	f.requests = append(f.requests, req)
	var idx int
	if len(req.Tools) == 0 {
		f.planCalls++
	} else {
		idx = f.actCalls
		f.actCalls++
	}
	script := f.actResponses
	noTools := f.noToolsResp
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if len(req.Tools) == 0 {
		resp := planFakeResp
		if noTools != nil {
			resp = *noTools
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	var resp fakeResp
	if idx < len(script) {
		resp = script[idx]
	} else {
		resp = script[len(script)-1]
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *fakeLLM) snapshot() (reqs []fakeRequest, planCalls, actCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeRequest(nil), f.requests...), f.planCalls, f.actCalls
}

func sqlResp(sql string) fakeResp {
	r := fakeResp{}
	r.Choices = append(r.Choices, struct {
		Message struct {
			Role      string         `json:"role"`
			Content   string         `json:"content"`
			ToolCalls []fakeToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}{FinishReason: "stop"})
	r.Choices[0].Message.Role = "assistant"
	r.Choices[0].Message.Content = sql
	r.Usage.PromptTokens = 30
	r.Usage.CompletionTokens = 10
	return r
}

// planFakeResp is the scripted answer for the agent's tool-less plan call.
var planFakeResp = func() fakeResp {
	r := sqlResp(`{"constructs_to_translate":["TOP"],"tables_needed":["orders"]}`)
	r.Usage.PromptTokens = 100
	r.Usage.CompletionTokens = 20
	return r
}()

func toolCallResp(name, args string) fakeResp {
	r := fakeResp{}
	r.Choices = append(r.Choices, struct {
		Message struct {
			Role      string         `json:"role"`
			Content   string         `json:"content"`
			ToolCalls []fakeToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}{FinishReason: "tool_calls"})
	r.Choices[0].Message.Role = "assistant"
	r.Choices[0].Message.ToolCalls = []fakeToolCall{{ID: "call_1"}}
	r.Choices[0].Message.ToolCalls[0].Function.Name = name
	r.Choices[0].Message.ToolCalls[0].Function.Arguments = args
	r.Usage.PromptTokens = 30
	r.Usage.CompletionTokens = 10
	return r
}

func testRegistry(sch *schema.Schema) *tools.Registry {
	dialectRef, err := tools.NewDialectRef()
	if err != nil {
		panic(err)
	}
	return tools.NewRegistry(&tools.SchemaLookup{Schema: sch}, dialectRef)
}

func newAgentForTest(t *testing.T, srv *httptest.Server, reg *tools.Registry, maxAttempts int, tracesDir string) *Agent {
	t.Helper()
	client := llm.New(llm.Config{
		BaseURL:        srv.URL,
		Model:          "test-model",
		MaxRetries:     0,
		RetryBaseDelay: time.Millisecond,
	})
	dk := seededDuckDB(t)
	return New(client, reg,
		WithExecutors(dk, dk),
		WithTraceDir(tracesDir),
		WithCaseID("case-1"),
		WithMaxAttempts(maxAttempts))
}

func readTraceSteps(t *testing.T, path string) []Step {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace: %v", err)
	}
	defer func() { _ = f.Close() }()
	var steps []Step
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Step
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatalf("decode trace line %q: %v", sc.Text(), err)
		}
		steps = append(steps, s)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return steps
}

// --- tests --------------------------------------------------------------------

// TestAgentDefaultMaxAttemptsAllowsRepair: without WithMaxAttempts the agent
// must still use its default budget (4) and repair, not exhaust on the first
// failure (regression: a 0 max-attempts plumbing bug made the loop
// single-shot).
func TestAgentDefaultMaxAttemptsAllowsRepair(t *testing.T) {
	fake := &fakeLLM{actResponses: []fakeResp{
		sqlResp("SELECT o_orderkey, o_bogus FROM orders"),
		sqlResp("SELECT count(*) FROM orders"),
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	tracesDir := t.TempDir()
	client := llm.New(llm.Config{
		BaseURL:        srv.URL,
		Model:          "test-model",
		MaxRetries:     0,
		RetryBaseDelay: time.Millisecond,
	})
	a := New(client, testRegistry(testSchema()),
		WithExecutors(seededDuckDB(t), seededDuckDB(t)),
		WithTraceDir(tracesDir),
		WithCaseID("case-default-attempts"))

	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "tsql",
		Schema:        *testSchema(),
	}
	res, err := a.Run(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusGreen || st.Attempt != 2 {
		t.Fatalf("status=%q attempts=%d, want green in 2 attempts", st.Status, st.Attempt)
	}
	if st.MaxAttempts != 4 {
		t.Fatalf("default max attempts = %d, want 4", st.MaxAttempts)
	}
	_ = res
}

// TestAgentReachesGreenInTwoAttempts drives the full loop against a scripted
// responder: attempt 1 emits a hallucinated column (guard rejects it without
// executing), attempt 2 emits valid SQL (verify diffs clean). The run must
// reach green in exactly 2 attempts and the trace must contain both
// candidates.
func TestAgentReachesGreenInTwoAttempts(t *testing.T) {
	fake := &fakeLLM{actResponses: []fakeResp{
		sqlResp("```sql\nSELECT o_orderkey, o_bogus FROM orders\n```"),
		sqlResp("SELECT count(*) FROM orders"),
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	tracesDir := t.TempDir()
	a := newAgentForTest(t, srv, testRegistry(testSchema()), 4, tracesDir)

	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "tsql",
		Schema:        *testSchema(),
	}
	res, err := a.Run(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}

	if st.Status != StatusGreen {
		t.Fatalf("status = %q, want green", st.Status)
	}
	if st.Attempt != 2 {
		t.Fatalf("attempts = %d, want exactly 2", st.Attempt)
	}
	if st.Candidate != "SELECT count(*) FROM orders" {
		t.Fatalf("candidate = %q", st.Candidate)
	}

	_, planCalls, actCalls := fake.snapshot()
	if planCalls != 1 {
		t.Fatalf("plan calls = %d, want 1", planCalls)
	}
	if actCalls != 2 {
		t.Fatalf("act calls = %d, want 2", actCalls)
	}

	data, err := os.ReadFile(res.TracePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "o_bogus") {
		t.Fatal("trace missing attempt-1 candidate with hallucinated column")
	}
	if !strings.Contains(string(data), "count(*) FROM orders") {
		t.Fatal("trace missing attempt-2 candidate")
	}

	steps := readTraceSteps(t, res.TracePath)
	var actSteps []Step
	for _, s := range steps {
		if s.Node == NodeAct {
			actSteps = append(actSteps, s)
		}
	}
	if len(actSteps) != 2 {
		t.Fatalf("act steps = %d, want 2", len(actSteps))
	}
	if !strings.Contains(actSteps[0].Candidate, "o_bogus") {
		t.Fatalf("act step 1 candidate = %q", actSteps[0].Candidate)
	}
	if actSteps[1].Candidate != "SELECT count(*) FROM orders" {
		t.Fatalf("act step 2 candidate = %q", actSteps[1].Candidate)
	}
	// Token accounting must be present on LLM-backed steps.
	if actSteps[0].PromptTokens == 0 || actSteps[0].CompletionTokens == 0 {
		t.Fatalf("token counts missing on act step: %+v", actSteps[0])
	}
	if res.PromptTokens == 0 || res.CompletionTokens == 0 {
		t.Fatalf("run token totals missing: %+v", res)
	}
}

// TestAgentExhaustedMaxAttempts pins the repair loop: a model that keeps
// hallucinating must exhaust at MaxAttempts without ever executing.
func TestAgentExhaustedMaxAttempts(t *testing.T) {
	fake := &fakeLLM{actResponses: []fakeResp{
		sqlResp("SELECT o_orderkey, o_bogus FROM orders"),
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	a := newAgentForTest(t, srv, testRegistry(testSchema()), 2, t.TempDir())

	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "tsql",
		Schema:        *testSchema(),
	}
	res, err := a.Run(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusExhausted {
		t.Fatalf("status = %q, want exhausted", st.Status)
	}
	if st.Attempt != 2 {
		t.Fatalf("attempts = %d, want 2", st.Attempt)
	}

	steps := readTraceSteps(t, res.TracePath)
	var actCount, verifyCount int
	for _, s := range steps {
		switch s.Node {
		case NodeAct:
			actCount++
		case NodeVerify:
			verifyCount++
		}
	}
	if actCount != 2 {
		t.Fatalf("act steps = %d, want 2", actCount)
	}
	if verifyCount != 0 {
		t.Fatalf("guard violations must not reach verify; got %d verify steps", verifyCount)
	}
	last := steps[len(steps)-1]
	if last.Status != StatusExhausted {
		t.Fatalf("last step status = %q, want exhausted", last.Status)
	}
}

// TestAgentToolCallRound exercises the actNode tool loop: the model asks for
// dialect_ref first, the tool result is appended to the history verbatim, and
// the follow-up produces valid SQL.
func TestAgentToolCallRound(t *testing.T) {
	fake := &fakeLLM{actResponses: []fakeResp{
		toolCallResp("dialect_ref", `{"construct":"NVL","source_dialect":"oracle"}`),
		sqlResp("SELECT count(*) FROM orders"),
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	a := newAgentForTest(t, srv, testRegistry(testSchema()), 4, t.TempDir())

	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "oracle",
		Schema:        *testSchema(),
	}
	res, err := a.Run(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusGreen {
		t.Fatalf("status = %q, want green; trace=%s", st.Status, res.TracePath)
	}

	steps := readTraceSteps(t, res.TracePath)
	var found bool
	for _, s := range steps {
		if s.Node != NodeAct || len(s.ToolCalls) == 0 {
			continue
		}
		found = true
		tc := s.ToolCalls[0]
		if tc.Name != "dialect_ref" {
			t.Fatalf("tool call name = %q", tc.Name)
		}
		if !strings.Contains(tc.Result, "COALESCE") {
			t.Fatalf("tool result missing COALESCE: %q", tc.Result)
		}
	}
	if !found {
		t.Fatal("no act step with a tool call recorded")
	}

	// The tool result must appear verbatim in the follow-up request history.
	reqs, _, _ := fake.snapshot()
	if len(reqs) < 2 {
		t.Fatalf("requests = %d, want >= 2", len(reqs))
	}
	var toolMsgFound bool
	for _, m := range reqs[len(reqs)-1].Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "COALESCE") {
			toolMsgFound = true
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			if m.ToolCalls[0].Function.Name != "dialect_ref" {
				t.Fatalf("history tool call = %+v", m.ToolCalls[0])
			}
		}
	}
	if !toolMsgFound {
		t.Fatal("tool result not found in follow-up message history")
	}
}

// TestAgentVerifyDiffFailureRoutesToRepair: a candidate that executes but
// returns wrong results must route through repair with diff feedback, and a
// scripted correction reaches green.
func TestAgentVerifyDiffFailureRoutesToRepair(t *testing.T) {
	fake := &fakeLLM{actResponses: []fakeResp{
		sqlResp("SELECT count(*) FROM region"), // wrong table: 5 rows vs 200
		sqlResp("SELECT count(*) FROM orders"),
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	a := newAgentForTest(t, srv, testRegistry(testSchema()), 4, t.TempDir())

	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "tsql",
		Schema:        *testSchema(),
	}
	res, err := a.Run(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusGreen || st.Attempt != 2 {
		t.Fatalf("status=%q attempts=%d, want green in 2", st.Status, st.Attempt)
	}

	steps := readTraceSteps(t, res.TracePath)
	var repairDetail string
	for _, s := range steps {
		if s.Node == NodeRepair {
			repairDetail = s.Detail
		}
	}
	if !strings.Contains(repairDetail, "i:200") || !strings.Contains(repairDetail, "i:5") {
		t.Fatalf("repair feedback missing diff values: %q", repairDetail)
	}
}
