package clickhouse

import (
	"fmt"
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
	return data, rows.Err()
}

// ---------------------------------------------------------------------------
// Value extraction helpers (used by handlers)
// ---------------------------------------------------------------------------

// GetStr safely extracts a string from a map.
func GetStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// GetF64 safely extracts a float64 from a map.
func GetF64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok && v != nil {
		switch val := v.(type) {
		case float64:
			return val
		case uint64:
			return float64(val)
		case int64:
			return float64(val)
		case float32:
			return float64(val)
		default:
			return 0
		}
	}
	return 0
}
