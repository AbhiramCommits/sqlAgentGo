//go:build !noduckdb

package exec

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/marcboeker/go-duckdb" // registers the duckdb database/sql driver
)

// duckdbColumnName maps DuckDB's expression-derived column names
// (count_star(), sum(o_shippriority), count(DISTINCT x), ...) to the
// conventional names Postgres and Snowflake use, so cross-engine result sets
// can be diffed on column names.
func duckdbColumnName(name string) string {
	switch {
	case name == "count_star()":
		return "count"
	case strings.HasPrefix(name, "count(DISTINCT "):
		return "count"
	case strings.HasPrefix(name, "sum("),
		strings.HasPrefix(name, "avg("),
		strings.HasPrefix(name, "min("),
		strings.HasPrefix(name, "max("):
		return strings.SplitN(name, "(", 2)[0]
	default:
		return name
	}
}

// DuckDBExecutor executes queries against an embedded DuckDB instance.
type DuckDBExecutor struct {
	db *sql.DB
}

// NewDuckDBExecutor opens an embedded DuckDB. Pass path "" for a shared
// in-memory database.
func NewDuckDBExecutor(path string) (*DuckDBExecutor, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	// A single connection keeps all statements on the same in-memory
	// database instance.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}
	return &DuckDBExecutor{db: db}, nil
}

// LoadSQL executes a multi-statement script (e.g. db/seed_duckdb.sql).
func (d *DuckDBExecutor) LoadSQL(script string) error {
	stmts, err := SplitStatements(script)
	if err != nil {
		return err
	}
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return fmt.Errorf("duckdb exec %q: %w", truncate(s, 60), err)
		}
	}
	return nil
}

// Exec runs a statement that produces no result rows.
func (d *DuckDBExecutor) Exec(ctx context.Context, sql string) error {
	_, err := d.db.ExecContext(ctx, sql)
	return err
}

// Run executes sql and materializes the result set.
func (d *DuckDBExecutor) Run(ctx context.Context, sql string) (ResultSet, error) {
	rows, err := d.db.QueryContext(ctx, sql)
	if err != nil {
		return ResultSet{}, fmt.Errorf("duckdb query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return ResultSet{}, fmt.Errorf("duckdb columns: %w", err)
	}
	for i, c := range cols {
		cols[i] = duckdbColumnName(c)
	}

	out := ResultSet{Cols: cols, Rows: [][]any{}}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return ResultSet{}, fmt.Errorf("duckdb scan: %w", err)
		}
		row := make([]any, len(raw))
		for i, v := range raw {
			row[i] = normalize(v)
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return ResultSet{}, fmt.Errorf("duckdb rows: %w", err)
	}
	return out, nil
}

// Close releases the DuckDB connection.
func (d *DuckDBExecutor) Close() error {
	return d.db.Close()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
