package query

import (
	"encoding/json"
	"fmt"
	"log"

	"deeptrace-backend/client"
)

// QueryShowTags queries deepflow-server for tag metadata.
// Called from transport/dbdesc.go after cache miss, before file fallback.
// Bypasses DataSourceChain (tags are metadata, not data).
func QueryShowTags(zt *client.ZerotraceService, bodyStr string) (*Result, error) {
	if zt == nil || !zt.Available() {
		return nil, nil
	}

	var req struct {
		Database string `json:"DATABASE"`
		Table    string `json:"TABLE"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, nil
	}

	db := req.Database
	if db == "" {
		db = "flow_log"
	}
	tbl := req.Table
	if tbl == "" {
		tbl = "l7_flow_log"
	}

	sql := fmt.Sprintf("SHOW tags FROM `%s`", tbl)

	rows, err := zt.QueryRaw(db, sql)
	if err != nil {
		log.Printf("⚠️ ZT ShowTags error: %v (db=%s tbl=%s)", err, db, tbl)
		return nil, nil
	}
	if len(rows.Values) == 0 {
		return &Result{Data: []map[string]interface{}{}, Type: "DBDescription"}, nil
	}

	// Convert columns+values to []map[string]interface{}.
	// This is similar to flowlog.buildData but simpler (no _id conversion needed for tag metadata).
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
		r["_querier_region"] = "本地"
		data = append(data, r)
	}

	return &Result{
		Data:   data,
		Count:  len(data),
		Type:   "DBDescription",
		Fields: map[string]interface{}{},
	}, nil
}
