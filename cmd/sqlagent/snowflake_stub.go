//go:build !snowflake

package main

import (
	"context"
	"fmt"

	"sqlagent/internal/exec"
)

// snowflakeAvailable reports whether Snowflake credentials are configured.
// Without -tags snowflake the executor is not compiled in, so it is never
// available even when the environment is set.
func snowflakeAvailable() bool {
	return false
}

// newSnowflakeTarget reports that this binary was not built with the
// Snowflake target (rebuild with -tags snowflake).
func newSnowflakeTarget(context.Context) (exec.Executor, error) {
	return nil, fmt.Errorf("snowflake target not compiled in: rebuild with -tags snowflake")
}
