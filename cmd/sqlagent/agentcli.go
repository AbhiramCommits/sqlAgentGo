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

// buildDuckDBTarget constructs the zero-credential verification target: an
// embedded DuckDB instance seeded from db/seed_duckdb.sql. It returns nil
// when the driver is not compiled in (noduckdb builds) or the seed is
// missing.
func buildDuckDBTarget() exec.Executor {
	dk, err := exec.NewDuckDBExecutor("")
	if err != nil {
		return nil
	}
	script, err := os.ReadFile("db/seed_duckdb.sql")
	if err != nil {
		_ = dk.Close()
		return nil
	}
	if err := dk.LoadSQL(string(script)); err != nil {
		_ = dk.Close()
		return nil
	}
	return dk
}

// buildPipeline wires the shared pieces of the evaluation pipeline: the
// introspected schema, the Postgres source executor, the verification target
// (DuckDB by default, Snowflake in -tags snowflake builds), and the tool
// registry.
func buildPipeline(ctx context.Context, target exec.Executor) (*schema.Schema, *exec.PostgresExecutor, *tools.Registry, error) {
	sch, err := schema.Load(ctx, defaultDSN(), "public")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("schema introspection (is `make up` running?): %w", err)
	}
	pg, err := exec.NewPostgresExecutor(defaultDSN())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("postgres executor: %w", err)
	}

	dialectRef, err := tools.NewDialectRef()
	if err != nil {
		return nil, nil, nil, err
	}
	dryRunTarget := target
	if dryRunTarget == nil {
		dryRunTarget = pg
	}
	reg := tools.NewRegistry(
		&tools.SchemaLookup{Schema: sch},
		dialectRef,
		&tools.DryRun{Target: dryRunTarget},
		&tools.ExecuteAndDiff{Source: pg, Target: target},
	)
	return sch, pg, reg, nil
}

// buildAgent wires the full translation pipeline for the convert/serve
// commands, using DuckDB as the verification target.
func buildAgent(ctx context.Context, cfg *config.Config, caseID string, maxAttempts int) (*agent.Agent, *schema.Schema, error) {
	target := buildDuckDBTarget()
	if target == nil {
		return nil, nil, fmt.Errorf("no verification target available: duckdb driver unavailable (noduckdb build)")
	}
	sch, pg, reg, err := buildPipeline(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	a := agent.New(buildLLMClient(cfg, ""), reg,
		agent.WithExecutors(pg, target),
		agent.WithTraceDir("traces"),
		agent.WithCaseID(caseID),
		agent.WithMaxAttempts(maxAttempts),
	)
	return a, sch, nil
}
