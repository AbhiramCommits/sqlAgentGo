package exec

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGoldens rewrites the .golden files when -update is passed:
// go test ./internal/exec -run TestDiffGolden -update
var updateGoldens = flag.Bool("update", false, "update golden files")

// TestDiffGolden pins the human-readable Diff report for the five
// differential-comparison scenarios that matter most: row reordering, float
// tolerance, NULL vs empty string, differing column counts, and differing
// column order.
func TestDiffGolden(t *testing.T) {
	cases := []struct {
		name string
		a, b ResultSet
	}{
		{
			name: "row_reordering",
			a:    ResultSet{Cols: []string{"k", "v"}, Rows: [][]any{{int64(1), "a"}, {int64(2), "b"}}},
			b:    ResultSet{Cols: []string{"k", "v"}, Rows: [][]any{{int64(2), "b"}, {int64(1), "a"}}},
		},
		{
			name: "float_tolerance",
			a:    ResultSet{Cols: []string{"x"}, Rows: [][]any{{1.0000000001}, {2.5}}},
			b:    ResultSet{Cols: []string{"x"}, Rows: [][]any{{1.0000000002}, {2.5}}},
		},
		{
			name: "null_vs_empty_string",
			a:    ResultSet{Cols: []string{"x"}, Rows: [][]any{{nil}}},
			b:    ResultSet{Cols: []string{"x"}, Rows: [][]any{{""}}},
		},
		{
			name: "column_count_diff",
			a:    ResultSet{Cols: []string{"a", "b"}, Rows: [][]any{{int64(1), int64(2)}}},
			b:    ResultSet{Cols: []string{"a"}, Rows: [][]any{{int64(1)}}},
		},
		{
			name: "column_order_diff",
			a:    ResultSet{Cols: []string{"a", "b"}, Rows: [][]any{{int64(1), int64(2)}}},
			b:    ResultSet{Cols: []string{"b", "a"}, Rows: [][]any{{int64(2), int64(1)}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := Diff(tc.a, tc.b).String()
			path := filepath.Join("testdata", "golden", tc.name+".golden")
			if *updateGoldens {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(report+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update to regenerate)", path, err)
			}
			want := strings.TrimRight(string(data), "\n")
			if report != want {
				t.Fatalf("Diff report mismatch:\n--- got ---\n%s\n--- want (%s) ---\n%s", report, path, want)
			}
		})
	}
}

// TestDiffGoldenDeterminism guards against flaky report formatting: diffing
// the same pair twice must yield byte-identical reports.
func TestDiffGoldenDeterminism(t *testing.T) {
	a := ResultSet{Cols: []string{"k", "v"}, Rows: [][]any{{int64(1), "a"}, {int64(3), nil}}}
	b := ResultSet{Cols: []string{"k", "v"}, Rows: [][]any{{int64(3), ""}, {int64(2), "b"}}}
	first := Diff(a, b).String()
	for i := 0; i < 5; i++ {
		if got := Diff(a, b).String(); got != first {
			t.Fatalf("non-deterministic report:\n%s\nvs\n%s", first, got)
		}
	}
}
