package exec

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// numericToFloat converts pgx numeric values to float64.
func numericToFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case pgtype.Numeric:
		f, err := t.Float64Value()
		if err != nil || !f.Valid {
			return 0, false
		}
		return f.Float64, true
	case *pgtype.Numeric:
		if t == nil {
			return 0, false
		}
		f, err := t.Float64Value()
		if err != nil || !f.Valid {
			return 0, false
		}
		return f.Float64, true
	}
	return 0, false
}

// timeValue extracts a time.Time from pgx temporal types.
func timeValue(v any) (time.Time, bool) {
	switch t := v.(type) {
	case pgtype.Date:
		if !t.Valid {
			return time.Time{}, false
		}
		return t.Time, true
	case *pgtype.Date:
		if t == nil || !t.Valid {
			return time.Time{}, false
		}
		return t.Time, true
	case pgtype.Timestamp:
		if !t.Valid {
			return time.Time{}, false
		}
		return t.Time, true
	case *pgtype.Timestamp:
		if t == nil || !t.Valid {
			return time.Time{}, false
		}
		return t.Time, true
	case pgtype.Timestamptz:
		if !t.Valid {
			return time.Time{}, false
		}
		return t.Time, true
	case *pgtype.Timestamptz:
		if t == nil || !t.Valid {
			return time.Time{}, false
		}
		return t.Time, true
	}
	return time.Time{}, false
}
