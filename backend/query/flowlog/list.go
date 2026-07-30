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
	"deeptrace-backend/enum"
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
		Where   string   `json:"WHERE"`
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
func QueryList(zt *client.ZerotraceService, enumSvc *enum.EnumService, bodyStr string) (*query.Result, error) {
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
			case strings.HasPrefix(lower, "enum("):
				inner := strings.Trim(expr[5:len(expr)-1], "`")
				// is_async/is_tls may not exist in local CH; use empty fallback.
				if isEnumUnsupported(strings.ToLower(inner)) {
					cols = append(cols, fmt.Sprintf("'' AS `%s`", key))
				} else {
					cols = append(cols, fmt.Sprintf("%s AS `%s`", expr, key))
				}
			case strings.HasPrefix(lower, "newtag("),
				strings.HasPrefix(lower, "icon_id("),
				strings.HasPrefix(lower, "node_type("):
				cols = append(cols, fmt.Sprintf("%s AS `%s`", expr, key))

			default:
				col := strings.Trim(expr, "`")

				// Columns not in local CH: replace with empty to avoid ZT failures.
				lowCol := strings.ToLower(col)
				if lowCol == "is_async" || lowCol == "is_tls" || lowCol == "role" ||
					strings.HasPrefix(lowCol, "gprocess.biz_type") ||
					strings.HasPrefix(lowCol, "k8s.annotation_") ||
					strings.HasPrefix(lowCol, "cloud.tag_") ||
					lowCol == "attribute" ||
					false { // process_/x_request_ now mapped to process_id_/x_request_id_
					cleanKey := strings.Trim(key, "`")
					cols = append(cols, fmt.Sprintf("'' AS `%s`", cleanKey))
					continue
				}

				if isFlowLog && tbl == "l7_flow_log" {
					switch col {
					case "event_desc":
						col = "request_resource"
					case "event_type":
						col = "l7_protocol"
					case "epc_0":
						col = "epc_id_0"
					case "epc_1":
						col = "epc_id_1"
					case "process_0":
						col = "process_id_0"
					case "process_1":
						col = "process_id_1"
					case "x_request_0":
						col = "x_request_id_0"
					case "x_request_1":
						col = "x_request_id_1"
					case "k8s.label_0":
						col = "pod_id_0"
					case "k8s.label_1":
						col = "pod_id_1"
					case "k8s.annotation_0":
						col = "pod_service_id_0"
					case "k8s.annotation_1":
						col = "pod_service_id_1"
					case "k8s.env_0":
						col = "pod_id_0"
					case "k8s.env_1":
						col = "pod_id_1"
					case "cloud.tag_0":
						col = "l3_device_id_0"
					case "cloud.tag_1":
						col = "l3_device_id_1"
					case "os.app_0":
						col = "gprocess_id_0"
					case "os.app_1":
						col = "gprocess_id_1"
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

	sb, sd := "", ""
	if req.Sort != nil { sb, sd = req.Sort.OrderBy, req.Sort.SortedBy }
	extras := []string{}
	if len(req.Queries) > 0 && req.Queries[0].Where != "" {
		extras = append(extras, req.Queries[0].Where)
	}
	sql := query.BuildBaseSQL(selectCols, tbl, extras, req.TimeStart, req.TimeEnd,
		"", sb, sd, req.PageSize, 0)
	if req.PageSize <= 0 { sql += " LIMIT 100" }
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
		log.Printf("⚠️  FlowLogDetail ZT failed: %v", err)
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
	totalCount := 0
	if req.Total && req.PageIndex <= 1 {
		totalCount = len(rows.Values)
		countSQL := query.BuildBaseSQL("Count(row) AS cnt", tbl, extras, req.TimeStart, req.TimeEnd, "", "", "", 0, 0)
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
	data := BuildData(rows, "")
	for ir, row := range data {
		for ic, col := range rows.Columns {
			if ic >= len(rows.Schemas) {
				continue
			}
			preAs := strings.ToLower(rows.Schemas[ic].PreAs)
			if !strings.HasPrefix(preAs, "enum(") {
				continue
			}
			enumName := strings.TrimPrefix(preAs, "enum(")
			enumName = strings.TrimSuffix(enumName, ")")
			// Map API tag name to int_enum_map tag name (ZT uses l7_ prefix for flow_log).
			switch enumName {
			case "signal_source": enumName = "l7_signal_source"
			case "protocol": enumName = "l7_protocol"
			}
			// ZT may return English display name (string) or raw value (int/float).
			// Try raw column value first for EnumService lookup, fall back to ZT's value.
			val := row[col]
			if enumSvc != nil {
				if rawVal, ok2 := row[enumName]; ok2 && rawVal != nil {
					display := enumSvc.GetDisplay(enumName, rawVal)
					log.Printf("enum %s: raw=%v display=%v", enumName, rawVal, display)
					data[ir][col] = display
				}
			} else if s, ok := val.(string); ok && s != "" {
				if zh := EnumZHCN(s); zh != "" {
					data[ir][col] = zh
				}
			}
		}
	}

	// ---------------------------------------------------------------------------
	// SCHEMAS
	// ---------------------------------------------------------------------------
	schemas := BuildSchemas(rows, queryID)

	return &query.Result{
		Data:   data,
		Count:  totalCount,
		Type:   "Flow_Log_Detail_List",
		Fields: schemas,
	}, nil
}



// isEnumUnsupported reports whether the local ZT/deepflow-server doesn't support Enum() for this column.
func isEnumUnsupported(col string) bool {
	switch col {
	case "is_async", "is_tls", "is_reversed", "is_ipv4", "is_internet_0", "is_internet_1",
		"tunnel_type", "span_kind", "nat_source", 
		"tap_side":
		return true
	}
	if strings.HasPrefix(col, "gprocess.biz_type") {
		return true
	}
	return false
}
