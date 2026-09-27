//go:build snowflake

package main

import (
	"context"
	"fmt"
	"os"

	"sqlagent/internal/exec"
)

// snowflakeAvailable reports whether Snowflake credentials are configured.
func snowflakeAvailable() bool {
	return os.Getenv("SNOWFLAKE_ACCOUNT") != ""
}

// newSnowflakeTarget builds the Snowflake verification target from the
// SNOWFLAKE_* environment variables:
//
//	SNOWFLAKE_ACCOUNT (required), SNOWFLAKE_USER, SNOWFLAKE_PASSWORD,
//	SNOWFLAKE_DATABASE (default TPCH), SNOWFLAKE_SCHEMA (default PUBLIC),
//	SNOWFLAKE_WAREHOUSE (default COMPUTE_WH)
//
// The target schema must be provisioned first (see db/seed_snowflake.sql).
func newSnowflakeTarget(ctx context.Context) (exec.Executor, error) {
	account := os.Getenv("SNOWFLAKE_ACCOUNT")
	user := os.Getenv("SNOWFLAKE_USER")
	password := os.Getenv("SNOWFLAKE_PASSWORD")
	if account == "" {
		return nil, fmt.Errorf("SNOWFLAKE_ACCOUNT is unset")
	}
	database := envDefault("SNOWFLAKE_DATABASE", "TPCH")
	schema := envDefault("SNOWFLAKE_SCHEMA", "PUBLIC")
	warehouse := envDefault("SNOWFLAKE_WAREHOUSE", "COMPUTE_WH")

	dsn := fmt.Sprintf("%s:%s@%s/%s/%s?warehouse=%s", user, password, account, database, schema, warehouse)
	return exec.NewSnowflakeExecutor(dsn)
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
