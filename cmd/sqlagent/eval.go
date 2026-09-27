package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"sqlagent/internal/agent"
	"sqlagent/internal/config"
)

func newEvalCmd() *cobra.Command {
	var (
		dialect     string
		file        string
		caseID      string
		maxAttempts int
	)
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run the conversion pipeline and verify it end to end",
		Long: `Runs the full agent loop (plan, act with tools, guard, verify with
repairs) for a source query and reports the outcome, attempt count, final
candidate, and trace path. Exits non-zero unless the run is green.`,
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
			id := caseID
			if id == "" {
				id = "eval"
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

			fmt.Fprintf(cmd.OutOrStdout(), "status=%s attempts=%d trace=%s\n", st.Status, st.Attempt, res.TracePath)
			fmt.Fprintln(cmd.OutOrStdout(), st.Candidate)
			if st.Status != agent.StatusGreen {
				return fmt.Errorf("evaluation not green: status=%s (trace: %s)", st.Status, res.TracePath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dialect, "dialect", "tsql", "source dialect: tsql or oracle")
	cmd.Flags().StringVarP(&file, "file", "f", "", "SQL file to evaluate (default: stdin)")
	cmd.Flags().StringVar(&caseID, "case-id", "", "trace case id (default: eval)")
	cmd.Flags().IntVar(&maxAttempts, "max-attempts", 0, "max conversion attempts (default 4)")
	return cmd
}
