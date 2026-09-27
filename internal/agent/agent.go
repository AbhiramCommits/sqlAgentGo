// Package agent translates T-SQL and Oracle PL/SQL SELECT statements into
// Snowflake-flavored SQL. The LLM integration is not implemented yet: this is
// a stub that passes queries through unchanged so the rest of the pipeline
// (corpus loading, executors, differential verification) can be exercised.
package agent

import (
	"context"
	"errors"

	"sqlagent/internal/config"
	"sqlagent/internal/schema"
)

// Dialect identifies a source SQL dialect.
type Dialect string

const (
	DialectTSQL   Dialect = "tsql"
	DialectOracle Dialect = "oracle"
)

// Agent converts SQL between dialects. TODO: wire in the LLM client using
// config.LLM (base URL, model, API key env var, max retries).
type Agent struct {
	cfg *config.Config
}

// New creates an Agent from configuration.
func New(cfg *config.Config) *Agent {
	return &Agent{cfg: cfg}
}

// Convert translates a source-dialect SELECT into Snowflake SQL.
// STUB: returns the query unchanged until LLM integration lands.
func (a *Agent) Convert(ctx context.Context, dialect string, query string, sch *schema.Schema) (string, error) {
	if query == "" {
		return "", errors.New("empty query")
	}
	switch Dialect(dialect) {
	case DialectTSQL, DialectOracle:
		// supported dialects
	default:
		return "", errors.New("unsupported dialect: " + dialect)
	}
	return query, nil
}
