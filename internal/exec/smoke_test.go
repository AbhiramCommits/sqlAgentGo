package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSmokeSeedAndDiff seeds Postgres (docker-compose) and an embedded
// DuckDB instance, runs the same queries on both engines, and asserts the
// ResultSets diff clean.
//
// Requires `make up` first; it is skipped when Postgres is unreachable.
func TestSmokeSeedAndDiff(t *testing.T) {
	ctx := context.Background()

	pg, err := NewPostgresExecutor(DefaultPostgresDSN())
	if err != nil {
		t.Skipf("postgres not reachable (run `make up`): %v", err)
	}
	defer pg.Close()

	pgSeed := readSeedFile(t, "seed.sql")
	stmts, err := SplitStatements(pgSeed)
	if err != nil {
		t.Fatalf("parse seed.sql: %v", err)
	}
	for _, s := range stmts {
		if err := pg.Exec(ctx, s); err != nil {
			t.Fatalf("postgres seed statement failed: %v\n%s", err, s)
		}
	}

	dk, err := NewDuckDBExecutor("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dk.Close() }()
	if err := dk.LoadSQL(readSeedFile(t, "seed_duckdb.sql")); err != nil {
		t.Fatalf("duckdb seed failed: %v", err)
	}

	queries := []string{
		"SELECT count(*) FROM orders",
		"SELECT count(*) FROM lineitem",
		"SELECT count(*) FROM customer",
		"SELECT sum(o_shippriority) FROM orders",
		"SELECT count(DISTINCT o_orderdate) FROM orders",
	}

	for _, q := range queries {
		pgRes, err := pg.Run(ctx, q)
		if err != nil {
			t.Fatalf("postgres %s: %v", q, err)
		}
		dkRes, err := dk.Run(ctx, q)
		if err != nil {
			t.Fatalf("duckdb %s: %v", q, err)
		}
		report := Diff(pgRes, dkRes)
		if !report.Equal {
			t.Errorf("%s:\npostgres=%v duckdb=%v\n%s", q, pgRes.Rows, dkRes.Rows, report.String())
		}
	}
}

func readSeedFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "db", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
