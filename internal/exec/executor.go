// Package exec provides query executors behind a common interface plus
// ResultSet diffing for differential execution verification.
package exec

import (
	"context"
	"os"
)

// Executor runs a SQL statement and returns its result set. Implementations
// may additionally expose statement-only methods (Exec, Close) beyond this
// interface.
type Executor interface {
	Run(ctx context.Context, sql string) (ResultSet, error)
}

// DefaultPostgresDSN returns the Postgres DSN from SQLAGENT_PG_DSN, falling
// back to the docker-compose service on localhost:5435.
func DefaultPostgresDSN() string {
	if dsn := os.Getenv("SQLAGENT_PG_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://sqlagent:sqlagent@localhost:5435/tpch?sslmode=disable"
}
