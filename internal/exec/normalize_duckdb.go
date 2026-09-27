//go:build !noduckdb

package exec

import (
	"math/big"

	duckdb "github.com/marcboeker/go-duckdb"
)

// duckNormalize coerces DuckDB-specific scalar types (*big.Int HUGEINT,
// duckdb.Decimal) into int64/float64.
func duckNormalize(v any) (any, bool) {
	switch t := v.(type) {
	case *big.Int:
		if t.IsInt64() {
			return t.Int64(), true
		}
		f, _ := new(big.Float).SetInt(t).Float64()
		return f, true
	case duckdb.Decimal:
		return t.Float64(), true
	case *duckdb.Decimal:
		if t == nil {
			return nil, false
		}
		return t.Float64(), true
	}
	return nil, false
}
