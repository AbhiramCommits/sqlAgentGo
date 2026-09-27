package guard

import (
	"strings"
	"testing"

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

func TestCheckGrounded(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantViols int
		wantKind  ViolationKind
		wantIdent string // substring match on Identifier
		wantSugg  string // substring match on Suggestion, if non-empty
	}{
		{
			name:      "simple grounded query",
			sql:       "SELECT o_orderkey, o_totalprice FROM orders",
			wantViols: 0,
		},
		{
			name:      "hallucinated column",
			sql:       "SELECT o_orderkey, o_bogus FROM orders",
			wantViols: 1,
			wantKind:  KindColumn,
			wantIdent: "o_bogus",
			wantSugg:  "orders.",
		},
		{
			name:      "typo column suggests nearest column",
			sql:       "SELECT o_orderkye FROM orders",
			wantViols: 1,
			wantKind:  KindColumn,
			wantIdent: "o_orderkye",
			wantSugg:  "orders.o_orderkey",
		},
		{
			name:      "hallucinated table",
			sql:       "SELECT * FROM orders_nope",
			wantViols: 1,
			wantKind:  KindTable,
			wantIdent: "orders_nope",
			wantSugg:  "orders",
		},
		{
			name:      "CTE outputs must not be flagged",
			sql:       "WITH t AS (SELECT o_orderkey AS k, o_totalprice + 1 AS p FROM orders) SELECT k, p FROM t",
			wantViols: 0,
		},
		{
			name:      "CTE with select star",
			sql:       "WITH t AS (SELECT * FROM orders) SELECT t.o_orderkey FROM t",
			wantViols: 0,
		},
		{
			name:      "CTE shadows a schema table",
			sql:       "WITH orders AS (SELECT 1 AS x) SELECT x FROM orders",
			wantViols: 0,
		},
		{
			name:      "alias qualified columns",
			sql:       "SELECT c.c_name, o.o_orderkey FROM customer AS c JOIN orders o ON c.c_custkey = o.o_custkey",
			wantViols: 0,
		},
		{
			name:      "alias with hallucinated column",
			sql:       "SELECT c.c_bogus FROM customer c",
			wantViols: 1,
			wantKind:  KindColumn,
			wantIdent: "c.c_bogus",
			wantSugg:  "customer.",
		},
		{
			name:      "alias typo suggests nearest column",
			sql:       "SELECT c.c_custkye FROM customer c",
			wantViols: 1,
			wantKind:  KindColumn,
			wantIdent: "c.c_custkye",
			wantSugg:  "customer.c_custkey",
		},
		{
			name:      "select star from grounded table",
			sql:       "SELECT * FROM orders",
			wantViols: 0,
		},
		{
			name:      "select star from hallucinated table",
			sql:       "SELECT * FROM missing_table",
			wantViols: 1,
			wantKind:  KindTable,
			wantIdent: "missing_table",
		},
		{
			name:      "qualified star via alias",
			sql:       "SELECT o.* FROM orders o",
			wantViols: 0,
		},
		{
			name:      "qualified star via real table name",
			sql:       "SELECT orders.* FROM orders",
			wantViols: 0,
		},
		{
			name:      "qualified star on unknown qualifier",
			sql:       "SELECT nope.* FROM orders o",
			wantViols: 1,
			wantKind:  KindTable,
			wantIdent: "nope.*",
		},
		{
			name:      "unqualified existing column",
			sql:       "SELECT c_name FROM customer",
			wantViols: 0,
		},
		{
			name:      "unqualified hallucinated column",
			sql:       "SELECT c_bogus FROM customer",
			wantViols: 1,
			wantKind:  KindColumn,
			wantIdent: "c_bogus",
			wantSugg:  "customer.",
		},
		{
			name:      "order by select alias",
			sql:       "SELECT o_orderkey AS k FROM orders ORDER BY k",
			wantViols: 0,
		},
		{
			name:      "group by select alias",
			sql:       "SELECT o_orderstatus AS s, count(*) FROM orders GROUP BY s",
			wantViols: 0,
		},
		{
			name:      "correlated subquery",
			sql:       "SELECT c.c_name FROM customer c WHERE EXISTS (SELECT 1 FROM orders o WHERE o.o_custkey = c.c_custkey)",
			wantViols: 0,
		},
		{
			name:      "from subquery with alias",
			sql:       "SELECT x.k FROM (SELECT o_orderkey AS k FROM orders) x",
			wantViols: 0,
		},
		{
			name:      "duplicate references flagged once",
			sql:       "SELECT o_bogus FROM orders WHERE o_bogus > 1 AND o_bogus < 5",
			wantViols: 1,
			wantKind:  KindColumn,
			wantIdent: "o_bogus",
		},
		{
			name:      "union branches share schema grounding",
			sql:       "SELECT o_orderkey FROM orders UNION ALL SELECT c_custkey FROM customer",
			wantViols: 0,
		},
		{
			name:      "unknown qualifier on column",
			sql:       "SELECT bogus.o_orderkey FROM orders",
			wantViols: 1,
			wantKind:  KindTable,
			wantIdent: "bogus",
		},
		{
			name:      "aggregate with star",
			sql:       "SELECT count(*) FROM lineitem",
			wantViols: 0,
		},
		{
			name:      "cte outputs ground unqualified columns",
			sql:       "WITH ranked AS (SELECT o_orderkey, o_totalprice FROM orders) SELECT o_orderkey FROM ranked",
			wantViols: 0,
		},
		{
			name:      "anonymous subquery outputs are visible",
			sql:       "SELECT o_orderkey, rn FROM (SELECT o_orderkey, ROW_NUMBER() OVER (ORDER BY o_totalprice DESC) AS rn FROM orders) WHERE rn <= 10",
			wantViols: 0,
		},
		{
			name:      "order by without qualifier",
			sql:       "SELECT o_orderkey FROM orders ORDER BY o_orderkey DESC",
			wantViols: 0,
		},
		{
			name:      "qualified order by with descending",
			sql:       "SELECT c.c_name, o.o_orderkey FROM customer c LEFT JOIN orders o ON o.o_custkey = c.c_custkey ORDER BY c.c_custkey, o.o_orderkey DESC",
			wantViols: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viols, err := CheckGrounded(tt.sql, testSchema())
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if len(viols) != tt.wantViols {
				t.Fatalf("got %d violations, want %d: %v", len(viols), tt.wantViols, viols)
			}
			if tt.wantViols == 0 {
				return
			}
			v := viols[0]
			if tt.wantKind != "" && v.Kind != tt.wantKind {
				t.Fatalf("violation kind = %q, want %q", v.Kind, tt.wantKind)
			}
			if tt.wantIdent != "" && !strings.Contains(v.Identifier, tt.wantIdent) {
				t.Fatalf("identifier = %q, want substring %q", v.Identifier, tt.wantIdent)
			}
			if tt.wantSugg != "" && !strings.Contains(v.Suggestion, tt.wantSugg) {
				t.Fatalf("suggestion = %q, want substring %q", v.Suggestion, tt.wantSugg)
			}
		})
	}
}

func TestCheckGroundedParseError(t *testing.T) {
	if _, err := CheckGrounded("SELECT FROM WHERE", testSchema()); err == nil {
		t.Fatal("expected parse error for invalid SQL")
	}
}

func TestCheckGroundedSkipsNonSelect(t *testing.T) {
	viols, err := CheckGrounded("DELETE FROM nope", testSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(viols) != 0 {
		t.Fatalf("expected non-SELECT statements to be skipped, got %v", viols)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"kitten", "sitting", 3},
		{"o_bogus", "o_orderkey", 8},
		{"o_ordrkey", "o_orderkey", 1},
		{"o_orderkye", "o_orderkey", 2},
		{"", "abc", 3},
		{"abc", "abc", 0},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Fatalf("levenshtein(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
