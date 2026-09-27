package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"sqlagent/internal/exec"
	"sqlagent/internal/schema"
	"sqlagent/internal/tools"
)

// newToolsCmd exposes the tool registry from the CLI so tools can be listed
// and dispatched by name without the LLM loop.
func newToolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "tools",
		Short:  "Inspect and invoke agent tools",
		Hidden: true,
	}
	cmd.AddCommand(newToolsListCmd(), newToolsInvokeCmd())
	return cmd
}

func newToolsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg, err := buildToolRegistry(cmd.Context())
			if err != nil {
				return err
			}
			for _, t := range reg.All() {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-16s %s\n", t.Name(), t.Description())
			}
			return nil
		},
	}
}

func newToolsInvokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "invoke <tool> [json-args]",
		Short: "Invoke a tool by name with JSON arguments",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := buildToolRegistry(cmd.Context())
			if err != nil {
				return err
			}
			var raw json.RawMessage
			if len(args) == 2 {
				raw = json.RawMessage(args[1])
			}
			out, err := reg.Invoke(cmd.Context(), args[0], raw)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

// buildToolRegistry wires the four tools to the introspected schema and the
// source (Postgres) and target (DuckDB) executors.
func buildToolRegistry(ctx context.Context) (*tools.Registry, error) {
	sch, err := schema.Load(ctx, defaultDSN(), "public")
	if err != nil {
		return nil, fmt.Errorf("schema introspection (is `make up` running?): %w", err)
	}
	pg, err := exec.NewPostgresExecutor(defaultDSN())
	if err != nil {
		return nil, fmt.Errorf("postgres executor: %w", err)
	}
	dk, err := exec.NewDuckDBExecutor("")
	if err != nil {
		return nil, fmt.Errorf("duckdb executor: %w", err)
	}
	script, err := os.ReadFile("db/seed_duckdb.sql")
	if err != nil {
		return nil, fmt.Errorf("read db/seed_duckdb.sql: %w", err)
	}
	if err := dk.LoadSQL(string(script)); err != nil {
		return nil, fmt.Errorf("seed duckdb: %w", err)
	}

	dialectRef, err := tools.NewDialectRef()
	if err != nil {
		return nil, err
	}
	return tools.NewRegistry(
		&tools.SchemaLookup{Schema: sch},
		dialectRef,
		&tools.DryRun{Target: dk},
		&tools.ExecuteAndDiff{Source: pg, Target: dk},
	), nil
}
