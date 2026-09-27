package corpus

import (
	"path/filepath"
	"testing"
)

func TestLoadDir(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "cases")
	c, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("expected at least one case")
	}
	for _, tc := range c.Cases {
		if tc.Name == "" || tc.Dialect == "" || tc.Query == "" {
			t.Fatalf("malformed case: %+v", tc)
		}
		switch tc.Dialect {
		case "tsql", "oracle":
		default:
			t.Fatalf("unexpected dialect %q in case %q", tc.Dialect, tc.Name)
		}
	}
}
