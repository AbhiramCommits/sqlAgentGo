// Command sqlagent converts T-SQL / Oracle PL/SQL SELECT statements into
// Snowflake-flavored SQL and verifies translations by differential execution.
package main

import (
	"os"

	"github.com/spf13/cobra"

	"sqlagent/internal/exec"
)

func defaultDSN() string {
	return exec.DefaultPostgresDSN()
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "sqlagent",
		Short:        "SQL dialect conversion agent",
		Long:         "Converts T-SQL and Oracle PL/SQL SELECT statements into Snowflake-flavored SQL and verifies translations by differential execution.",
		SilenceUsage: true,
	}
	root.AddCommand(
		newConvertCmd(),
		newEvalCmd(),
		newServeCmd(),
		newSeedCmd(),
		newToolsCmd(),
	)
	return root
}
