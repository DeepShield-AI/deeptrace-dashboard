package clickhouse

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// rowCollector handles type-aware ClickHouse row scanning.
type rowCollector struct {
	colNames []string
	colTypes []driver.ColumnType
}

func newRowCollector(rows driver.Rows) *rowCollector {
	return &rowCollector{
		colNames: rows.Columns(),
		colTypes: rows.ColumnTypes(),
	}
}

func (rc *rowCollector) makeTargets() []interface{} {
	targets := make([]interface{}, len(rc.colTypes))
	for i, ct := range rc.colTypes {
		targets[i] = reflect.New(ct.ScanType()).Interface()
	}
	return targets
}

func valueFromTarget(target interface{}) interface{} {
	rv := reflect.ValueOf(target).Elem()

	// Dereference single pointer (*T -> T).
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		inner := rv.Elem()
		if inner.Kind() == reflect.Ptr {
			if inner.IsNil() {
				return nil
			}
			return inner.Elem().Interface()
		}
		return inner.Interface()
	}

	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Uint64:
		return rv.Uint()
	case reflect.Uint, reflect.Uint32, reflect.Uint16, reflect.Uint8:
		return float64(rv.Uint())
	case reflect.Int64, reflect.Int, reflect.Int32, reflect.Int16, reflect.Int8:
		return float64(rv.Int())
	case reflect.Float64, reflect.Float32:
		return rv.Float()
	case reflect.Bool:
		return rv.Bool()
	case reflect.Struct:
		if t, ok := rv.Interface().(time.Time); ok {
			return t.UnixMicro()
		}
		return rv.Interface()
	case reflect.Slice:
		// ClickHouse IPv4/IPv6 can be scanned as []uint8.
		if rv.Type().Elem().Kind() == reflect.Uint8 && (rv.Len() == 4 || rv.Len() == 16) {
			b := make([]byte, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				b[i] = byte(rv.Index(i).Uint())
			}
			if rv.Len() == 4 {
				return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
			}
			return fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
				b[0:2], b[2:4], b[4:6], b[6:8],
				b[8:10], b[10:12], b[12:14], b[14:16])
		}
		// ClickHouse Array(T) is scanned as e.g. []string, convert to []interface{} for GetArr.
		s := rv
		result := make([]interface{}, s.Len())
		for i := 0; i < s.Len(); i++ {
			result[i] = s.Index(i).Interface()
		}
		return result
	case reflect.Array:
		// ClickHouse IPv4 is [4]byte, IPv6 is [16]byte.
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				b[i] = byte(rv.Index(i).Uint())
			}
			if rv.Len() == 4 {
				return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
			}
			if rv.Len() == 16 {
				return fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
					b[0:2], b[2:4], b[4:6], b[6:8],
					b[8:10], b[10:12], b[12:14], b[14:16])
			}
		}
		return rv.Interface()
	default:
		return rv.Interface()
	}
}

// ScanRows scans all rows from a ClickHouse query result into []map[string]interface{}.
func ScanRows(rows driver.Rows) ([]map[string]interface{}, error) {
	rc := newRowCollector(rows)
	colNames := rc.colNames
	var data []map[string]interface{}

	for rows.Next() {
		targets := rc.makeTargets()
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}
		row := map[string]interface{}{}
		for i, name := range colNames {
			row[name] = valueFromTarget(targets[i])
		}
		data = append(data, row)
	}
	for _, row := range data {
		for k, v := range row {
			if f, ok := v.(float64); ok && math.IsNaN(f) {
				row[k] = nil
			}
		}
	}
	return data, rows.Err()
}

// ---------------------------------------------------------------------------
// Value extraction helpers (used by handlers)
// ---------------------------------------------------------------------------

// GetStr safely extracts a string from a map (Sprintf fallback for non-strings).
func GetStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// ToFloat64 converts a value to float64 (supports json.Number).
func ToFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case int:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

// ToIntOK converts a value to int (supports json.Number).
func ToIntOK(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case int:
		return n, true
	case uint8:
		return int(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return int(f), true
		}
	}
	return 0, false
}

// ToInt64 converts a value to int64 (0 if not numeric).
func ToInt64(v interface{}) int64 {
	if n, ok := ToInt64OK(v); ok {
		return n
	}
	return 0
}

// ToInt64OK converts a value to int64 (supports json.Number).
func ToInt64OK(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case uint64:
		return int64(n), true
	case int:
		return int64(n), true
	case uint8:
		return int64(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, true
		}
	}
	return 0, false
}

// Get returns the typed value for key. It first tries a strict assertion,
// then a numeric coercion for numeric T (same value set as
// ToIntOK/ToInt64OK/ToFloat64, including json.Number). Missing, nil or
// unconvertible values yield the zero value.
func Get[T any](m map[string]interface{}, key string) T {
	if v, ok := m[key]; ok && v != nil {
		if t, ok := v.(T); ok {
			return t
		}
		if t, ok := coerceNumeric[T](v); ok {
			return t
		}
	}
	var zero T
	return zero
}

// coerceNumeric converts a value to the requested numeric type. The switch
// dispatches on the instantiated T, so any(n).(T) always succeeds inside a
// matching case.
func coerceNumeric[T any](v interface{}) (T, bool) {
	switch any((*new(T))).(type) {
	case int:
		n, ok := ToIntOK(v)
		return any(n).(T), ok
	case int64:
		n, ok := ToInt64OK(v)
		return any(n).(T), ok
	case float64:
		n, ok := ToFloat64(v)
		return any(n).(T), ok
	case float32:
		n, ok := ToFloat64(v)
		return any(float32(n)).(T), ok
	case uint64:
		n, ok := toUint64OK(v)
		return any(n).(T), ok
	default:
		var z T
		return z, false
	}
}

// toUint64OK converts a value to uint64 (same value set as ToInt64OK).
func toUint64OK(v interface{}) (uint64, bool) {
	switch n := v.(type) {
	case float64:
		return uint64(n), true
	case int64:
		return uint64(n), true
	case uint64:
		return n, true
	case int:
		return uint64(n), true
	case uint8:
		return uint64(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return uint64(i), true
		}
	}
	return 0, false
}
