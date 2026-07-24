// Package flowlog implements FlowLogDetail list and info queries.
// Both use deepflow-server direct (zerotrace), bypassing DataSourceChain.
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

// listRequest mirrors the JSON body of a FlowLogDetailList request.
type listRequest struct {
	Database  string `json:"DATABASE"`
	Table     string `json:"TABLE"`
	PageIndex int    `json:"PAGE_INDEX"`
	PageSize  int    `json:"PAGE_SIZE"`
	Queries   []struct {
		QueryID string   `json:"QUERY_ID"`
		Roles   []string `json:"ROLES"`
		Select  string   `json:"SELECT"`
	} `json:"QUERIES"`
	TimeStart int64  `json:"time_start"`
	TimeEnd   int64  `json:"time_end"`
	Total     bool   `json:"TOTAL"`
	Sort      *sort  `json:"SORT,omitempty"`
}

// sort represents the ORDER BY clause from the request.
type sort struct {
	OrderBy  string `json:"ORDER_BY"`
	SortedBy string `json:"SORTED_BY"`
}

// QueryList executes a FlowLogDetailList query via deepflow-server.
// Returns TYPE: "Flow_Log_Detail_List".
func QueryList(zt *client.ZerotraceService, bodyStr string) (*query.Result, error) {
	if zt == nil {
		return &query.Result{
			Data:  []map[string]interface{}{},
			Count: 0,
			Type:  "Flow_Log_Detail_List",
		}, nil
	}

	var req listRequest
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
	//
	// Most DeepFlow DSL expressions (Enum, icon_id, node_type, newTag) are passed
	// through as-is — deepflow-server's /v1/query/ interprets them natively.
	//
	// Only map columns that don't exist in ClickHouse for the target table:
	//   event_desc → request_resource (flow_log, l7_flow_log)
	//   event_type → l7_protocol      (flow_log, l7_flow_log)
	//   protocol   → l7_protocol
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
	// Build WHERE clause
	// ---------------------------------------------------------------------------
	var timeClauses []string
	if req.TimeStart > 0 {
		timeClauses = append(timeClauses, fmt.Sprintf("time >= %d", req.TimeStart))
	}
	if req.TimeEnd > 0 {
		timeClauses = append(timeClauses, fmt.Sprintf("time <= %d", req.TimeEnd))
	}

	whereClause := ""
	if len(timeClauses) > 0 {
		whereClause = " WHERE " + strings.Join(timeClauses, " AND ")
	}

	// ---------------------------------------------------------------------------
	// ORDER BY
	// ---------------------------------------------------------------------------
	orderClause := ""
	if req.Sort != nil && req.Sort.OrderBy != "" {
		dir := "ASC"
		if strings.ToUpper(req.Sort.SortedBy) == "DESC" {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf(" ORDER BY `%s` %s", req.Sort.OrderBy, dir)
	}

	limit := req.PageSize
	if limit <= 0 {
		limit = 100
	}

	sql := fmt.Sprintf("SELECT %s FROM `%s`%s%s LIMIT %d",
		selectCols, tbl, whereClause, orderClause, limit)

	log.Printf("🔍 ZT FlowLogDetail: db=%s sql=%s", db, sql)

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
			Data:  []map[string]interface{}{},
			Count: 0,
			Type:  "Flow_Log_Detail_List",
		}, nil
	}

	// ---------------------------------------------------------------------------
	// Total count
	// ---------------------------------------------------------------------------
	totalCount := len(rows.Values)
	if req.Total {
		countSQL := fmt.Sprintf("SELECT count(*) AS cnt FROM `%s`%s", tbl, whereClause)
		countRows, err := zt.QueryRaw(db, countSQL)
		if err == nil && len(countRows.Values) > 0 && len(countRows.Values[0]) > 0 {
			switch v := countRows.Values[0][0].(type) {
			case float64:
				totalCount = int(v)
			case uint64:
				totalCount = int(v)
			case json.Number:
				if n, e := v.Int64(); e == nil {
					totalCount = int(n)
				}
			}
		}
	}

	// ---------------------------------------------------------------------------
	// Post-process
	// ---------------------------------------------------------------------------
	data := buildData(rows, "")
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
			if zh := enumZHCN(val); zh != "" {
				data[ir][col] = zh
			}
		}
	}

	// ---------------------------------------------------------------------------
	// SCHEMAS
	// ---------------------------------------------------------------------------
	schemas := buildSchemas(rows, queryID)

	return &query.Result{
		Data:   data,
		Count:  totalCount,
		Type:   "Flow_Log_Detail_List",
		Fields: schemas,
	}, nil
}
