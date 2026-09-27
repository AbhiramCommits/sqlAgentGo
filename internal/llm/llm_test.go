package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, srv *httptest.Server, maxRetries int) *Client {
	t.Helper()
	return New(Config{
		BaseURL:        srv.URL + "/v1",
		APIKey:         "test-key",
		Model:          "test-model",
		MaxRetries:     maxRetries,
		RetryBaseDelay: time.Millisecond,
	})
}

func TestChatRequestShapeAndUsage(t *testing.T) {
	var (
		mu      sync.Mutex
		gotPath string
		gotAuth string
		got     map[string]json.RawMessage
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"SELECT 1"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`))
	}))
	defer srv.Close()

	c := testClient(t, srv, 0)
	resp, err := c.Chat(context.Background(),
		[]Message{{Role: "system", Content: "hi"}},
		[]ToolDef{{Type: "function", Function: FunctionDef{Name: "dry_run", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "SELECT 1" {
		t.Fatalf("content = %q", resp.Content)
	}
	if resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 3 {
		t.Fatalf("usage = %+v", resp.Usage)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth = %q", gotAuth)
	}
	var model string
	var temperature float64
	var messages []Message
	var tools []ToolDef
	if err := json.Unmarshal(got["model"], &model); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got["temperature"], &temperature); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got["messages"], &messages); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	if model != "test-model" {
		t.Fatalf("model = %q", model)
	}
	if temperature != 0 {
		t.Fatalf("temperature = %v, want 0", temperature)
	}
	if len(messages) != 1 || messages[0].Role != "system" {
		t.Fatalf("messages = %+v", messages)
	}
	if len(tools) != 1 || tools[0].Function.Name != "dry_run" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{}}`))
	}))
	defer srv.Close()

	resp, err := testClient(t, srv, 3).Chat(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q", resp.Content)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestRetryOn5xxThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(int(500 + n))
			_, _ = w.Write([]byte("boom"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{}}`))
	}))
	defer srv.Close()

	if _, err := testClient(t, srv, 3).Chat(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"server exploded"}}`))
	}))
	defer srv.Close()

	_, err := testClient(t, srv, 2).Chat(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error after retries")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3 (1 initial + 2 retries)", calls.Load())
	}
}

func TestNonRetryableErrorIsImmediate(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()

	if _, err := testClient(t, srv, 3).Chat(context.Background(), nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestToolCallsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "choices":[{"message":{"role":"assistant","content":null,"tool_calls":[
    {"id":"call_1","type":"function","function":{"name":"dialect_ref","arguments":"{\"construct\":\"NVL\",\"source_dialect\":\"oracle\"}"}}
  ]},"finish_reason":"tool_calls"}],
  "usage":{"prompt_tokens":50,"completion_tokens":10}
}`))
	}))
	defer srv.Close()

	resp, err := testClient(t, srv, 0).Chat(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "dialect_ref" || tc.Function.Arguments != `{"construct":"NVL","source_dialect":"oracle"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", resp.FinishReason)
	}
}

func TestContextCancellationDuringBackoff(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err := testClient(t, srv, 5).Chat(ctx, nil, nil)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}
