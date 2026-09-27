package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"sqlagent/internal/exec"
	"sqlagent/internal/schema"
)

// testSchema mirrors the TPCH-lite seed (db/seed.sql).
func testSchema() *schema.Schema {
	return &schema.Schema{Tables: []schema.Table{
		{Name: "region", Columns: cols(
			"r_regionkey", "integer", "r_name", "character varying", "r_comment", "character varying")},
		{Name: "nation", Columns: cols(
			"n_nationkey", "integer", "n_name", "character varying", "n_regionkey", "integer", "n_comment", "character varying")},
		{Name: "customer", Columns: cols(
			"c_custkey", "integer", "c_name", "character varying", "c_address", "character varying",
			"c_nationkey", "integer", "c_phone", "character varying", "c_acctbal", "numeric",
			"c_mktsegment", "character varying", "c_comment", "character varying")},
		{Name: "orders", Columns: cols(
			"o_orderkey", "integer", "o_custkey", "integer", "o_orderstatus", "character varying",
			"o_totalprice", "numeric", "o_orderdate", "date", "o_orderpriority", "character varying",
			"o_clerk", "character varying", "o_shippriority", "integer", "o_comment", "character varying")},
		{Name: "lineitem", Columns: cols(
			"l_orderkey", "integer", "l_partkey", "integer", "l_suppkey", "integer", "l_linenumber", "integer",
			"l_quantity", "numeric", "l_extendedprice", "numeric", "l_discount", "numeric", "l_tax", "numeric",
			"l_returnflag", "character varying", "l_linestatus", "character varying", "l_shipdate", "date",
			"l_commitdate", "date", "l_receiptdate", "date", "l_shipinstruct", "character varying",
			"l_shipmode", "character varying", "l_comment", "character varying")},
	}}
}

func cols(pairs ...string) []schema.Column {
	out := make([]schema.Column, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, schema.Column{Name: pairs[i], Type: pairs[i+1]})
	}
	return out
}

// seededDuckDB is a process-wide seeded in-memory DuckDB instance shared by
// the tools tests (read-only queries against db/seed_duckdb.sql data).
var (
	duckDBOnce sync.Once
	duckDBExec *exec.DuckDBExecutor
	duckDBErr  error
)

func seededDuckDB(t *testing.T) *exec.DuckDBExecutor {
	t.Helper()
	duckDBOnce.Do(func() {
		duckDBExec, duckDBErr = exec.NewDuckDBExecutor("")
		if duckDBErr != nil {
			return
		}
		path := filepath.Join("..", "..", "db", "seed_duckdb.sql")
		script, err := os.ReadFile(path)
		if err != nil {
			duckDBErr = err
			return
		}
		duckDBErr = duckDBExec.LoadSQL(string(script))
	})
	if duckDBErr != nil {
		t.Fatalf("seed duckdb: %v", duckDBErr)
	}
	return duckDBExec
}

func invoke(t *testing.T, tool Tool, args string) (string, error) {
	t.Helper()
	return tool.Invoke(context.Background(), json.RawMessage(args))
}

func TestSchemaLookup(t *testing.T) {
	tool := &SchemaLookup{Schema: testSchema()}

	tests := []struct {
		name    string
		args    string
		wantSub []string
		wantErr bool
	}{
		{
			name:    "no table lists names only",
			args:    `{}`,
			wantSub: []string{"customer", "lineitem", "nation", "orders", "region"},
		},
		{
			name:    "explicit table lists columns",
			args:    `{"table": "orders"}`,
			wantSub: []string{"o_orderkey", "integer", "o_totalprice", "numeric", "o_orderdate", "date"},
		},
		{
			name:    "table lookup is case-insensitive",
			args:    `{"table": "ORDERS"}`,
			wantSub: []string{"o_orderkey"},
		},
		{
			name:    "unknown table errors",
			args:    `{"table": "nope"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := invoke(t, tool, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got output %q", out)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantSub {
				if !strings.Contains(out, want) {
					t.Fatalf("output %q missing %q", out, want)
				}
			}
			if strings.Contains(out, "integer") && strings.Contains(out, "customer") &&
				len(tt.wantSub) > 0 && tt.wantSub[0] == "customer" {
				t.Fatalf("table listing should not include column details: %q", out)
			}
		})
	}
}

func TestDialectRef(t *testing.T) {
	tool, err := NewDialectRef()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		args    string
		wantSub []string
		wantErr bool
	}{
		{
			name:    "tsql TOP maps to LIMIT",
			args:    `{"construct": "TOP", "source_dialect": "tsql"}`,
			wantSub: []string{"TOP n", "LIMIT n"},
		},
		{
			name:    "oracle NVL maps to COALESCE",
			args:    `{"construct": "NVL", "source_dialect": "oracle"}`,
			wantSub: []string{"NVL", "COALESCE"},
		},
		{
			name:    "tsql GETDATE maps to CURRENT_TIMESTAMP",
			args:    `{"construct": "GETDATE", "source_dialect": "tsql"}`,
			wantSub: []string{"GETDATE", "CURRENT_TIMESTAMP"},
		},
		{
			name:    "oracle ROWNUM maps to QUALIFY ROW_NUMBER",
			args:    `{"construct": "ROWNUM", "source_dialect": "oracle"}`,
			wantSub: []string{"ROWNUM", "ROW_NUMBER"},
		},
		{
			name:    "tsql DATEADD documents arg order",
			args:    `{"construct": "DATEADD", "source_dialect": "tsql"}`,
			wantSub: []string{"DATEADD", "DATEDIFF"},
		},
		{
			name:    "string concatenation covers both dialects",
			args:    `{"construct": "concatenation", "source_dialect": "tsql"}`,
			wantSub: []string{"||"},
		},
		{
			name:    "NVL is oracle-only, not tsql",
			args:    `{"construct": "NVL", "source_dialect": "tsql"}`,
			wantErr: true,
		},
		{
			name:    "unknown construct errors",
			args:    `{"construct": "FRABBLE", "source_dialect": "tsql"}`,
			wantErr: true,
		},
		{
			name:    "unknown dialect errors",
			args:    `{"construct": "TOP", "source_dialect": "mysql"}`,
			wantErr: true,
		},
		{
			name:    "missing construct errors",
			args:    `{"source_dialect": "tsql"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := invoke(t, tool, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got output %q", out)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantSub {
				if !strings.Contains(out, want) {
					t.Fatalf("output %q missing %q", out, want)
				}
			}
		})
	}
}

func TestDryRun(t *testing.T) {
	tool := &DryRun{Target: seededDuckDB(t)}

	tests := []struct {
		name    string
		args    string
		wantSub []string
		wantErr bool
	}{
		{
			name:    "valid select explains",
			args:    `{"sql": "SELECT count(*) FROM orders"}`,
			wantSub: []string{"orders"},
		},
		{
			name:    "parse errors are returned verbatim",
			args:    `{"sql": "SELEC count(*) FROM orders"}`,
			wantErr: true,
		},
		{
			name:    "missing table errors",
			args:    `{"sql": "SELECT * FROM no_such_table"}`,
			wantErr: true,
		},
		{
			name:    "non-select is refused",
			args:    `{"sql": "DELETE FROM orders"}`,
			wantErr: true,
		},
		{
			name:    "multi-statement is refused",
			args:    `{"sql": "SELECT 1; SELECT 2"}`,
			wantErr: true,
		},
		{
			name:    "empty sql errors",
			args:    `{"sql": ""}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := invoke(t, tool, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got output %q", out)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantSub {
				if !strings.Contains(out, want) {
					t.Fatalf("output %q missing %q", out, want)
				}
			}
		})
	}
}

func TestExecuteAndDiff(t *testing.T) {
	dk := seededDuckDB(t)
	tool := &ExecuteAndDiff{Source: dk, Target: dk}

	tests := []struct {
		name    string
		args    string
		wantSub []string
		wantErr bool
	}{
		{
			name:    "identical queries diff clean",
			args:    `{"source_sql": "SELECT count(*) FROM orders", "target_sql": "SELECT count(*) FROM orders"}`,
			wantSub: []string{"OK"},
		},
		{
			name:    "differing results report mismatch",
			args:    `{"source_sql": "SELECT count(*) FROM orders", "target_sql": "SELECT count(*) FROM region"}`,
			wantSub: []string{"row 0", "i:200", "i:5"},
		},
		{
			name:    "target engine error surfaces",
			args:    `{"source_sql": "SELECT count(*) FROM orders", "target_sql": "SELEC 1"}`,
			wantErr: true,
		},
		{
			name:    "missing args error",
			args:    `{"source_sql": "SELECT 1"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := invoke(t, tool, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got output %q", out)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantSub {
				if !strings.Contains(out, want) {
					t.Fatalf("output %q missing %q", out, want)
				}
			}
		})
	}
}

func TestRegistryDispatch(t *testing.T) {
	lookup := &SchemaLookup{Schema: testSchema()}
	dialectRef, err := NewDialectRef()
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(lookup, dialectRef)

	if len(r.Names()) != 2 {
		t.Fatalf("expected 2 registered tools, got %v", r.Names())
	}
	if _, ok := r.Get("schema_lookup"); !ok {
		t.Fatal("schema_lookup not found")
	}
	if _, ok := r.Get("does_not_exist"); ok {
		t.Fatal("unexpected tool found")
	}

	out, err := r.Invoke(context.Background(), "schema_lookup", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "orders") {
		t.Fatalf("unexpected registry dispatch output: %q", out)
	}

	if _, err := r.Invoke(context.Background(), "nope", nil); err == nil {
		t.Fatal("expected unknown-tool error")
	}
}

func TestToolSchemasAreValidJSON(t *testing.T) {
	lookup := &SchemaLookup{Schema: testSchema()}
	dialectRef, err := NewDialectRef()
	if err != nil {
		t.Fatal(err)
	}
	dk := seededDuckDB(t)
	for _, tool := range []Tool{
		lookup, dialectRef, &DryRun{Target: dk}, &ExecuteAndDiff{Source: dk, Target: dk},
	} {
		var v any
		if err := json.Unmarshal(tool.JSONSchema(), &v); err != nil {
			t.Fatalf("%s: JSONSchema is not valid JSON: %v", tool.Name(), err)
		}
		if tool.Name() == "" || tool.Description() == "" {
			t.Fatalf("tool %q missing name or description", tool.Name())
		}
	}
}
