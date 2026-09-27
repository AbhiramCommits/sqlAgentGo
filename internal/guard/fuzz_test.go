package guard

import (
	"strings"
	"testing"

	"github.com/auxten/postgresql-parser/pkg/sql/parser"
)

// FuzzCheckClosed is the safety net for the guard's identifier extraction:
// for any input it must never panic, and it must fail closed — the only way
// to get zero violations is for the SQL to actually parse.
//
// Run with: go test ./internal/guard -fuzz FuzzCheckClosed -fuzztime 30s
func FuzzCheckClosed(f *testing.F) {
	seeds := []string{
		"SELECT o_orderkey FROM orders",
		"SELECT o_bogus FROM orders",
		"WITH t AS (SELECT 1 AS x) SELECT x FROM t",
		"SELECT * FROM orders",
		"SELECT FROM WHERE",
		"SELECT 'unterminated",
		"SELECT o_orderkey FROM orders WHERE o_totalprice > '9000.00'",
		"SELECT c.c_name, (SELECT count(*) FROM orders o WHERE o.o_custkey = c.c_custkey) FROM customer c",
		"SELECT * FROM `weird` identifiers",
		"",
		"SELECT 1; SELECT 2; SELECT 3",
		"INSERT INTO nope (a) VALUES (1)",
		"-- only a comment",
		"WITH RECURSIVE r AS (SELECT 1 UNION ALL SELECT x+1 FROM r WHERE x < 10) SELECT * FROM r",
		"SELECT o.* FROM orders o ORDER BY o.o_orderdate DESC NULLS LAST",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, sql string) {
		// Must never panic on malformed input.
		viols := CheckClosed(sql, testSchema())

		// Fail closed: zero violations is only allowed when the SQL parses.
		if len(viols) == 0 {
			if _, err := parser.Parse(sql); err != nil {
				t.Fatalf("guard silently passed unparseable SQL %q: %v", sql, err)
			}
		}
		// Parse violations must carry the reason.
		for _, v := range viols {
			if v.Kind == KindParse && v.Suggestion == "" {
				t.Fatalf("parse violation without suggestion for %q", sql)
			}
			// All outputs must be renderable (greppable trace safety).
			if s := v.String(); s == "" || strings.ContainsAny(s, "\x00") {
				t.Fatalf("unrenderable violation for %q: %q", sql, s)
			}
		}
	})
}
