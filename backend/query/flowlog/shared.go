package flowlog

import (
	"encoding/json"
	"fmt"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
)

// buildData post-processes deepflow-server rows: converts _id to string,
// handles null fields, adds _querier_region.
func BuildData(rows *client.QueryResult, region string) []map[string]interface{} {
	data := make([]map[string]interface{}, 0, len(rows.Values))
	for _, row := range rows.Values {
		r := make(map[string]interface{}, len(rows.Columns)+1)

		for i, col := range rows.Columns {
			if i >= len(row) {
				continue
			}
			val := row[i]

			if col == "_id" {
				switch v := val.(type) {
				case float64:
					r[col] = fmt.Sprintf("%.0f", v)
				case json.Number:
					// Use raw string to preserve full UInt64 precision
					r[col] = v.String()
				default:
					r[col] = fmt.Sprintf("%v", v)
				}
				continue
			}

			// Convert json.Number to float64 for backward compatibility
			if num, ok := val.(json.Number); ok {
				if f, err := num.Float64(); err == nil {
					val = f
				}
			}

			if val == nil {
				switch col {
				case "response_exception":
					r[col] = ""
				default:
					r[col] = nil
				}
				continue
			}

			r[col] = val
		}

		if region != "" {
			r["_querier_region"] = region
		} else if _, has := r["_querier_region"]; !has {
			r["_querier_region"] = clickhouse.QuerierRegion
		}

		data = append(data, r)
	}
	return data
}

// buildSchemas constructs the SCHEMAS map from deepflow-server column schemas.
func BuildSchemas(rows *client.QueryResult, queryID string) map[string]interface{} {
	schemas := map[string]interface{}{}
	for i, col := range rows.Columns {
		vt, tp, preAs := "String", 0, ""
		if i < len(rows.Schemas) {
			vt = rows.Schemas[i].ValueType
			tp = rows.Schemas[i].Type
			preAs = rows.Schemas[i].PreAs
		}
		unit := ""
		if i < len(rows.Schemas) {
			unit = rows.Schemas[i].Unit
		}
		schemas[col] = map[string]interface{}{
			"label_type": "", "pre_as": preAs, "type": tp,
			"unit": unit, "value_type": vt,
		}
	}
	if _, has := schemas["query_id"]; !has && queryID != "" {
		schemas["query_id"] = map[string]interface{}{
			"label_type": "", "pre_as": fmt.Sprintf("newTag('%s')", queryID), "type": 0,
			"unit": "", "value_type": "String",
		}
	}
	return schemas
}

// enumZHCN translates known English deepflow-server enum display values to Chinese.
func EnumZHCN(val string) string {
	switch val {
	case "No":
		return "否"
	case "Yes":
		return "是"
	case "Success":
		return "正常"
	case "Timeout":
		return "超时"
	case "Client Error":
		return "客户端异常"
	case "Server Error":
		return "服务端异常"
	case "Unknown":
		return "未知"
	case "Client Process":
		return "客户端进程"
	case "Server Process":
		return "服务端进程"
	case "Client":
		return "客户端"
	case "Server":
		return "服务端"
	}
	return ""
}
