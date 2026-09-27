package schema

import (
	"context"
	"testing"

	"sqlagent/internal/exec"
)

// TestLoadIntrospectsSeedSchema verifies information_schema introspection
// against the docker-compose Postgres. Skipped when Postgres is unreachable.
func TestLoadIntrospectsSeedSchema(t *testing.T) {
	s, err := Load(context.Background(), exec.DefaultPostgresDSN(), "public")
	if err != nil {
		t.Skipf("postgres not reachable (run `make up` + `make seed`): %v", err)
	}

	want := []string{"customer", "lineitem", "nation", "orders", "region"}
	got := map[string]*Table{}
	for i := range s.Tables {
		got[s.Tables[i].Name] = &s.Tables[i]
	}
	for _, name := range want {
		tbl, ok := got[name]
		if !ok {
			t.Fatalf("missing table %q, got %v", name, got)
		}
		if len(tbl.Columns) == 0 {
			t.Fatalf("table %q has no columns", name)
		}
	}

	orders := s.TableByName("orders")
	if orders == nil {
		t.Fatal("orders table missing")
	}
	if orders.Columns[0].Name != "o_orderkey" {
		t.Fatalf("unexpected first column of orders: %+v", orders.Columns[0])
	}
}
