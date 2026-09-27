// Package guard checks that emitted (translated) SQL stays grounded in the
// introspected schema: every referenced table and column must exist.
//
// Parser choice: github.com/auxten/postgresql-parser (CockroachDB v20.1
// grammar, goyacc-generated pure Go).
//
//   - It is a real PostgreSQL grammar. Snowflake SQL is Postgres-flavored
//     (CASE, CAST, ||, COALESCE, LIMIT, CTEs), so translated statements parse
//     reliably, unlike with a MySQL-grammar parser.
//   - It supports CTEs (WITH ... AS), which this guard must resolve so that
//     CTE-local outputs are not flagged. The alternative,
//     xwb1989/sqlparser, is MySQL-grammar and has no CTE support, which
//     would make the CTE requirement impossible.
//   - Pure Go (no cgo): the only cgo in this repo is already go-duckdb.
//
// Known limitation: Snowflake-specific clauses not in the PostgreSQL grammar
// (notably QUALIFY) will fail to parse; CheckGrounded surfaces that as a
// parse error rather than a violation.
package guard

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/auxten/postgresql-parser/pkg/sql/parser"
	"github.com/auxten/postgresql-parser/pkg/sql/sem/tree"

	"sqlagent/internal/schema"
)

// ViolationKind classifies an ungrounded identifier.
type ViolationKind string

const (
	KindTable  ViolationKind = "table"
	KindColumn ViolationKind = "column"
)

// GroundednessViolation describes one identifier in the SQL that is absent
// from the introspected schema, with a suggested correction.
type GroundednessViolation struct {
	Kind       ViolationKind `json:"kind"`
	Identifier string        `json:"identifier"`
	Suggestion string        `json:"suggestion,omitempty"`
}

func (v GroundednessViolation) String() string {
	s := fmt.Sprintf("%s %q is not present in the schema", v.Kind, v.Identifier)
	if v.Suggestion != "" {
		s += fmt.Sprintf("; did you mean %q?", v.Suggestion)
	}
	return s
}

// starOutputs marks a source whose outputs are unknown-but-unrestricted
// (SELECT * ...): any column reference through it is grounded.
var starOutputs = []string{"*"}

// tableSource is a resolved FROM source: either a schema table, or a
// CTE/subquery whose outputs are known (or nil when unknown).
type tableSource struct {
	name    string
	schemaT *schema.Table
	outputs []string
}

// scopeFrame holds names visible in one SELECT scope: aliases, CTE names,
// referenced schema tables (for unqualified resolution), anonymous subquery
// outputs, and the select list's own output names (so ORDER BY/GROUP BY
// aliases resolve).
type scopeFrame struct {
	sources  map[string]*tableSource
	schemaTs []*schema.Table
	anon     []*tableSource
	selfOut  map[string]bool
	selfStar bool
}

// collector walks a statement AST and records violations.
type collector struct {
	sch        *schema.Schema
	frames     []*scopeFrame
	viols      []GroundednessViolation
	seen       map[string]bool
	tableCache map[*tree.TableName]*tableSource
}

// CheckGrounded parses sql and returns every identifier that is absent from
// sch. CTE outputs, subquery aliases, and select-list aliases are resolved so
// they are not false positives. Statements that are not SELECT-like are
// skipped. A parse error is returned as an error (not a violation).
func CheckGrounded(sql string, sch *schema.Schema) ([]GroundednessViolation, error) {
	stmts, err := parser.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("parse emitted SQL: %w", err)
	}
	c := &collector{
		sch:        sch,
		seen:       map[string]bool{},
		tableCache: map[*tree.TableName]*tableSource{},
	}
	for _, stmt := range stmts {
		switch s := stmt.AST.(type) {
		case *tree.Select, *tree.ParenSelect, *tree.Insert, *tree.CreateTable:
			c.walkNode(s)
		default:
			// Only SELECT-like statements are guarded.
		}
	}
	return c.viols, nil
}

// --- traversal ---------------------------------------------------------

// walkNode is a recursive AST walk with scope threading, adapted from
// auxten/postgresql-parser's pkg/walk switch (which is pre-order only and
// cannot carry scope). Only *tree.Select pushes/pops a scope frame.
func (c *collector) walkNode(n interface{}) {
	if n == nil {
		return
	}
	// The parser hands out typed nils (e.g. (*tree.Where)(nil) for a missing
	// WHERE clause); treat them like plain nils.
	switch v := reflect.ValueOf(n); v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		if v.IsNil() {
			return
		}
	}
	if _, ok := n.(tree.Datum); ok {
		return
	}
	switch node := n.(type) {
	case *tree.Select:
		c.enterSelect(node)
		if node.With != nil {
			c.walkNode(node.With)
		}
		c.walkNode(node.Select)
		c.walkNode(node.OrderBy)
		c.walkNode(node.Limit)
		c.exitSelect()

	case *tree.SelectClause:
		// FROM first so aliases exist before columns are checked.
		for _, t := range node.From.Tables {
			c.walkNode(t)
		}
		// The select list itself is checked strictly: selfOut is only
		// populated AFTER the list, so a hallucinated column cannot hide
		// behind its own alias.
		c.walkNode(node.Exprs)
		c.walkNode(node.Where)
		c.recordSelfOutputs(node)
		c.walkNode(node.GroupBy)
		c.walkNode(node.Having)
		for _, e := range node.DistinctOn {
			c.walkNode(e)
		}
		for _, wd := range node.Window {
			c.walkNode(wd)
		}

	case *tree.UnionClause:
		c.walkNode(node.Left)
		c.walkNode(node.Right)

	case *tree.ParenSelect:
		c.walkNode(node.Select)

	case *tree.With:
		for _, cte := range node.CTEList {
			c.walkNode(cte)
		}

	case *tree.CTE:
		c.walkNode(node.Stmt)

	case *tree.AliasedTableExpr:
		c.walkNode(node.Expr)
		c.registerAlias(node)

	case *tree.JoinTableExpr:
		c.walkNode(node.Left)
		c.walkNode(node.Right)
		c.walkNode(node.Cond)

	case *tree.OnJoinCond:
		c.walkNode(node.Expr)

	case *tree.From:
		for _, t := range node.Tables {
			c.walkNode(t)
		}

	case *tree.TableName:
		c.visitTableName(node)

	case tree.TableName:
		t := node
		c.visitTableName(&t)

	case *tree.UnresolvedName:
		c.visitUnresolvedName(node)

	case tree.UnqualifiedStar:
		// Bare *: grounded if the FROM table was grounded (checked elsewhere).

	case *tree.Subquery:
		c.walkNode(node.Select)

	case tree.SelectExprs:
		for _, e := range node {
			c.walkNode(e)
		}

	case tree.SelectExpr:
		c.walkNode(node.Expr)

	case *tree.Where:
		c.walkNode(node.Expr)

	case tree.Exprs:
		for _, e := range node {
			c.walkNode(e)
		}

	case *tree.Order:
		c.walkNode(node.Expr)
		c.walkNode(node.Table)

	case tree.OrderBy:
		for _, o := range node {
			c.walkNode(o)
		}

	case *tree.Limit:
		c.walkNode(node.Count)

	case tree.GroupBy:
		for _, e := range node {
			c.walkNode(e)
		}

	case tree.DistinctOn:
		for _, e := range node {
			c.walkNode(e)
		}

	case *tree.ValuesClause:
		for _, r := range node.Rows {
			c.walkNode(r)
		}

	case *tree.Insert:
		c.walkNode(node.With)
		c.walkNode(node.Table)
		if node.Rows != nil {
			c.walkNode(node.Rows)
		}

	case *tree.CreateTable:
		if node.AsSource != nil {
			c.walkNode(node.AsSource)
		}

	case *tree.FuncExpr:
		if node.WindowDef != nil {
			c.walkNode(node.WindowDef)
		}
		c.walkNode(node.Exprs)
		c.walkNode(node.Filter)

	case *tree.WindowDef:
		c.walkNode(node.Partitions)
		if node.Frame != nil {
			c.walkNode(node.Frame)
		}

	case *tree.WindowFrame:
		if node.Bounds.StartBound != nil {
			c.walkNode(node.Bounds.StartBound)
		}
		if node.Bounds.EndBound != nil {
			c.walkNode(node.Bounds.EndBound)
		}

	case *tree.WindowFrameBound:
		c.walkNode(node.OffsetExpr)

	case *tree.CastExpr:
		c.walkNode(node.Expr)

	case *tree.CoalesceExpr:
		for _, e := range node.Exprs {
			c.walkNode(e)
		}

	case *tree.BinaryExpr:
		c.walkNode(node.Left)
		c.walkNode(node.Right)

	case *tree.AndExpr:
		c.walkNode(node.Left)
		c.walkNode(node.Right)

	case *tree.OrExpr:
		c.walkNode(node.Left)
		c.walkNode(node.Right)

	case *tree.ComparisonExpr:
		c.walkNode(node.Left)
		c.walkNode(node.Right)

	case *tree.RangeCond:
		c.walkNode(node.Left)
		c.walkNode(node.From)
		c.walkNode(node.To)

	case *tree.CaseExpr:
		c.walkNode(node.Expr)
		c.walkNode(node.Else)
		for _, w := range node.Whens {
			c.walkNode(w.Cond)
			c.walkNode(w.Val)
		}

	case *tree.Tuple:
		for _, e := range node.Exprs {
			c.walkNode(e)
		}

	case *tree.ParenExpr:
		c.walkNode(node.Expr)

	case *tree.NotExpr:
		c.walkNode(node.Expr)

	case *tree.UnaryExpr:
		c.walkNode(node.Expr)

	case *tree.AnnotateTypeExpr:
		c.walkNode(node.Expr)

	case *tree.Array:
		for _, e := range node.Exprs {
			c.walkNode(e)
		}

	case *tree.ColumnTableDef, *tree.NumVal, *tree.StrVal, tree.DBool,
		*tree.SetVar, *tree.IndexTableDef, *tree.FamilyTableDef,
		*tree.UniqueConstraintTableDef:
		// Leaves without identifiers to check.

	default:
		// Unknown node types are skipped: a guard may be conservative but
		// must not crash the agent loop.
	}
}

// --- scope management ----------------------------------------------------

func (c *collector) enterSelect(sel *tree.Select) {
	f := &scopeFrame{
		sources: map[string]*tableSource{},
		selfOut: map[string]bool{},
	}
	c.frames = append(c.frames, f)
	if sel.With != nil {
		for _, cte := range sel.With.CTEList {
			name := strings.ToLower(string(cte.Name.Alias))
			if name == "" {
				continue
			}
			f.sources[name] = &tableSource{name: name, outputs: cteOutputs(cte.Stmt)}
		}
	}
}

func (c *collector) exitSelect() {
	c.frames = c.frames[:len(c.frames)-1]
}

func (c *collector) top() *scopeFrame {
	return c.frames[len(c.frames)-1]
}

// recordSelfOutputs makes select-list output names (aliases, projected
// column names) resolvable for ORDER BY / GROUP BY / HAVING. This is
// intentionally lenient: WHERE technically cannot reference select aliases,
// but a guard should prefer false negatives over false positives.
func (c *collector) recordSelfOutputs(cl *tree.SelectClause) {
	out, star := clauseOutputs(cl)
	if star {
		c.top().selfStar = true
	}
	for _, name := range out {
		c.top().selfOut[name] = true
	}
}

// lookupSource finds a name in the scope frames, innermost first.
func (c *collector) lookupSource(name string) *tableSource {
	for i := len(c.frames) - 1; i >= 0; i-- {
		if src, ok := c.frames[i].sources[name]; ok {
			return src
		}
	}
	return nil
}

// --- FROM handling -------------------------------------------------------

// visitTableName resolves a FROM table against CTEs (innermost first, so a
// CTE shadows a schema table) and then the introspected schema.
func (c *collector) visitTableName(tn *tree.TableName) {
	// Compare the raw Name, not its String(): tree.Name("").String() renders
	// the quoted empty marker `""`, which would look non-empty here.
	raw := string(tn.TableName)
	if raw == "" {
		return
	}
	name := strings.ToLower(raw)
	if src := c.lookupSource(name); src != nil {
		c.tableCache[tn] = src
		return
	}
	src := &tableSource{name: name}
	if t := c.sch.TableByName(name); t != nil {
		src.schemaT = t
		c.top().schemaTs = append(c.top().schemaTs, t)
	} else {
		c.violate(GroundednessViolation{
			Kind:       KindTable,
			Identifier: name,
			Suggestion: c.nearestTable(name),
		})
	}
	c.tableCache[tn] = src
}

// registerAlias binds a FROM item's alias to its resolved source. A subquery
// without an alias still makes its outputs visible for unqualified column
// resolution in the enclosing SELECT.
func (c *collector) registerAlias(ate *tree.AliasedTableExpr) {
	alias := strings.ToLower(string(ate.As.Alias))
	var src *tableSource
	switch e := ate.Expr.(type) {
	case *tree.TableName:
		src = c.tableCache[e]
	case *tree.Subquery:
		src = &tableSource{name: alias, outputs: selectStatementOutputs(e.Select)}
	default:
		src = &tableSource{name: alias}
	}
	if src == nil {
		src = &tableSource{name: alias}
	}
	if alias == "" {
		// Anonymous subquery: its outputs stay resolvable, but it binds no
		// qualifier.
		c.top().anon = append(c.top().anon, src)
		return
	}
	c.top().sources[alias] = src
}

// --- column handling ------------------------------------------------------

func (c *collector) visitUnresolvedName(un *tree.UnresolvedName) {
	if un.Star {
		if un.NumParts >= 2 {
			c.checkStarQualifier(strings.ToLower(un.Parts[un.NumParts-1]))
		}
		return
	}
	col := strings.ToLower(un.Parts[0])
	if col == "" {
		return
	}
	if un.NumParts >= 2 {
		c.checkQualifiedColumn(strings.ToLower(un.Parts[1]), col)
		return
	}
	c.checkUnqualifiedColumn(col)
}

func (c *collector) checkStarQualifier(qual string) {
	if c.lookupSource(qual) != nil {
		return
	}
	if c.sch.TableByName(qual) != nil {
		return
	}
	c.violate(GroundednessViolation{
		Kind:       KindTable,
		Identifier: qual + ".*",
		Suggestion: c.nearestTable(qual),
	})
}

func (c *collector) checkQualifiedColumn(qual, col string) {
	if src := c.lookupSource(qual); src != nil {
		if src.schemaT != nil {
			if src.schemaT.ColumnByName(col) == nil {
				c.violate(GroundednessViolation{
					Kind:       KindColumn,
					Identifier: qual + "." + col,
					Suggestion: c.nearestColumnIn(src.schemaT, col),
				})
			}
			return
		}
		// CTE or subquery source: outputs are CTE-local. Known-output matches
		// are grounded; anything else is deliberately NOT flagged — we cannot
		// enumerate CTE outputs reliably, and the requirement is that CTE
		// outputs must not be false positives.
		return
	}
	if t := c.sch.TableByName(qual); t != nil {
		if t.ColumnByName(col) == nil {
			c.violate(GroundednessViolation{
				Kind:       KindColumn,
				Identifier: qual + "." + col,
				Suggestion: c.nearestColumnIn(t, col),
			})
		}
		return
	}
	c.violate(GroundednessViolation{
		Kind:       KindTable,
		Identifier: qual,
		Suggestion: c.nearestTable(qual),
	})
}

func (c *collector) checkUnqualifiedColumn(col string) {
	for i := len(c.frames) - 1; i >= 0; i-- {
		f := c.frames[i]
		if f.selfStar || f.selfOut[col] {
			return
		}
		for _, src := range f.sources {
			if src.schemaT != nil {
				if src.schemaT.ColumnByName(col) != nil {
					return
				}
				continue
			}
			if outputsContain(src.outputs, col) {
				return
			}
		}
		for _, src := range f.anon {
			if outputsContain(src.outputs, col) {
				return
			}
		}
		for _, t := range f.schemaTs {
			if t.ColumnByName(col) != nil {
				return
			}
		}
	}
	c.violate(GroundednessViolation{
		Kind:       KindColumn,
		Identifier: col,
		Suggestion: c.nearestColumnVisible(col),
	})
}

// --- violations and suggestions -------------------------------------------

func (c *collector) violate(v GroundednessViolation) {
	key := string(v.Kind) + "/" + v.Identifier
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.viols = append(c.viols, v)
}

func (c *collector) nearestTable(name string) string {
	best, bestDist := "", 1<<30
	for _, t := range c.sch.Tables {
		if d := levenshtein(name, strings.ToLower(t.Name)); d < bestDist {
			best, bestDist = t.Name, d
		}
	}
	return best
}

// nearestColumnIn suggests the closest column within one table, rendered
// qualified so the model can paste it directly.
func (c *collector) nearestColumnIn(t *schema.Table, name string) string {
	best, bestDist := "", 1<<30
	for _, col := range t.Columns {
		if d := levenshtein(name, strings.ToLower(col.Name)); d < bestDist {
			best, bestDist = col.Name, d
		}
	}
	if best == "" {
		return ""
	}
	return t.Name + "." + best
}

// nearestColumnVisible suggests the closest column among the tables visible
// in the query; falls back to all schema columns.
func (c *collector) nearestColumnVisible(name string) string {
	var tables []*schema.Table
	for i := len(c.frames) - 1; i >= 0; i-- {
		tables = append(tables, c.frames[i].schemaTs...)
	}
	if len(tables) == 0 {
		for i := range c.sch.Tables {
			tables = append(tables, &c.sch.Tables[i])
		}
	}
	best, bestDist := "", 1<<30
	for _, t := range tables {
		for _, col := range t.Columns {
			if d := levenshtein(name, strings.ToLower(col.Name)); d < bestDist {
				best, bestDist = t.Name+"."+col.Name, d
			}
		}
	}
	return best
}

// --- CTE/subquery outputs -------------------------------------------------

// cteOutputs computes the output column names of a CTE statement.
func cteOutputs(stmt tree.Statement) []string {
	switch s := stmt.(type) {
	case *tree.Select:
		return selectStatementOutputs(s.Select)
	case *tree.ParenSelect:
		return selectStatementOutputs(s.Select.Select)
	default:
		return nil
	}
}

// selectStatementOutputs returns the output names of a select statement.
// nil means "unknown" (unresolvable expression) — the guard stays lenient.
func selectStatementOutputs(ss tree.SelectStatement) []string {
	switch s := ss.(type) {
	case *tree.SelectClause:
		out, star := clauseOutputs(s)
		if star {
			return starOutputs
		}
		return out
	case *tree.UnionClause:
		return selectOutputsOfSelect(s.Left)
	case *tree.ParenSelect:
		return selectStatementOutputs(s.Select.Select)
	default:
		return nil
	}
}

// selectOutputsOfSelect unwraps a *tree.Select to its SelectStatement.
func selectOutputsOfSelect(sel *tree.Select) []string {
	if sel == nil {
		return nil
	}
	return selectStatementOutputs(sel.Select)
}

// clauseOutputs returns the output names of a SelectClause: the explicit
// alias, or for a plain column reference its name. Unresolvable expressions
// are skipped. star reports SELECT * (or t.*).
func clauseOutputs(cl *tree.SelectClause) (out []string, star bool) {
	for _, e := range cl.Exprs {
		// Raw string, not String(): empty UnrestrictedName renders `""`.
		if as := strings.ToLower(string(e.As)); as != "" {
			out = append(out, as)
			continue
		}
		switch ex := e.Expr.(type) {
		case tree.UnqualifiedStar:
			star = true
		case *tree.UnresolvedName:
			if ex.Star {
				star = true
			} else if name := strings.ToLower(ex.Parts[0]); name != "" {
				out = append(out, name)
			}
		}
	}
	return out, star
}

func outputsContain(outputs []string, col string) bool {
	if outputs == nil {
		return false
	}
	for _, o := range outputs {
		if o == "*" || o == col {
			return true
		}
	}
	return false
}

// --- levenshtein ----------------------------------------------------------

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur := make([]int, len(br)+1)
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
