package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"sqlagent/internal/agent"
	"sqlagent/internal/config"
)

func newConvertCmd() *cobra.Command {
	var (
		dialect     string
		file        string
		target      string
		caseID      string
		maxAttempts int
	)
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert a source-dialect SELECT into Snowflake SQL",
		Long: `Reads a query from --file (or stdin), runs the translation agent
(plan -> act with tools -> guard -> verify, repairing up to --max-attempts),
and prints the final Snowflake SQL. Exits non-zero when the run is exhausted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			query, err := readQuery(file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if target != "snowflake" {
				return fmt.Errorf("unsupported target %q (want snowflake)", target)
			}

			cfg, err := config.Load("configs", ".")
			if err != nil {
				return err
			}

			id := caseID
			if id == "" {
				id = "stdin"
				if file != "" {
					base := filepath.Base(file)
					id = strings.TrimSuffix(base, filepath.Ext(base))
				}
			}

			a, sch, err := buildAgent(cmd.Context(), cfg, id, maxAttempts)
			if err != nil {
				return err
			}

			st := &agent.State{
				SourceSQL:     query,
				SourceDialect: dialect,
				Schema:        *sch,
				MaxAttempts:   maxAttempts,
			}
			res, err := a.Run(cmd.Context(), st)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), st.Candidate)
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "attempts=%d status=%s trace=%s\n", st.Attempt, st.Status, res.TracePath)
			if st.Status == agent.StatusExhausted {
				return fmt.Errorf("conversion exhausted after %d attempts (trace: %s)", st.Attempt, res.TracePath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dialect, "dialect", "tsql", "source dialect: tsql or oracle")
	cmd.Flags().StringVarP(&file, "file", "f", "", "SQL file to convert (default: stdin)")
	cmd.Flags().StringVar(&target, "target", "snowflake", "target dialect (snowflake)")
	cmd.Flags().StringVar(&caseID, "case-id", "", "trace case id (default: derived from --file, else stdin)")
	cmd.Flags().IntVar(&maxAttempts, "max-attempts", 0, "max conversion attempts (default 4)")
	return cmd
}

func readQuery(file string, stdin io.Reader) (string, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read query file: %w", err)
		}
		return string(data), nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return string(data), nil
}
