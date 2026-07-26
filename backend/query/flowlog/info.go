package flowlog

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"deeptrace-backend/client"
	"deeptrace-backend/clickhouse"
	"deeptrace-backend/query"
)

// infoRequest mirrors the JSON body of a FlowLogDetailInfo request.
// This is a separate struct from listRequest — the two APIs are independent.
type infoRequest struct {
	Database  string `json:"DATABASE"`
	Table     string `json:"TABLE"`
	Region    string `json:"REGION"`
	Queries   []struct {
		QueryID string   `json:"QUERY_ID"`
		Select  string   `json:"SELECT"`
		Where   string   `json:"WHERE"`
		Roles   []string `json:"ROLES"`
	} `json:"QUERIES"`
	TimeStart int64 `json:"time_start"`
	TimeEnd   int64 `json:"time_end"`
}

// QueryInfo executes a FlowLogDetailInfo query via deepflow-server.
// Returns TYPE: "Flow_Log_Detail_Info". Response omits COUNT (handled by transport).
func QueryInfo(zt *client.ZerotraceService, bodyStr string) (*query.Result, error) {
	if zt == nil {
		return &query.Result{
			Data: []map[string]interface{}{},
			Type: "Flow_Log_Detail_Info",
		}, nil
	}

	var req infoRequest
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	db := req.Database
	if db == "" {
		db = "flow_log"
	}
	tbl := req.Table
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	isFlowLog := db == "flow_log"

	// ---------------------------------------------------------------------------
	// Build SELECT columns
	// ---------------------------------------------------------------------------
	var selectCols string
	if len(req.Queries) > 0 && req.Queries[0].Select != "" {
		items := clickhouse.ParseSelectList(req.Queries[0].Select)
		var cols []string
		for _, item := range items {
			expr, key := item.Expr, item.Key
			lower := strings.ToLower(expr)

			switch {
			case strings.HasPrefix(lower, "newtag("),
				strings.HasPrefix(lower, "enum("),
				strings.HasPrefix(lower, "icon_id("),
				strings.HasPrefix(lower, "node_type("):
				cols = append(cols, fmt.Sprintf("%s AS `%s`", expr, key))

			default:
				col := strings.Trim(expr, "`")

				if isFlowLog && tbl == "l7_flow_log" {
					switch col {
					case "event_desc":
						col = "request_resource"
					case "event_type":
						col = "l7_protocol"
					}
				}

				// Normalize key: strip backticks (parseOneSelect may leave them
				// for bare backtick-quoted columns like `metrics`).
				cleanKey := strings.Trim(key, "`")
				if col != cleanKey {
					cols = append(cols, fmt.Sprintf("`%s` AS `%s`", col, cleanKey))
				} else {
					cols = append(cols, fmt.Sprintf("`%s`", col))
				}
			}
		}
		if len(cols) > 0 {
			selectCols = strings.Join(cols, ", ")
		}
	}

	// ---------------------------------------------------------------------------
	// Build WHERE clause (time range + query-level _id filter)
	// ---------------------------------------------------------------------------
	var timeClauses []string
	if req.TimeStart > 0 {
		timeClauses = append(timeClauses, fmt.Sprintf("time >= %d", req.TimeStart))
	}
	if req.TimeEnd > 0 {
		timeClauses = append(timeClauses, fmt.Sprintf("time <= %d", req.TimeEnd))
	}

	if len(req.Queries) > 0 && req.Queries[0].Where != "" {
		timeClauses = append(timeClauses, req.Queries[0].Where)
	}

	whereClause := ""
	if len(timeClauses) > 0 {
		whereClause = " WHERE " + strings.Join(timeClauses, " AND ")
	}

	// ---------------------------------------------------------------------------
	// Full SQL (no SORT, no TOTAL — single-row detail query)
	// ---------------------------------------------------------------------------
	sql := fmt.Sprintf("SELECT %s FROM `%s`%s LIMIT 1",
		selectCols, tbl, whereClause)

	log.Printf("🔍 ZT FlowLogDetailInfo: db=%s sql=%s", db, sql)

	// ---------------------------------------------------------------------------
	// Query deepflow-server
	// ---------------------------------------------------------------------------
	queryID := ""
	if len(req.Queries) > 0 {
		queryID = req.Queries[0].QueryID
	}

	rows, err := zt.QueryRaw(db, sql)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return &query.Result{
			Data: []map[string]interface{}{},
			Type: "Flow_Log_Detail_Info",
		}, nil
	}

	// ---------------------------------------------------------------------------
	// Post-process
	// ---------------------------------------------------------------------------
	data := BuildData(rows, req.Region)

	for ir, row := range data {
		for ic, col := range rows.Columns {
			if ic >= len(rows.Schemas) {
				continue
			}
			preAs := strings.ToLower(rows.Schemas[ic].PreAs)
			if !strings.HasPrefix(preAs, "enum(") {
				continue
			}
			val, ok := row[col].(string)
			if !ok || val == "" {
				continue
			}
			if zh := EnumZHCN(val); zh != "" {
				data[ir][col] = zh
			}
		}
	}

	// ---------------------------------------------------------------------------
	// SCHEMAS
	// ---------------------------------------------------------------------------
	schemas := BuildSchemas(rows, queryID)

	return &query.Result{
		Data:   data,
		Type:   "Flow_Log_Detail_Info",
		Fields: schemas,
	}, nil
}
