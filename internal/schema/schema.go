// Package schema introspects relational schemas, currently from Postgres
// information_schema.
package schema

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema is a set of tables.
type Schema struct {
	Tables []Table
}

// Table is a relation with an ordered column list.
type Table struct {
	Name    string
	Columns []Column
}

// Column is a typed column.
type Column struct {
	Name string
	Type string
}

// Load connects to Postgres at dsn and reads table metadata for the given
// schema (default "public").
func Load(ctx context.Context, dsn, schemaName string) (*Schema, error) {
	if schemaName == "" {
		schemaName = "public"
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	const q = `
SELECT table_name, column_name, data_type, ordinal_position
FROM information_schema.columns
WHERE table_schema = $1
ORDER BY table_name, ordinal_position`

	rows, err := pool.Query(ctx, q, schemaName)
	if err != nil {
		return nil, fmt.Errorf("query information_schema: %w", err)
	}
	defer rows.Close()

	s := &Schema{}
	byName := map[string]*Table{}
	for rows.Next() {
		var tableName, columnName, dataType string
		var ordinal int
		if err := rows.Scan(&tableName, &columnName, &dataType, &ordinal); err != nil {
			return nil, fmt.Errorf("scan information_schema row: %w", err)
		}
		t, ok := byName[tableName]
		if !ok {
			s.Tables = append(s.Tables, Table{Name: tableName})
			t = &s.Tables[len(s.Tables)-1]
			byName[tableName] = t
		}
		t.Columns = append(t.Columns, Column{Name: columnName, Type: dataType})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read information_schema: %w", err)
	}
	return s, nil
}

// TableByName returns the named table or nil.
func (s *Schema) TableByName(name string) *Table {
	for i := range s.Tables {
		if s.Tables[i].Name == name {
			return &s.Tables[i]
		}
	}
	return nil
}
