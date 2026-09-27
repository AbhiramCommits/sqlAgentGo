package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const dsnTimeout = 10 * time.Second

// PostgresExecutor executes queries against Postgres via pgx/v5.
type PostgresExecutor struct {
	pool *pgxpool.Pool
}

// NewPostgresExecutor connects to Postgres at dsn and verifies connectivity.
func NewPostgresExecutor(dsn string) (*PostgresExecutor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dsnTimeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresExecutor{pool: pool}, nil
}

// Exec runs a statement that produces no result rows (DDL, INSERT, ...).
func (p *PostgresExecutor) Exec(ctx context.Context, sql string) error {
	_, err := p.pool.Exec(ctx, sql)
	return err
}

// Run executes sql and materializes the result set.
func (p *PostgresExecutor) Run(ctx context.Context, sql string) (ResultSet, error) {
	rows, err := p.pool.Query(ctx, sql)
	if err != nil {
		return ResultSet{}, fmt.Errorf("postgres query: %w", err)
	}
	defer rows.Close()

	cols := make([]string, len(rows.FieldDescriptions()))
	for i, fd := range rows.FieldDescriptions() {
		cols[i] = fd.Name
	}

	out := ResultSet{Cols: cols, Rows: [][]any{}}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return ResultSet{}, fmt.Errorf("postgres scan: %w", err)
		}
		row := make([]any, len(vals))
		for i, v := range vals {
			row[i] = normalize(v)
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return ResultSet{}, fmt.Errorf("postgres rows: %w", err)
	}
	return out, nil
}

// Close releases the underlying connection pool.
func (p *PostgresExecutor) Close() {
	p.pool.Close()
}
