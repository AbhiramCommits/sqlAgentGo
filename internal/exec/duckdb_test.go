package exec

import (
	"context"
	"testing"
)

func TestDuckDBColumnNameNormalization(t *testing.T) {
	cases := map[string]string{
		"count_star()":                "count",
		"count(DISTINCT o_orderdate)": "count",
		"sum(o_shippriority)":         "sum",
		"avg(o_totalprice)":           "avg",
		"min(o_orderkey)":             "min",
		"max(o_totalprice)":           "max",
		"o_orderkey":                  "o_orderkey",
		"some_expression(x, y)":       "some_expression(x, y)",
	}
	for in, want := range cases {
		if got := duckdbColumnName(in); got != want {
			t.Fatalf("duckdbColumnName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDuckDBLoadSQLError(t *testing.T) {
	d, err := NewDuckDBExecutor("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := d.LoadSQL("CREATE TABLE t (x INT); INSERT INTO nope VALUES (1)"); err == nil {
		t.Fatal("expected error for invalid script statement")
	}
}

func TestDuckDBExecutorLifecycle(t *testing.T) {
	d, err := NewDuckDBExecutor("")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Exec(context.Background(), "CREATE TABLE t (x INT)"); err != nil {
		t.Fatal(err)
	}
	rs, err := d.Run(context.Background(), "SELECT 41 + 1 AS answer")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("rows = %+v", rs.Rows)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Fatal("short string truncated")
	}
	if truncate("1234567890abc", 10) != "1234567890..." {
		t.Fatal("long string not truncated")
	}
}

func TestDefaultPostgresDSN(t *testing.T) {
	if got := DefaultPostgresDSN(); got == "" {
		t.Fatal("empty dsn")
	}
	t.Setenv("SQLAGENT_PG_DSN", "postgres://custom")
	if got := DefaultPostgresDSN(); got != "postgres://custom" {
		t.Fatalf("env override ignored: %q", got)
	}
}
