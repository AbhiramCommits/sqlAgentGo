package main

import (
	"context"
	"fmt"
	"os"

	"sqlagent/internal/agent"
	"sqlagent/internal/config"
	"sqlagent/internal/exec"
	"sqlagent/internal/llm"
	"sqlagent/internal/schema"
	"sqlagent/internal/tools"
)

// buildLLMClient constructs the OpenAI-compatible client from viper config;
// the API key is read from the environment variable named by
// config.llm.api_key_env.
func buildLLMClient(cfg *config.Config) *llm.Client {
	return llm.New(llm.Config{
		BaseURL:     cfg.LLM.BaseURL,
		APIKey:      os.Getenv(cfg.LLM.APIKeyEnv),
		Model:       cfg.LLM.Model,
		MaxRetries:  cfg.LLM.MaxRetries,
		Temperature: cfg.LLM.Temperature,
	})
}

// buildAgent wires the full translation pipeline: introspected schema,
// Postgres source executor, seeded DuckDB target executor, the tool
// registry, and the LLM client.
func buildAgent(ctx context.Context, cfg *config.Config, caseID string, maxAttempts int) (*agent.Agent, *schema.Schema, error) {
	sch, err := schema.Load(ctx, defaultDSN(), "public")
	if err != nil {
		return nil, nil, fmt.Errorf("schema introspection (is `make up` running?): %w", err)
	}
	pg, err := exec.NewPostgresExecutor(defaultDSN())
	if err != nil {
		return nil, nil, fmt.Errorf("postgres executor: %w", err)
	}
	dk, err := exec.NewDuckDBExecutor("")
	if err != nil {
		return nil, nil, fmt.Errorf("duckdb executor: %w", err)
	}
	script, err := os.ReadFile("db/seed_duckdb.sql")
	if err != nil {
		return nil, nil, fmt.Errorf("read db/seed_duckdb.sql: %w", err)
	}
	if err := dk.LoadSQL(string(script)); err != nil {
		return nil, nil, fmt.Errorf("seed duckdb: %w", err)
	}

	dialectRef, err := tools.NewDialectRef()
	if err != nil {
		return nil, nil, err
	}
	reg := tools.NewRegistry(
		&tools.SchemaLookup{Schema: sch},
		dialectRef,
		&tools.DryRun{Target: dk},
		&tools.ExecuteAndDiff{Source: pg, Target: dk},
	)

	a := agent.New(buildLLMClient(cfg), reg,
		agent.WithExecutors(pg, dk),
		agent.WithTraceDir("traces"),
		agent.WithCaseID(caseID),
		agent.WithMaxAttempts(maxAttempts),
	)
	return a, sch, nil
}
