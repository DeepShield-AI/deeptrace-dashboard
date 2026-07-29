package transport

import (
	"context"
	"fmt"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
)

// RegisterDBDescription registers the DBDescription (ShowDatabases/ShowTables/etc.) endpoint.
func RegisterDBDescription(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/ShowMetricsFunctions", handleShowMetricsFunctions)
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/", handleDBDescription(deps))
}

func handleShowMetricsFunctions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
		"DATA": []map[string]interface{}{
			{"name": "Avg", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "AAvg", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Sum", "type": 1, "support_metric_types": []int{1}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Max", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Min", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Percentile", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 1, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "PercentileExact", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 1, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Spread", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Rspread", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Stddev", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Apdex", "type": 1, "support_metric_types": []int{3}, "unit_overwrite": "%", "additional_param_count": 1, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Uniq", "type": 1, "support_metric_types": []int{6}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": false, "value_type": "Number", "_querier_region": "本地"},
			{"name": "UniqExact", "type": 1, "support_metric_types": []int{6}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": false, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Percentage", "type": 3, "unit_overwrite": "%", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "PerSecond", "type": 3, "unit_overwrite": "$unit/s", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Histogram", "type": 3, "unit_overwrite": "", "additional_param_count": 1, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Last", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "Count", "type": 1, "support_metric_types": []int{8}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": "本地"},
			{"name": "TopK", "type": 1, "support_metric_types": []int{6}, "unit_overwrite": "$unit", "additional_param_count": 1, "is_support_other_operators": false, "value_type": "String", "_querier_region": "本地"},
			{"name": "Any", "type": 1, "support_metric_types": []int{6}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": false, "value_type": "String", "_querier_region": "本地"},
		},
	})
}

func handleDBDescription(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		path := r.URL.Path

		// 1. Try cache first (skip ShowTags to serve fresh virtual tags).
		if deps.Cache != nil {
			if cached := deps.Cache.FindWithBody(r.Method, r.URL.RequestURI(), bodyStr); cached != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Write(cached)
				return
			}
		}

		// Fallback: hardcoded / JSON file.
		switch {
		case strings.Contains(path, "ShowDatabases"):
			writeSuccess(w, []map[string]interface{}{
				{"name": "flow_metrics", "datasources": []string{"1m", "1s"}},
				{"name": "flow_log", "datasources": []string{"l4_flow_log", "l7_flow_log"}},
				{"name": "event", "datasources": []string{"perf_event", "alarm_event"}},
			})
		case strings.Contains(path, "ShowTables"):
			data, err := []map[string]interface{}{}, fmt.Errorf("no data")
			if err != nil {
				writeSuccess(w, []map[string]interface{}{
					{"name": "vtap_app_port", "datasources": []string{"1m", "1s"}},
					{"name": "vtap_flow_port", "datasources": []string{"1m", "1s"}},
					{"name": "application_map", "datasources": []string{"1m"}},
					{"name": "network_map", "datasources": []string{"1m"}},
				})
				return
			}
			writeSuccess(w, data)
		case strings.Contains(path, "ShowTag"):
			data, err := []map[string]interface{}{}, fmt.Errorf("no data")
			if err != nil {
				writeSuccess(w, []map[string]interface{}{
					{"name": "auto_service", "display_name": "服务", "type": "resource"},
					{"name": "auto_instance", "display_name": "实例", "type": "resource"},
					{"name": "ip", "display_name": "IP地址", "type": "resource"},
					{"name": "protocol", "display_name": "协议", "type": "int_enum"},
					{"name": "is_internet", "display_name": "网络类型", "type": "int_enum"},
					{"name": "role", "client_name": "role", "server_name": "role",
					"display_name": "角色", "display_name_zh": "角色", "display_name_en": "Role",
					"type": "int_enum", "category": "Capture Info",
					"operators": []string{"=", "!=", "IN", "NOT IN", ">=", "<="},
					"permissions": []bool{true, true, true},
					"description": "", "description_zh": "", "description_en": "",
					"related_tag": "", "deprecated": false,
					"not_supported_operators": []string{}, "table": ""},
					{"name": "response_status", "display_name": "响应状态", "type": "int_enum"},
					{"name": "observation_point", "display_name": "观测点", "type": "resource"},
					{"name": "server_port", "display_name": "服务端端口", "type": "resource"},
				})
				return
			}
			writeSuccess(w, data)
		default:
			writeSuccess(w, []interface{}{})
		}
	}
}

// tryCHDBDescription queries ClickHouse for DB description data directly.
// Returns true if a response was written.
func tryCHDBDescription(ch *clickhouse.CHService, w http.ResponseWriter, r *http.Request, path, bodyStr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch {
	case strings.Contains(path, "ShowDatabases"):
		rows, err := ch.Query(ctx, "SELECT name FROM system.databases WHERE name NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') ORDER BY name")
		if err != nil {
			return false
		}
		defer rows.Close()
		data, err := clickhouse.ScanRows(rows)
		if err != nil || len(data) == 0 {
			return false
		}
		result := make([]map[string]interface{}, 0, len(data))
		for _, row := range data {
			result = append(result, map[string]interface{}{"name": row["name"]})
		}
		writeSuccess(w, result)
		return true

	case strings.Contains(path, "ShowTables"):
		var req struct {
			Database string `json:"DATABASE"`
		}
		if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
			return false
		}
		db := req.Database
		if db == "" {
			db = "flow_log"
		}
		rows, err := ch.Query(ctx, "SELECT name FROM system.tables WHERE database='"+db+"' AND name NOT LIKE '%\\_local' AND engine != 'Distributed' AND engine != 'Dictionary' AND engine != 'MaterializedView' AND engine != 'View' AND engine != 'LiveView' ORDER BY name")
		if err != nil {
			return false
		}
		defer rows.Close()
		data, err := clickhouse.ScanRows(rows)
		if err != nil || len(data) == 0 {
			return false
		}
		result := make([]map[string]interface{}, 0, len(data))
		for _, row := range data {
			result = append(result, map[string]interface{}{"name": row["name"]})
		}
		writeSuccess(w, result)
		return true
	}
	return false
}
