package engine

// BuildSchemas creates a SCHEMAS map from a result row.
// Detects numeric types (float64/float32 → Float64, int → UInt64)
// and defaults to String for everything else.
func BuildSchemas(row map[string]interface{}) map[string]interface{} {
	schemas := map[string]interface{}{}
	for k, v := range row {
		vt, tp := "String", 0
		switch v.(type) {
		case float64, float32:
			vt, tp = "Float64", 1
		case int, int64, uint64:
			vt, tp = "UInt64", 1
		}
		schemas[k] = map[string]interface{}{
			"label_type": "", "pre_as": "", "type": tp,
			"unit": "", "value_type": vt,
		}
	}
	return schemas
}
