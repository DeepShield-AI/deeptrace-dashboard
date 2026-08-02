package dbdesc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/logging"
)

// Query executes a DB description query.
// ShowDatabases/ShowTables run against ClickHouse; ShowTag runs against
// deepflow-server (ZT), which resolves the tag metadata natively.
// Returns the result data and true if the path was recognized.
func Query(zt *client.ZerotraceService, ch *clickhouse.CHService, path, bodyStr string) ([]map[string]interface{}, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch {
	case strings.Contains(path, "ShowDatabases"):
		rows, err := ch.Query(ctx, "SELECT name FROM system.databases WHERE name NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') ORDER BY name")
		if err != nil {
			return nil, false
		}
		defer rows.Close()
		data, err := clickhouse.ScanRows(rows)
		if err != nil || len(data) == 0 {
			return nil, false
		}
		result := make([]map[string]interface{}, 0, len(data))
		for _, row := range data {
			result = append(result, map[string]interface{}{"name": row["name"]})
		}
		return result, true

	case strings.Contains(path, "ShowTables"):
		var req struct {
			Database string `json:"DATABASE"`
		}
		if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
			return nil, false
		}
		db := req.Database
		if db == "" {
			db = "flow_log"
		}
		rows, err := ch.Query(ctx, "SELECT name FROM system.tables WHERE database='"+db+"' AND name NOT LIKE '%\\_local' AND engine != 'Distributed' AND engine != 'Dictionary' AND engine != 'MaterializedView' AND engine != 'View' AND engine != 'LiveView' ORDER BY name")
		if err != nil {
			return nil, false
		}
		defer rows.Close()
		data, err := clickhouse.ScanRows(rows)
		if err != nil || len(data) == 0 {
			return nil, false
		}
		result := make([]map[string]interface{}, 0, len(data))
		for _, row := range data {
			result = append(result, map[string]interface{}{"name": row["name"]})
		}
		return result, true

	case strings.Contains(path, "ShowTag"):
		return queryShowTags(zt, bodyStr)
	}
	return nil, false
}

// queryShowTags queries deepflow-server for tag metadata (SHOW tags FROM <table>).
// Bypasses ClickHouse — ZT resolves virtual tags natively.
func queryShowTags(zt *client.ZerotraceService, bodyStr string) ([]map[string]interface{}, bool) {
	if zt == nil || !zt.Available() {
		return nil, false
	}
	var req struct {
		Database string `json:"DATABASE"`
		Table    string `json:"TABLE"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, false
	}
	db := req.Database
	if db == "" {
		db = "flow_log"
	}
	tbl := req.Table
	if tbl == "" {
		tbl = "l7_flow_log"
	}

	rows, err := zt.QueryRaw(db, "SHOW tags FROM `"+tbl+"`")
	if err != nil {
		logging.Errorf("ZT ShowTags error: %v (db=%s tbl=%s)", err, db, tbl)
		return nil, false
	}
	if len(rows.Values) == 0 {
		return []map[string]interface{}{}, true
	}

	// Convert columns+values to []map[string]interface{} (tag metadata rows).
	data := make([]map[string]interface{}, 0, len(rows.Values))
	for _, row := range rows.Values {
		r := make(map[string]interface{}, len(rows.Columns)+1)
		for i, col := range rows.Columns {
			if i >= len(row) {
				continue
			}
			val := row[i]
			// Convert json.Number to float64 for JSON serialization compatibility.
			if num, ok := val.(json.Number); ok {
				if f, err := num.Float64(); err == nil {
					val = f
				} else {
					val = num.String()
				}
			}
			r[col] = val
		}
		r["_querier_region"] = clickhouse.QuerierRegion
		data = append(data, r)
	}
	return data, true
}
