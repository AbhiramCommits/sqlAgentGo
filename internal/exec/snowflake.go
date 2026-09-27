//go:build snowflake

package exec

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/snowflakedb/gosnowflake" // registers the snowflake database/sql driver
)

// SnowflakeExecutor executes queries against Snowflake via gosnowflake. It
// implements the same Executor interface as the DuckDB target, so `eval` can
// substitute it as the verification target when SNOWFLAKE_ACCOUNT is set.
//
// The target schema must be provisioned beforehand (db/seed_snowflake.sql
// produces fixtures matching the Postgres/DuckDB seeds).
type SnowflakeExecutor struct {
	db *sql.DB
}

// NewSnowflakeExecutor connects using a gosnowflake DSN of the form
// user:password@account/database/schema?warehouse=W.
func NewSnowflakeExecutor(dsn string) (*SnowflakeExecutor, error) {
	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, fmt.Errorf("open snowflake: %w", err)
	}
	// Snowflake doesn't need a connection pool larger than default; keep
	// bounded to avoid burning account connections.
	db.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping snowflake: %w", err)
	}
	return &SnowflakeExecutor{db: db}, nil
}

// Exec runs a statement that produces no result rows.
func (s *SnowflakeExecutor) Exec(ctx context.Context, sql string) error {
	_, err := s.db.ExecContext(ctx, sql)
	return err
}

// Run executes sql and materializes the result set. gosnowflake reports
// DECIMAL/NUMBER columns as strings, so numeric-typed columns are parsed
// back to float64 for engine-neutral comparison.
func (s *SnowflakeExecutor) Run(ctx context.Context, sql string) (ResultSet, error) {
	rows, err := s.db.QueryContext(ctx, sql)
	if err != nil {
		return ResultSet{}, fmt.Errorf("snowflake query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return ResultSet{}, fmt.Errorf("snowflake columns: %w", err)
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return ResultSet{}, fmt.Errorf("snowflake column types: %w", err)
	}

	out := ResultSet{Cols: cols, Rows: [][]any{}}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return ResultSet{}, fmt.Errorf("snowflake scan: %w", err)
		}
		row := make([]any, len(raw))
		for i, v := range raw {
			row[i] = normalizeSnowflake(v, types[i].DatabaseTypeName())
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return ResultSet{}, fmt.Errorf("snowflake rows: %w", err)
	}
	return out, nil
}

// normalizeSnowflake coerces gosnowflake values, parsing string-typed
// decimals into float64 so diffs against Postgres numerics are meaningful.
func normalizeSnowflake(v any, typeName string) any {
	n := normalize(v)
	if s, ok := n.(string); ok && isNumericType(typeName) {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return n
}

func isNumericType(t string) bool {
	t = strings.ToUpper(t)
	return strings.HasPrefix(t, "NUMBER") || strings.HasPrefix(t, "DECIMAL") ||
		strings.HasPrefix(t, "FIXED") || strings.HasPrefix(t, "INTEGER") ||
		strings.HasPrefix(t, "INT") || strings.HasPrefix(t, "FLOAT")
}

// Close releases the connection pool.
func (s *SnowflakeExecutor) Close() error {
	return s.db.Close()
}
