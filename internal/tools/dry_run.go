package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"sqlagent/internal/exec"
)

// DryRun validates SQL against the target engine with EXPLAIN. It never
// executes the statement, so it cannot mutate data.
type DryRun struct {
	Target exec.Executor
}

// Name implements Tool.
func (d *DryRun) Name() string { return "dry_run" }

// Description implements Tool.
func (d *DryRun) Description() string {
	return "EXPLAIN a candidate SELECT against the target engine; returns parse/plan errors verbatim without executing anything."
}

// JSONSchema implements Tool.
func (d *DryRun) JSONSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["sql"],
  "properties": {
    "sql": {
      "type": "string",
      "description": "The candidate SELECT statement to validate"
    }
  }
}`)
}

// Invoke implements Tool.
func (d *DryRun) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	if d.Target == nil {
		return "", fmt.Errorf("dry_run has no target executor")
	}
	var req struct {
		SQL string `json:"sql"`
	}
	if err := unmarshalArgs(args, &req); err != nil {
		return "", err
	}
	sql := strings.TrimSpace(req.SQL)
	if sql == "" {
		return "", fmt.Errorf("sql is required")
	}
	if err := requireSingleStatement(sql); err != nil {
		return "", err
	}
	if !isReadOnly(sql) {
		return "", fmt.Errorf("refusing to dry-run non-SELECT statement (dry_run never mutates)")
	}

	rs, err := d.Target.Run(ctx, "EXPLAIN "+sql)
	if err != nil {
		// Parse/plan errors are returned verbatim; no wrapping.
		return "", err
	}
	return formatResultSet(rs, 20), nil
}

// requireSingleStatement rejects multi-statement scripts.
func requireSingleStatement(sql string) error {
	stmts, err := exec.SplitStatements(sql)
	if err != nil {
		return fmt.Errorf("invalid SQL: %w", err)
	}
	if len(stmts) != 1 {
		return fmt.Errorf("exactly one statement required, got %d", len(stmts))
	}
	return nil
}

// isReadOnly checks that the first keyword is SELECT or WITH.
func isReadOnly(sql string) bool {
	first := strings.ToUpper(firstKeyword(sql))
	return first == "SELECT" || first == "WITH"
}

// firstKeyword strips leading whitespace and comments to find the first word.
func firstKeyword(sql string) string {
	for i := 0; i < len(sql); {
		switch {
		case sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r':
			i++
		case strings.HasPrefix(sql[i:], "--"):
			if j := strings.IndexByte(sql[i:], '\n'); j >= 0 {
				i += j
			} else {
				return ""
			}
		case strings.HasPrefix(sql[i:], "/*"):
			if j := strings.Index(sql[i:], "*/"); j >= 0 {
				i += j + 2
			} else {
				return ""
			}
		default:
			j := i
			for j < len(sql) && (isAlpha(sql[j]) || sql[j] == '_') {
				j++
			}
			return sql[i:j]
		}
	}
	return ""
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// formatResultSet renders a result set as a compact table, capped at maxRows.
func formatResultSet(rs exec.ResultSet, maxRows int) string {
	var b strings.Builder
	if len(rs.Cols) > 0 {
		b.WriteString(strings.Join(rs.Cols, " | "))
		b.WriteString("\n")
	}
	for i, row := range rs.Rows {
		if i >= maxRows {
			fmt.Fprintf(&b, "... (%d more rows)\n", len(rs.Rows)-maxRows)
			break
		}
		b.WriteString(formatCells(row))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatCells(row []any) string {
	parts := make([]string, len(row))
	for i, v := range row {
		switch t := v.(type) {
		case nil:
			parts[i] = "NULL"
		case string:
			parts[i] = t
		default:
			parts[i] = fmt.Sprintf("%v", t)
		}
	}
	return strings.Join(parts, " | ")
}
