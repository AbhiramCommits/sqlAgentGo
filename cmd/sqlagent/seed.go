package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"sqlagent/internal/exec"
)

// newSeedCmd seeds both engines: Postgres (docker-compose) from db/seed.sql
// and an embedded DuckDB instance from db/seed_duckdb.sql. It is hidden; it
// backs the `make seed` target.
func newSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "seed",
		Short:  "Seed Postgres and DuckDB with the TPCH-lite dataset",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			pg, err := exec.NewPostgresExecutor(defaultDSN())
			if err != nil {
				return fmt.Errorf("postgres unavailable (is `make up` running?): %w", err)
			}
			defer pg.Close()

			pgSeed, err := os.ReadFile("db/seed.sql")
			if err != nil {
				return fmt.Errorf("read db/seed.sql: %w", err)
			}
			stmts, err := exec.SplitStatements(string(pgSeed))
			if err != nil {
				return fmt.Errorf("parse db/seed.sql: %w", err)
			}
			for _, s := range stmts {
				if err := pg.Exec(ctx, s); err != nil {
					return fmt.Errorf("postgres seed: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "postgres: seeded from db/seed.sql (%d statements)\n", len(stmts))

			dk, err := exec.NewDuckDBExecutor("")
			if err != nil {
				return err
			}
			defer dk.Close()

			dkSeed, err := os.ReadFile("db/seed_duckdb.sql")
			if err != nil {
				return fmt.Errorf("read db/seed_duckdb.sql: %w", err)
			}
			if err := dk.LoadSQL(string(dkSeed)); err != nil {
				return fmt.Errorf("duckdb seed: %w", err)
			}

			for _, q := range []string{
				"SELECT count(*) FROM orders",
				"SELECT count(*) FROM lineitem",
			} {
				pgr, err := pg.Run(ctx, q)
				if err != nil {
					return err
				}
				dkr, err := dk.Run(ctx, q)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: postgres=%v duckdb=%v\n", q, pgr.Rows, dkr.Rows)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "duckdb: seeded from db/seed_duckdb.sql (in-memory)")
			return nil
		},
	}
}
