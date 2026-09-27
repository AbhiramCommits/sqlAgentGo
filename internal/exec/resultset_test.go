package exec

import (
	"strings"
	"testing"
)

func TestDiffEqualEmpty(t *testing.T) {
	r := Diff(ResultSet{}, ResultSet{})
	if !r.Equal {
		t.Fatalf("expected equal, got: %s", r.String())
	}
}

func TestDiffOrderInsensitive(t *testing.T) {
	a := ResultSet{Cols: []string{"k", "v"}, Rows: [][]any{
		{int64(1), "a"}, {int64(2), "b"},
	}}
	b := ResultSet{Cols: []string{"k", "v"}, Rows: [][]any{
		{int64(2), "b"}, {int64(1), "a"},
	}}
	if r := Diff(a, b); !r.Equal {
		t.Fatalf("expected order-insensitive equality, got: %s", r.String())
	}
}

func TestDiffFloatEpsilon(t *testing.T) {
	a := ResultSet{Cols: []string{"x"}, Rows: [][]any{{1.0000000001}}}
	b := ResultSet{Cols: []string{"x"}, Rows: [][]any{{1.0000000002}}}
	if r := Diff(a, b); !r.Equal {
		t.Fatalf("expected epsilon equality, got: %s", r.String())
	}

	a = ResultSet{Cols: []string{"x"}, Rows: [][]any{{1.0}}}
	b = ResultSet{Cols: []string{"x"}, Rows: [][]any{{1.5}}}
	if r := Diff(a, b); r.Equal {
		t.Fatal("expected mismatch beyond epsilon")
	}
}

func TestDiffNullAware(t *testing.T) {
	a := ResultSet{Cols: []string{"x"}, Rows: [][]any{{nil}, {"v"}}}
	b := ResultSet{Cols: []string{"x"}, Rows: [][]any{{"v"}, {nil}}}
	if r := Diff(a, b); !r.Equal {
		t.Fatalf("expected NULL equality, got: %s", r.String())
	}

	a = ResultSet{Cols: []string{"x"}, Rows: [][]any{{nil}}}
	b = ResultSet{Cols: []string{"x"}, Rows: [][]any{{""}}}
	if r := Diff(a, b); r.Equal {
		t.Fatal("expected NULL != empty string")
	}
}

func TestDiffIntVsFloatWithinEpsilon(t *testing.T) {
	a := ResultSet{Cols: []string{"x"}, Rows: [][]any{{int64(100)}}}
	b := ResultSet{Cols: []string{"x"}, Rows: [][]any{{float64(100.0)}}}
	if r := Diff(a, b); !r.Equal {
		t.Fatalf("expected int/float numeric equality, got: %s", r.String())
	}
}

func TestDiffSchemaMismatch(t *testing.T) {
	a := ResultSet{Cols: []string{"x"}, Rows: [][]any{{int64(1)}}}
	b := ResultSet{Cols: []string{"y"}, Rows: [][]any{{int64(1)}}}
	r := Diff(a, b)
	if r.Equal {
		t.Fatal("expected schema mismatch")
	}
	if r.SchemaDiff == "" {
		t.Fatal("expected schema diff message")
	}
}

func TestDiffRowCountMismatchReport(t *testing.T) {
	a := ResultSet{Cols: []string{"x"}, Rows: [][]any{{int64(1)}, {int64(2)}}}
	b := ResultSet{Cols: []string{"x"}, Rows: [][]any{{int64(1)}}}
	r := Diff(a, b)
	if r.Equal {
		t.Fatal("expected mismatch")
	}
	if r.RowCountLeft != 2 || r.RowCountRight != 1 {
		t.Fatalf("unexpected counts: %+v", r)
	}
	s := r.String()
	if !strings.Contains(s, "only on left") {
		t.Fatalf("expected extra-row detail in report, got: %s", s)
	}
}

func TestDiffColumnMismatchDetail(t *testing.T) {
	a := ResultSet{Cols: []string{"k", "v"}, Rows: [][]any{{int64(1), "a"}}}
	b := ResultSet{Cols: []string{"k", "v"}, Rows: [][]any{{int64(1), "b"}}}
	r := Diff(a, b)
	if r.Equal {
		t.Fatal("expected mismatch")
	}
	if !strings.Contains(r.String(), "v: s:a != s:b") {
		t.Fatalf("expected column detail, got: %s", r.String())
	}
}

func TestDiffMismatchTruncation(t *testing.T) {
	left := ResultSet{Cols: []string{"x"}}
	right := ResultSet{Cols: []string{"x"}}
	for i := 0; i < maxReportedMismatches+10; i++ {
		left.Rows = append(left.Rows, []any{int64(i + 1)})
		right.Rows = append(right.Rows, []any{int64((i + 1) * 2)})
	}
	r := Diff(left, right)
	if r.Equal {
		t.Fatal("expected mismatch")
	}
	if len(r.Mismatches) != maxReportedMismatches {
		t.Fatalf("expected %d reported mismatches, got %d", maxReportedMismatches, len(r.Mismatches))
	}
	if r.truncatedMismatches != 10 {
		t.Fatalf("expected 10 omitted mismatches, got %d", r.truncatedMismatches)
	}
}

func TestSplitStatements(t *testing.T) {
	script := `
-- a comment with a ; inside
CREATE TABLE t (x INT);
INSERT INTO t VALUES (1), (2);
INSERT INTO t VALUES ('a;b');
`
	stmts, err := SplitStatements(script)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d: %v", len(stmts), stmts)
	}
	if !strings.HasPrefix(stmts[0], "CREATE TABLE") {
		t.Fatalf("unexpected stmt 0: %q", stmts[0])
	}
	if !strings.Contains(stmts[2], "'a;b'") {
		t.Fatalf("semicolon inside string literal mishandled: %q", stmts[2])
	}
}

func TestSplitStatementsUnterminatedString(t *testing.T) {
	if _, err := SplitStatements("SELECT 'oops"); err == nil {
		t.Fatal("expected error for unterminated string")
	}
}
