package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sqlagent/internal/exec"
	"sqlagent/internal/llm"
)

// --- baseline mode -----------------------------------------------------------

func baselineAgent(t *testing.T, srv *httptest.Server, tracesDir string) *Agent {
	t.Helper()
	client := llm.New(llm.Config{
		BaseURL:        srv.URL,
		Model:          "test-model",
		MaxRetries:     0,
		RetryBaseDelay: time.Millisecond,
	})
	dk := seededDuckDB(t)
	return New(client, testRegistry(testSchema()),
		WithExecutors(dk, dk),
		WithTraceDir(tracesDir),
		WithCaseID("baseline-case"))
}

func TestRunBaselineGreen(t *testing.T) {
	noTools := sqlResp("SELECT count(*) FROM orders")
	fake := &fakeLLM{noToolsResp: &noTools}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	a := baselineAgent(t, srv, t.TempDir())
	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "tsql",
		Schema:        *testSchema(),
	}
	res, err := a.RunBaseline(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusGreen || st.Attempt != 1 {
		t.Fatalf("status=%q attempt=%d, want green/1", st.Status, st.Attempt)
	}
	if res.PromptTokens == 0 || res.CompletionTokens == 0 {
		t.Fatalf("token totals missing: %+v", res)
	}
	// Baseline makes exactly one LLM call: no tools were offered.
	reqs, planCalls, actCalls := fake.snapshot()
	if planCalls != 1 || actCalls != 0 {
		t.Fatalf("plan/act calls = %d/%d, want 1/0 (single tool-less call)", planCalls, actCalls)
	}
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	if len(reqs[0].Tools) != 0 {
		t.Fatal("baseline must not expose tools")
	}
}

func TestRunBaselineResultMismatch(t *testing.T) {
	noTools := sqlResp("SELECT count(*) FROM region") // wrong answer
	fake := &fakeLLM{noToolsResp: &noTools}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	a := baselineAgent(t, srv, t.TempDir())
	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "tsql",
		Schema:        *testSchema(),
	}
	if _, err := a.RunBaseline(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusExhausted || st.FailureClass != FailureResultMismatch {
		t.Fatalf("status=%q class=%q, want exhausted/result_mismatch", st.Status, st.FailureClass)
	}
}

func TestRunBaselineExecError(t *testing.T) {
	noTools := sqlResp("SELEC broken")
	fake := &fakeLLM{noToolsResp: &noTools}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	a := baselineAgent(t, srv, t.TempDir())
	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "tsql",
		Schema:        *testSchema(),
	}
	if _, err := a.RunBaseline(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusExhausted || st.FailureClass != FailureExecError {
		t.Fatalf("status=%q class=%q, want exhausted/exec_error", st.Status, st.FailureClass)
	}
}

func TestRunBaselineLLMError(t *testing.T) {
	srv := httptest.NewServer(httpStatusResponder(500))
	defer srv.Close()

	a := baselineAgent(t, srv, t.TempDir())
	st := &State{
		SourceSQL:     "SELECT count(*) FROM orders",
		SourceDialect: "tsql",
		Schema:        *testSchema(),
	}
	res, err := a.RunBaseline(context.Background(), st)
	if err == nil {
		t.Fatal("expected llm error")
	}
	if res == nil || res.TracePath == "" {
		t.Fatal("error path must still return a RunResult with a trace path")
	}
}

func httpStatusResponder(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}
}

// --- node edges ----------------------------------------------------------------

func TestActNodeEmptyResponseExhausts(t *testing.T) {
	fake := &fakeLLM{actResponses: []fakeResp{
		func() fakeResp {
			r := sqlResp("")
			r.Choices[0].Message.Content = ""
			return r
		}(),
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	client := llm.New(llm.Config{BaseURL: srv.URL, Model: "m", RetryBaseDelay: time.Millisecond})
	a := New(client, testRegistry(testSchema()),
		WithExecutors(seededDuckDB(t), seededDuckDB(t)),
		WithTraceDir(t.TempDir()),
		WithCaseID("empty-response"),
		WithMaxAttempts(1))

	st := &State{SourceSQL: "SELECT count(*) FROM orders", SourceDialect: "tsql", Schema: *testSchema()}
	if _, err := a.Run(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusExhausted || st.FailureClass != FailureExhausted {
		t.Fatalf("status=%q class=%q, want exhausted", st.Status, st.FailureClass)
	}
}

func TestRunLLMErrorReturnsResult(t *testing.T) {
	srv := httptest.NewServer(httpStatusResponder(500))
	defer srv.Close()

	client := llm.New(llm.Config{BaseURL: srv.URL, Model: "m", MaxRetries: 0, RetryBaseDelay: time.Millisecond})
	a := New(client, testRegistry(testSchema()),
		WithExecutors(seededDuckDB(t), seededDuckDB(t)),
		WithTraceDir(t.TempDir()),
		WithCaseID("llm-error"))

	st := &State{SourceSQL: "SELECT count(*) FROM orders", SourceDialect: "tsql", Schema: *testSchema()}
	res, err := a.Run(context.Background(), st)
	if err == nil {
		t.Fatal("expected llm error")
	}
	if res == nil || res.TracePath == "" {
		t.Fatal("error path must return RunResult with trace path")
	}
}

// --- pure helpers ----------------------------------------------------------------

func TestExtractSQLFences(t *testing.T) {
	cases := map[string]string{
		"SELECT 1":                 "SELECT 1",
		"```sql\nSELECT 1\n```":    "SELECT 1",
		"```\nSELECT 1\n```":       "SELECT 1",
		"  SELECT 1  ":             "SELECT 1",
		"```sql\nSELECT 1;\n```\n": "SELECT 1;",
	}
	for in, want := range cases {
		if got := extractSQL(in); got != want {
			t.Fatalf("extractSQL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderPlan(t *testing.T) {
	if got := renderPlan(`{"constructs_to_translate":["TOP"],"tables_needed":["orders"]}`); got != "constructs: TOP; tables: orders" {
		t.Fatalf("renderPlan = %q", got)
	}
	raw := "not json at all"
	if got := renderPlan(raw); got != raw {
		t.Fatalf("renderPlan fallback = %q", got)
	}
	if got := renderPlan("```json\n{\"constructs_to_translate\":[]}\n```"); !strings.Contains(got, "constructs:") {
		t.Fatalf("fenced renderPlan = %q", got)
	}
}

func TestDiffFailure(t *testing.T) {
	report := exec.Diff(
		exec.ResultSet{Cols: []string{"x"}, Rows: [][]any{{int64(200)}}},
		exec.ResultSet{Cols: []string{"x"}, Rows: [][]any{{int64(5)}}},
	)
	got := diffFailure(report)
	if !strings.Contains(got, "i:200") || !strings.Contains(got, "i:5") {
		t.Fatalf("diffFailure = %q", got)
	}

	schemaDiff := exec.Diff(
		exec.ResultSet{Cols: []string{"a"}, Rows: [][]any{{int64(1)}}},
		exec.ResultSet{Cols: []string{"b"}, Rows: [][]any{{int64(1)}}},
	)
	if got := diffFailure(schemaDiff); !strings.Contains(got, "schema mismatch") {
		t.Fatalf("schema diffFailure = %q", got)
	}

	countDiff := exec.Diff(
		exec.ResultSet{Cols: []string{"a"}, Rows: [][]any{{int64(1)}, {int64(2)}}},
		exec.ResultSet{Cols: []string{"a"}, Rows: [][]any{{int64(1)}}},
	)
	if got := diffFailure(countDiff); !strings.Contains(got, "only on left") {
		t.Fatalf("count diffFailure = %q", got)
	}
}

func TestTableNames(t *testing.T) {
	if got := tableNames(testSchema()); !strings.Contains(got, "orders") || !strings.Contains(got, "customer") {
		t.Fatalf("tableNames = %q", got)
	}
}

func TestRunRejectsNilState(t *testing.T) {
	a := New(nil, nil)
	if _, err := a.Run(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil state")
	}
	if _, err := a.RunBaseline(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestRunWithoutLLMClient(t *testing.T) {
	a := New(nil, testRegistry(testSchema()))
	if _, err := a.Run(context.Background(), &State{}); err == nil {
		t.Fatal("expected error without LLM client")
	}
}
