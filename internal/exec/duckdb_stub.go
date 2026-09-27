//go:build noduckdb

package exec

import (
	"context"
	"errors"
)

// This build tag removes the cgo DuckDB driver so the binary can be built
// fully static (CGO_ENABLED=0) and shipped on a distroless/static base image
// under 30MB. The server keeps running; differential verification then
// requires the Snowflake target (build with -tags "noduckdb snowflake").
// See the Dockerfile sqlagent-minimal target.

var errNoDuckDB = errors.New("duckdb support not compiled in (noduckdb build)")

// DuckDBExecutor is a stub in noduckdb builds.
type DuckDBExecutor struct{}

// NewDuckDBExecutor returns an error: the driver is not compiled in.
func NewDuckDBExecutor(string) (*DuckDBExecutor, error) {
	return nil, errNoDuckDB
}

// LoadSQL reports the driver is unavailable.
func (d *DuckDBExecutor) LoadSQL(string) error { return errNoDuckDB }

// Exec reports the driver is unavailable.
func (d *DuckDBExecutor) Exec(context.Context, string) error { return errNoDuckDB }

// Run reports the driver is unavailable.
func (d *DuckDBExecutor) Run(context.Context, string) (ResultSet, error) {
	return ResultSet{}, errNoDuckDB
}

// Close is a no-op on the stub.
func (d *DuckDBExecutor) Close() error { return nil }

// duckNormalize is a no-op in noduckdb builds (no DuckDB value types exist).
func duckNormalize(any) (any, bool) { return nil, false }
