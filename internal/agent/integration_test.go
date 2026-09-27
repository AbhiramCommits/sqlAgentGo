//go:build integration

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sqlagent/internal/config"
	"sqlagent/internal/corpus"
	"sqlagent/internal/llm"
)

// TestIntegrationLiveLLM runs three real corpus cases end to end against a
// live LLM endpoint. It is skipped when no API key is configured — unless
// LLM_BASE_URL/LLM_MODEL point the client at a local model server (Ollama,
// vLLM) that needs no key.
//
// Run with:
//
//	go test -tags integration ./internal/agent -run TestIntegrationLiveLLM -v
func TestIntegrationLiveLLM(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "configs"), ".")
	if err != nil {
		t.Fatal(err)
	}
	baseURL, model := cfg.LLM.BaseURL, cfg.LLM.Model
	if s := os.Getenv("LLM_BASE_URL"); s != "" {
		baseURL = s
	}
	if s := os.Getenv("LLM_MODEL"); s != "" {
		model = s
	}
	key := os.Getenv(cfg.LLM.APIKeyEnv)
	if key == "" && os.Getenv("LLM_BASE_URL") == "" {
		t.Skipf("skipping live-LLM integration test: %s unset", cfg.LLM.APIKeyEnv)
	}

	c, err := corpus.LoadDir(filepath.Join("..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tsql_top_n_orders", "orcl_nvl", "tsql_join_two"}
	var cases []corpus.Case
	for _, id := range want {
		found := false
		for _, tc := range c.Cases {
			if tc.ID == id {
				cases = append(cases, tc)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("corpus case %q not found", id)
		}
	}

	client := llm.New(llm.Config{
		BaseURL:        baseURL,
		APIKey:         key,
		Model:          model,
		MaxRetries:     cfg.LLM.MaxRetries,
		Temperature:    cfg.LLM.Temperature,
		RetryBaseDelay: time.Second,
	})
	a := New(client, testRegistry(testSchema()),
		WithExecutors(seededDuckDB(t), seededDuckDB(t)),
		WithTraceDir(t.TempDir()),
		WithCaseID("integration"),
		WithMaxAttempts(3))

	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			st := &State{
				CaseID:        tc.ID,
				SourceSQL:     tc.SourceSQL,
				SourceDialect: tc.SourceDialect,
				Schema:        *testSchema(),
				MaxAttempts:   3,
			}
			res, err := a.Run(context.Background(), st)
			if err != nil {
				t.Fatalf("agent run failed: %v", err)
			}
			switch st.Status {
			case StatusGreen, StatusExhausted:
			default:
				t.Fatalf("unexpected status %q", st.Status)
			}
			if st.Attempt < 1 {
				t.Fatalf("attempts = %d", st.Attempt)
			}
			if res.PromptTokens == 0 && res.CompletionTokens == 0 {
				t.Fatalf("no token usage recorded against live endpoint")
			}
			if _, err := os.Stat(res.TracePath); err != nil {
				t.Fatalf("trace not written: %v", err)
			}
			t.Logf("%s: status=%s attempts=%d prompt=%d completion=%d trace=%s",
				tc.ID, st.Status, st.Attempt, res.PromptTokens, res.CompletionTokens, res.TracePath)
		})
	}
}
