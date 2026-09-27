package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sqlagent/internal/schema"
)

// SchemaLookup reports tables and columns from the introspected schema.
type SchemaLookup struct {
	Schema *schema.Schema
}

// Name implements Tool.
func (s *SchemaLookup) Name() string { return "schema_lookup" }

// Description implements Tool.
func (s *SchemaLookup) Description() string {
	return "List schema tables, or inspect the columns and types of one table."
}

// JSONSchema implements Tool.
func (s *SchemaLookup) JSONSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "table": {
      "type": "string",
      "description": "Table name to inspect. Omit to list table names only."
    }
  }
}`)
}

// Invoke implements Tool.
func (s *SchemaLookup) Invoke(_ context.Context, args json.RawMessage) (string, error) {
	if s.Schema == nil {
		return "", fmt.Errorf("schema_lookup has no schema loaded")
	}
	var req struct {
		Table string `json:"table"`
	}
	if err := unmarshalArgs(args, &req); err != nil {
		return "", err
	}

	if strings.TrimSpace(req.Table) == "" {
		names := make([]string, 0, len(s.Schema.Tables))
		for _, t := range s.Schema.Tables {
			names = append(names, t.Name)
		}
		sort.Strings(names)
		return "tables: " + strings.Join(names, ", "), nil
	}

	tbl := s.Schema.TableByName(strings.TrimSpace(req.Table))
	if tbl == nil {
		var names []string
		for _, t := range s.Schema.Tables {
			names = append(names, t.Name)
		}
		sort.Strings(names)
		return "", fmt.Errorf("table %q not found; available tables: %s", req.Table, strings.Join(names, ", "))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "table %s (%d columns):\n", tbl.Name, len(tbl.Columns))
	for _, c := range tbl.Columns {
		fmt.Fprintf(&b, "  %s %s\n", c.Name, c.Type)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
