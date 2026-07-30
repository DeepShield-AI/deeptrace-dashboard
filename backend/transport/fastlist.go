package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/query"
	"deeptrace-backend/query/flowlog"
)


func handleFastFilterBlackLists(w http.ResponseWriter, r *http.Request) {
	db := r.URL.Query().Get("db")
	table := r.URL.Query().Get("table")
	pageKey := r.URL.Query().Get("page_key")

	// pre-defined tag/metric lists for known page_keys.
	type filterConfig struct {
		tagOrder    []string
		metricOrder []string
	}
	configs := map[string]filterConfig{
		"flow_log.l7_flow_log.app_link_trace": {
			tagOrder: []string{"signal_source", "chost", "host", "vpc", "subnet",
				"pod_cluster", "response_status", "pod_ns", "pod_node",
				"pod_service", "pod_group", "pod", "endpoint", "l7_protocol"},
			metricOrder: []string{"response_duration"},
		},
		"flow_log.l7_flow_log.app_flow_log": {
			tagOrder: []string{"response_status", "observation_point", "signal_source",
				"chost", "host", "vpc", "subnet", "pod_cluster", "pod_ns",
				"pod_node", "pod_service", "pod_group", "pod", "endpoint", "l7_protocol"},
			metricOrder: []string{"response_duration"},
		},
	}

	// Build key as "db.table.last_segment_of_page_key".
	// page_key looks like "flow_log.l7_flow_log.app_link_trace".
	key := ""
	if idx := strings.LastIndex(pageKey, "."); idx >= 0 && idx < len(pageKey)-1 {
		key = db + "." + table + "." + pageKey[idx+1:]
	}
	cfg, ok := configs[key]
	if !ok {
		cfg = filterConfig{tagOrder: []string{}, metricOrder: []string{}}
	}

	writeJSON(w, map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
		"DATA": map[string]interface{}{
			"tag_blacklist":    []interface{}{},
			"metric_blacklist": []interface{}{},
			"tag_order":        cfg.tagOrder,
			"metric_order":     cfg.metricOrder,
		},
	})
}


func handleFastFilterBlackLists(w http.ResponseWriter, r *http.Request) {
	db := r.URL.Query().Get("db")
	table := r.URL.Query().Get("table")
	pageKey := r.URL.Query().Get("page_key")

	// pre-defined tag/metric lists for known page_keys.
	type filterConfig struct {
		tagOrder    []string
		metricOrder []string
	}
	configs := map[string]filterConfig{
		"flow_log.l7_flow_log.app_link_trace": {
			tagOrder: []string{"signal_source", "chost", "host", "vpc", "subnet",
				"pod_cluster", "response_status", "pod_ns", "pod_node",
				"pod_service", "pod_group", "pod", "endpoint", "l7_protocol"},
			metricOrder: []string{"response_duration"},
		},
		"flow_log.l7_flow_log.app_flow_log": {
			tagOrder: []string{"response_status", "observation_point", "signal_source",
				"chost", "host", "vpc", "subnet", "pod_cluster", "pod_ns",
				"pod_node", "pod_service", "pod_group", "pod", "endpoint", "l7_protocol"},
			metricOrder: []string{"response_duration"},
		},
	}

	// Build key as "db.table.last_segment_of_page_key".
	// page_key looks like "flow_log.l7_flow_log.app_link_trace".
	key := ""
	if idx := strings.LastIndex(pageKey, "."); idx >= 0 && idx < len(pageKey)-1 {
		key = db + "." + table + "." + pageKey[idx+1:]
	}
	cfg, ok := configs[key]
	if !ok {
		cfg = filterConfig{tagOrder: []string{}, metricOrder: []string{}}
	}

	writeJSON(w, map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
		"DATA": map[string]interface{}{
			"tag_blacklist":    []interface{}{},
			"metric_blacklist": []interface{}{},
			"tag_order":        cfg.tagOrder,
			"metric_order":     cfg.metricOrder,
		},
	})
}

type fastListRequest struct {
	DB         string `json:"db"`
	Table      string `json:"table"`
	TimeStart  int64  `json:"time_start"`
	TimeEnd    int64  `json:"time_end"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	DataSource string `json:"data_source"`
	Where      *struct {
		ResourceSets []struct {
			ID        string `json:"id"`
			Condition []interface{} `json:"condition"` // flat [{key,op,val}] or nested AND/OR tree
		} `json:"resourceSets"`
		Paths []map[string]string `json:"paths"`
	} `json:"where"`
}

// virtualColumnMap maps virtual tag names to their physical ID column counterparts.
// When conditions compare virtual columns to numbers, ZT fails with type mismatches
// (String vs UInt16). Route to the physical ID column for numeric comparisons.
// fastListSkipCols lists columns that don't exist in raw ClickHouse tables
// and should be skipped in WHERE conditions.
var fastListSkipCols = map[string]struct{}{
	"role":          {},
	"is_internet":   {},
	"is_internet_0": {},
	"is_internet_1": {},
}



// flattenFastListConditions recursively extracts leaf conditions from a nested
// AND/OR condition tree (sent by the frontend in QuerierJs format).
func flattenFastListConditions(conds []interface{}, db string) []string {
	var result []string
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		// Leaf condition: has "key" field
		if key, hasKey := m["key"]; hasKey {
			op, _ := m["op"].(string)
			if op == "" {
				op = "="
			}
			col := fmt.Sprintf("%v", key)
			val := m["val"]

		// Skip columns that dont exist in raw ClickHouse.
		if _, skip := fastListSkipCols[col]; skip {
			continue
		}
			// Virtual tag (String) compared to number: use the physical ID column.
			if physicalCol := clickhouse.IDColumn(col); physicalCol != col {
				if _, isNum := val.(float64); isNum {
					col = physicalCol
					if db == "flow_log" {
						col += "_0"
					}
				}
			}

			// Quote string values, leave numeric values unquoted.
			var valStr string
			if s, ok := val.(string); ok {
				valStr = "'" + s + "'"
			} else {
				valStr = fmt.Sprintf("%v", val)
			}

			result = append(result, "`" + col + "` " + op + " " + valStr)
			continue
		}
		// Branch condition: has "val" array (nested children)
		if val, hasVal := m["val"]; hasVal {
			if children, ok := val.([]interface{}); ok {
				result = append(result, flattenFastListConditions(children, db)...)
			}
		}
	}
	return result
}
