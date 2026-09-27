package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"sqlagent/internal/agent"
	"sqlagent/internal/config"
	"sqlagent/internal/exec"
	"sqlagent/internal/schema"
)

func newEvalCmd() *cobra.Command {
	var (
		dialect string
		file    string
		source  string
		target  string
		noSeed  bool
	)
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Verify a translation by differential execution",
		Long: `Runs the source query on the source engine, translates it to
Snowflake SQL, runs the translation on the target engine, and diffs the two
result sets. Exits non-zero when the results differ.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := readQuery(file, cmd.InOrStdin())
			if err != nil {
				return err
			}

			cfg, err := config.Load("configs", ".")
			if err != nil {
				return err
			}
			a := agent.New(cfg)
			ctx := context.Background()

			sch, err := schema.Load(ctx, defaultDSN(), "public")
			if err != nil {
				return fmt.Errorf("schema introspection: %w", err)
			}

			converted, err := a.Convert(ctx, dialect, query, sch)
			if err != nil {
				return err
			}

			src, err := buildExecutor(source, !noSeed)
			if err != nil {
				return err
			}
			defer closeExecutor(src)
			tgt, err := buildExecutor(target, !noSeed)
			if err != nil {
				return err
			}
			defer closeExecutor(tgt)

			orig, err := src.Run(ctx, query)
			if err != nil {
				return fmt.Errorf("source engine (%s): %w", source, err)
			}
			got, err := tgt.Run(ctx, converted)
			if err != nil {
				return fmt.Errorf("target engine (%s): %w", target, err)
			}

			report := exec.Diff(orig, got)
			fmt.Fprintln(cmd.OutOrStdout(), report.String())
			if !report.Equal {
				return fmt.Errorf("differential verification failed: %s != %s", source, target)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dialect, "dialect", "tsql", "source dialect: tsql or oracle")
	cmd.Flags().StringVarP(&file, "file", "f", "", "SQL file to convert (default: stdin)")
	cmd.Flags().StringVar(&source, "source", "postgres", "source engine: postgres or duckdb")
	cmd.Flags().StringVar(&target, "target", "duckdb", "target engine: postgres or duckdb")
	cmd.Flags().BoolVar(&noSeed, "no-seed", false, "do not seed duckdb from db/seed_duckdb.sql")
	return cmd
}

func buildExecutor(engine string, seedDuck bool) (exec.Executor, error) {
	switch engine {
	case "postgres":
		return exec.NewPostgresExecutor(defaultDSN())
	case "duckdb":
		d, err := exec.NewDuckDBExecutor("")
		if err != nil {
			return nil, err
		}
		if seedDuck {
			script, err := os.ReadFile("db/seed_duckdb.sql")
			if err != nil {
				return nil, fmt.Errorf("read duckdb seed: %w", err)
			}
			if err := d.LoadSQL(string(script)); err != nil {
				return nil, err
			}
		}
		return d, nil
	default:
		return nil, fmt.Errorf("unknown engine %q (want postgres or duckdb)", engine)
	}
}

func closeExecutor(e exec.Executor) {
	type closer interface{ Close() error }
	if c, ok := e.(closer); ok {
		_ = c.Close()
	}
}
