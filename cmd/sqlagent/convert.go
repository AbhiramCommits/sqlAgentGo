package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"sqlagent/internal/agent"
	"sqlagent/internal/config"
	"sqlagent/internal/schema"
)

func newConvertCmd() *cobra.Command {
	var (
		dialect string
		file    string
	)
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert a source-dialect SELECT into Snowflake SQL",
		Long:  "Reads a query from --file (or stdin) and prints the translated Snowflake SQL. Translation is currently a stub (pass-through).",
		Args:  cobra.NoArgs,
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
			var sch *schema.Schema
			if s, err := schema.Load(ctx, defaultDSN(), "public"); err == nil {
				sch = s
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: schema introspection unavailable: %v\n", err)
			}

			out, err := a.Convert(ctx, dialect, query, sch)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().StringVar(&dialect, "dialect", "tsql", "source dialect: tsql or oracle")
	cmd.Flags().StringVarP(&file, "file", "f", "", "SQL file to convert (default: stdin)")
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
