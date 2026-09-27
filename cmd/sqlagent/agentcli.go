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
// config.llm.api_key_env. model overrides the configured model when non-empty.
func buildLLMClient(cfg *config.Config, model string) *llm.Client {
	if model == "" {
		model = cfg.LLM.Model
	}
	return llm.New(llm.Config{
		BaseURL:     cfg.LLM.BaseURL,
		APIKey:      os.Getenv(cfg.LLM.APIKeyEnv),
		Model:       model,
		MaxRetries:  cfg.LLM.MaxRetries,
		Temperature: cfg.LLM.Temperature,
	})
}

// buildPipeline wires the shared pieces of the evaluation harness: the
// introspected schema, the Postgres source executor, the seeded DuckDB
// target executor, and the tool registry.
func buildPipeline(ctx context.Context) (*schema.Schema, *exec.PostgresExecutor, *exec.DuckDBExecutor, *tools.Registry, error) {
	sch, err := schema.Load(ctx, defaultDSN(), "public")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("schema introspection (is `make up` running?): %w", err)
	}
	pg, err := exec.NewPostgresExecutor(defaultDSN())
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("postgres executor: %w", err)
	}
	dk, err := exec.NewDuckDBExecutor("")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("duckdb executor: %w", err)
	}
	script, err := os.ReadFile("db/seed_duckdb.sql")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("read db/seed_duckdb.sql: %w", err)
	}
	if err := dk.LoadSQL(string(script)); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("seed duckdb: %w", err)
	}

	dialectRef, err := tools.NewDialectRef()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	reg := tools.NewRegistry(
		&tools.SchemaLookup{Schema: sch},
		dialectRef,
		&tools.DryRun{Target: dk},
		&tools.ExecuteAndDiff{Source: pg, Target: dk},
	)
	return sch, pg, dk, reg, nil
}

// buildAgent wires the full translation pipeline for the convert/serve
// commands.
func buildAgent(ctx context.Context, cfg *config.Config, caseID string, maxAttempts int) (*agent.Agent, *schema.Schema, error) {
	sch, pg, dk, reg, err := buildPipeline(ctx)
	if err != nil {
		return nil, nil, err
	}
	a := agent.New(buildLLMClient(cfg, ""), reg,
		agent.WithExecutors(pg, dk),
		agent.WithTraceDir("traces"),
		agent.WithCaseID(caseID),
		agent.WithMaxAttempts(maxAttempts),
	)
	return a, sch, nil
}
