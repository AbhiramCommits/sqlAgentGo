package exec

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// ResultSet is a materialized query result: ordered column names and rows.
type ResultSet struct {
	Cols []string
	Rows [][]any
}

// maxReportedMismatches caps the per-report mismatch detail lines.
const maxReportedMismatches = 20

// floatEpsilon is the absolute tolerance used when comparing floats.
const floatEpsilon = 1e-9

// DiffReport describes the outcome of comparing two ResultSets.
type DiffReport struct {
	Equal               bool
	SchemaDiff          string // "" when column names match
	RowCountLeft        int
	RowCountRight       int
	ColsLeft            int
	ColsRight           int
	Mismatches          []string
	truncatedMismatches int
}

// String renders a human-readable report.
func (r DiffReport) String() string {
	if r.Equal {
		return fmt.Sprintf("OK: %d rows x %d columns match", r.RowCountLeft, r.ColsLeft)
	}
	var b strings.Builder
	if r.SchemaDiff != "" {
		b.WriteString("schema mismatch: ")
		b.WriteString(r.SchemaDiff)
		b.WriteString("\n")
	}
	if r.RowCountLeft != r.RowCountRight {
		fmt.Fprintf(&b, "row count differs: left=%d right=%d\n", r.RowCountLeft, r.RowCountRight)
	}
	for _, m := range r.Mismatches {
		b.WriteString(m)
		b.WriteString("\n")
	}
	if r.truncatedMismatches > 0 {
		fmt.Fprintf(&b, "... (%d more mismatches omitted)\n", r.truncatedMismatches)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Diff compares two ResultSets in an order-insensitive fashion: rows are
// sorted by a canonical key, floats are compared with epsilon, NULLs compare
// only equal to NULL, and the report lists the first N mismatches.
func Diff(a, b ResultSet) DiffReport {
	rep := DiffReport{
		RowCountLeft:  len(a.Rows),
		RowCountRight: len(b.Rows),
		ColsLeft:      len(a.Cols),
		ColsRight:     len(b.Cols),
	}

	if !slices.Equal(a.Cols, b.Cols) {
		rep.SchemaDiff = fmt.Sprintf("left cols %v != right cols %v", a.Cols, b.Cols)
	}

	left := sortedRows(a)
	right := sortedRows(b)

	n := min(len(left), len(right))
	for i := 0; i < n; i++ {
		detail := rowMismatch(i, a.Cols, left[i], right[i])
		if detail == "" {
			continue
		}
		rep.record(detail)
	}
	if len(left) > n {
		for _, row := range left[n:] {
			rep.record(fmt.Sprintf("row %d: only on left: %v", n, formatCells(row.row)))
		}
	}
	if len(right) > n {
		for _, row := range right[n:] {
			rep.record(fmt.Sprintf("row %d: only on right: %v", n, formatCells(row.row)))
		}
	}

	rep.Equal = rep.SchemaDiff == "" &&
		rep.RowCountLeft == rep.RowCountRight &&
		len(rep.Mismatches) == 0
	return rep
}

func (r *DiffReport) record(line string) {
	if len(r.Mismatches) < maxReportedMismatches {
		r.Mismatches = append(r.Mismatches, line)
	} else {
		r.truncatedMismatches++
	}
}

type sortedRow struct {
	key string
	row []any
}

func sortedRows(rs ResultSet) []sortedRow {
	out := make([]sortedRow, len(rs.Rows))
	for i, row := range rs.Rows {
		out[i] = sortedRow{key: canonicalKey(row), row: row}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// rowMismatch returns "" when the two rows are equal, otherwise a readable
// per-column mismatch description.
func rowMismatch(idx int, cols []string, l, r sortedRow) string {
	var parts []string
	n := max(len(l.row), len(r.row))
	for c := 0; c < n; c++ {
		var lv, rv any
		var lok, rok bool
		if c < len(l.row) {
			lv, lok = l.row[c], true
		}
		if c < len(r.row) {
			rv, rok = r.row[c], true
		}
		if lok != rok || !cellEqual(lv, rv) {
			colName := fmt.Sprintf("col %d", c)
			if c < len(cols) {
				colName = cols[c]
			}
			parts = append(parts, fmt.Sprintf("%s: %s != %s", colName, formatCell(lv), formatCell(rv)))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("row %d: %s", idx, strings.Join(parts, ", "))
}

func cellEqual(a, b any) bool {
	an, bn := normalize(a), normalize(b)
	if an == nil || bn == nil {
		return an == nil && bn == nil
	}
	af, aok := an.(float64)
	bf, bok := bn.(float64)
	if aok && bok {
		return math.Abs(af-bf) <= floatEpsilon
	}
	ai, aiok := an.(int64)
	bi, biok := bn.(int64)
	if aiok && biok {
		return ai == bi
	}
	if aok && biok {
		return math.Abs(af-float64(bi)) <= floatEpsilon
	}
	if aiok && bok {
		return math.Abs(float64(ai)-bf) <= floatEpsilon
	}
	return an == bn
}

// canonicalKey renders a row as a sortable string with type prefixes so that
// equal rows always produce equal keys.
func canonicalKey(row []any) string {
	var b strings.Builder
	for _, v := range row {
		fmt.Fprintf(&b, "%s\x00", formatCell(v))
	}
	return b.String()
}

func formatCells(row []any) string {
	parts := make([]string, len(row))
	for i, v := range row {
		parts[i] = formatCell(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatCell(v any) string {
	n := normalize(v)
	switch t := n.(type) {
	case nil:
		return "NULL"
	case string:
		return "s:" + t
	case int64:
		return fmt.Sprintf("i:%d", t)
	case float64:
		return fmt.Sprintf("f:%f", t)
	case bool:
		return fmt.Sprintf("b:%t", t)
	default:
		return fmt.Sprintf("o:%v", t)
	}
}

// normalize coerces driver-specific values into comparable Go types:
// all integers to int64, floats to float64, times to UTC RFC3339 strings,
// and NULL to nil.
func normalize(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case int:
		return int64(t)
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case int64:
		return t
	case uint:
		return int64(t)
	case uint8:
		return int64(t)
	case uint16:
		return int64(t)
	case uint32:
		return int64(t)
	case uint64:
		return int64(t)
	case float32:
		return float64(t)
	case float64:
		return t
	case bool:
		return t
	case string:
		return t
	case []byte:
		return string(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	default:
		if f, ok := numericToFloat(t); ok {
			return f
		}
		if tt, ok := timeValue(t); ok {
			return tt.UTC().Format(time.RFC3339Nano)
		}
		if n, ok := duckNormalize(t); ok {
			return n
		}
		return fmt.Sprintf("%v", t)
	}
}
