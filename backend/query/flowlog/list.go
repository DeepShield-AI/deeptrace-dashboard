// Package flowlog implements FlowLogDetail list and info queries.
// Both use deepflow-server direct (zerotrace), bypassing DataSourceChain.
package flowlog

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	sortpkg "sort"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/enum"
	"deeptrace-backend/logging"
	"deeptrace-backend/query"
) // listRequest mirrors the JSON body of a FlowLogDetailList request.

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
	TimeStart int64 `json:"time_start"`
	TimeEnd   int64 `json:"time_end"`
	Total     bool  `json:"TOTAL"`
	Sort      *sort `json:"SORT,omitempty"`
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

	// FlowLogDetail is a ZT-direct path (no chain fallback): pre-check the
	// SELECT so unsupported aggregations fail clearly instead of round-tripping
	// to zerotrace (which rejects Avg/Sum/max/PerSecond at parse time).
	if len(req.Queries) > 0 && !ztSupportedSelect(req.Queries[0].Select) {
		return nil, fmt.Errorf("unsupported aggregation in FlowLogDetail select")
	}

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
					lowCol == "attribute" {
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
	if req.Sort != nil {
		sb, sd = req.Sort.OrderBy, req.Sort.SortedBy
	}
	extras := []string{}
	if len(req.Queries) > 0 && req.Queries[0].Where != "" {
		extras = append(extras, req.Queries[0].Where)
	}
	// Pagination: offset is derived from PAGE_INDEX/PAGE_SIZE (same as QueryListZT).
	// Page 2+ must not return the same rows as page 1.
	offset := 0
	if req.PageIndex > 1 && req.PageSize > 0 {
		offset = (req.PageIndex - 1) * req.PageSize
	}
	sql := query.BuildBaseSQL(selectCols, tbl, extras, req.TimeStart, req.TimeEnd,
		"", sb, sd, req.PageSize, offset)
	if req.PageSize <= 0 {
		sql += " LIMIT 100"
	}
	logging.Debugf("ZT FlowLogDetail: db=%s sql=%s", db, sql)

	// ---------------------------------------------------------------------------
	// Query deepflow-server
	// ---------------------------------------------------------------------------
	queryID := ""
	if len(req.Queries) > 0 {
		queryID = req.Queries[0].QueryID
	}

	rows, err := zt.QueryRaw(db, sql)
	if err != nil {
		logging.Errorf("FlowLogDetail ZT failed: %v", err)
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
	// Total count: queried on every TOTAL request (any page). A failed count
	// query keeps totalCount at 0 — using the page size as a guess would
	// misreport the real total; the envelope still emits COUNT via
	// TotalRequested, per the api_cache contract (TOTAL=true → COUNT always).
	totalCount := 0
	if req.Total {
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
			case "signal_source":
				enumName = "l7_signal_source"
			case "protocol":
				enumName = "l7_protocol"
			}
			// ZT may return English display name (string) or raw value (int/float).
			// Try raw column value first for EnumService lookup, fall back to ZT's value.
			val := row[col]
			if enumSvc != nil {
				if rawVal, ok2 := row[enumName]; ok2 && rawVal != nil {
					display := enumSvc.GetDisplay(enumName, rawVal)
					// logging.Debugf("enum %s: raw=%v display=%v", enumName, rawVal, display)
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
	// The cloud contract always carries a sum_log_count SCHEMAS entry for
	// FlowLogDetailList (pre_as "Sum(log_count)"); the DATA rows don't have
	// the column, but the frontend renders aggregates from SCHEMAS.
	if _, ok := schemas["sum_log_count"]; !ok {
		schemas["sum_log_count"] = map[string]interface{}{
			"label_type": "", "pre_as": "Sum(log_count)",
			"type": 1, "unit": "个", "value_type": "UInt64",
		}
	}

	return &query.Result{
		Data:           data,
		Count:          totalCount,
		Type:           "Flow_Log_Detail_List",
		Fields:         schemas,
		TotalRequested: req.Total,
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
	return strings.HasPrefix(col, "gprocess.biz_type")
}

// QueryListZT and QueryTopZT: ZT-based implementations.
func QueryListZT(zt *client.ZerotraceService, bodyStr string) (*query.Result, error) {
	if zt == nil || !zt.Available() {
		return nil, nil
	}
	var req query.QuerierListRequest
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, nil
	}
	req.NormalizeQuery()
	if len(req.Queries) == 0 {
		return nil, nil
	}
	db := req.Database
	tbl := req.Table
	if db == "" {
		db = "flow_log"
	}
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	resolvedTbl := tbl
	if db == "flow_metrics" && !strings.Contains(tbl, ".") {
		if req.DataSource != "" {
			resolvedTbl = tbl + "." + req.DataSource
		} else {
			resolvedTbl = tbl + ".1m"
		}
	}
	// If range exceeds 1 day (1440 min), clamp to last day and mark partial.
	ts := req.TimeStart
	te := req.TimeEnd
	isPartial := te-ts > 86400
	if isPartial {
		ts = te - 86400
	}
	sf, sd := "", ""
	if req.Sort != nil {
		sf, sd = req.Sort.OrderBy, req.Sort.SortedBy
	}
	if !ztSupportedSelect(req.Queries[0].Select) {
		return nil, nil // not served — the chain tries the next source
	}
	sql, _ := buildSQL(req.Queries[0].Select, resolvedTbl, req.Queries[0].Where,
		ts, te, "", "", 0,
		req.PageSize, req.PageIndex, req.Interval, sf, sd, false)
	if sql == "" {
		return nil, nil
	}
	logging.Debugf("querier.List: db=%s sql=%s", db, sql)

	qid := ""
	if len(req.Queries) > 0 {
		qid = req.Queries[0].QueryID
	}
	rows, err := zt.QueryRaw(db, sql)
	if err != nil {
		logging.Warnf("querier.List: ZT unavailable (%v), falling back to CH", err)
		return nil, nil
	}
	if len(rows.Values) == 0 {
		result := &query.Result{Data: []map[string]interface{}{}, Type: "Application_Detail_List"}
		if isPartial {
			// The window was clamped to the last day — the frontend must be
			// told the result is partial even when it is empty.
			result.OptStatus = "PARTIAL_RESULT"
			result.Description = "最大可查询时间为 1440 分钟"
		}
		return result, nil
	}
	data := BuildData(rows, "")
	schemas := BuildSchemas(rows, qid)
	fillListExtraFields(req.Queries[0].Select, data)
	translateEnumResults(data)
	os := rows.OptStatus
	if isPartial {
		os = "PARTIAL_RESULT"
	}
	if os == "" {
		os = "SUCCESS"
	}
	desc := ""
	if os == "PARTIAL_RESULT" {
		desc = "最大可查询时间为 1440 分钟"
	}
	return &query.Result{Data: data, Type: "Application_Detail_List", Fields: schemas, OptStatus: os, Description: desc}, nil
}

func QueryTopZT(zt *client.ZerotraceService, bodyStr string) (*query.Result, error) {
	if zt == nil || !zt.Available() {
		return nil, nil
	}
	var req query.QuerierListRequest
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, nil
	}
	req.NormalizeQuery()
	if len(req.Queries) == 0 {
		return nil, nil
	}
	db := req.Database
	tbl := req.Table
	if db == "" {
		db = "flow_log"
	}
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	resolvedTbl := tbl
	if db == "flow_metrics" && !strings.Contains(tbl, ".") {
		if req.DataSource != "" {
			resolvedTbl = tbl + "." + req.DataSource
		} else {
			resolvedTbl = tbl + ".1m"
		}
	}
	// If range exceeds 1 day (1440 min), clamp to last day and mark partial.
	ts := req.TimeStart
	te := req.TimeEnd
	isPartial := te-ts > 86400
	if isPartial {
		ts = te - 86400
	}
	sf, sd := "", ""
	if req.Sort != nil {
		sf, sd = req.Sort.OrderBy, req.Sort.SortedBy
	}
	if !ztSupportedSelect(req.Queries[0].Select) {
		return nil, nil // not served — the chain tries the next source
	}
	sql, hasHist := buildSQL(req.Queries[0].Select, resolvedTbl, req.Queries[0].Where,
		ts, te, req.Queries[0].GroupBy, req.Queries[0].Select,
		int(req.Top), req.PageSize, req.PageIndex, req.Interval, sf, sd, true)
	if sql == "" {
		return nil, nil
	}
	logging.Debugf("querier.Top: db=%s sql=%s", db, sql)

	qid := ""
	if len(req.Queries) > 0 {
		qid = req.Queries[0].QueryID
	}
	rows, err := zt.QueryRaw(db, sql)
	if err != nil {
		logging.Warnf("querier.Top: ZT unavailable (%v), falling back to CH", err)
		return nil, nil // not served — let the chain try the next source
	}
	if len(rows.Values) == 0 {
		result := &query.Result{Data: []map[string]interface{}{}}
		if isPartial {
			// The window was clamped to the last day — the frontend must be
			// told the result is partial even when it is empty.
			result.OptStatus = "PARTIAL_RESULT"
			result.Description = "最大可查询时间为 1440 分钟"
		}
		return result, nil
	}
	data := BuildData(rows, "")
	schemas := BuildSchemas(rows, qid)
	fillTopExtraFields(req.Queries[0].Select, data)
	translateEnumResults(data)
	if hasHist {
		data = query.BuildHistory(data, req.Queries[0].Select, req.Queries[0].Metrics,
			ts, te, int64(req.Interval), req.Fill)
	}
	fillTopMetaFields(data, req.Queries[0].Select)
	fillTopSchemas(schemas, req.Queries[0].Select, hasHist, req.Interval)
	os := rows.OptStatus
	if os == "" {
		os = "SUCCESS"
	}
	if isPartial {
		os = "PARTIAL_RESULT"
	}
	desc := ""
	if os == "PARTIAL_RESULT" {
		desc = "最大可查询时间为 1440 分钟"
	}
	return &query.Result{Data: data, Type: "", OptStatus: os, Description: desc}, nil
}

// fallbackTopQueryCH tries a direct CH query when ZT fails on Top queries.

func fillTopMetaFields(data []map[string]interface{}, sel string) {
	excl := computeExcludedColumns(sel)
	for _, row := range data {
		if _, ok := row["UID"]; ok {
			continue
		}
		var keys []string
		for k := range row {
			if k == "HISTORY" || k == "_querier_region" || k == "UID" || excl[k] {
				continue
			}
			keys = append(keys, k)
		}
		sortpkg.Strings(keys)
		var ups []string
		for _, k := range keys {
			if v := row[k]; v != nil {
				ups = append(ups, fmt.Sprintf("%s=%v", k, v))
			}
		}
		row["UID"] = strings.Join(ups, ",")
		if len(ups) > 0 {
			row["UID_NAME"] = ups[len(ups)-1]
		} else {
			row["UID_NAME"] = ""
		}
		if _, ok := row["Enum(response_status)"]; ok {
			row["FULL_NAME"] = fmt.Sprintf("响应状态=%v，响应状态=%v", row["Enum(response_status)"], row["response_status"])
		} else {
			row["FULL_NAME"] = ""
		}
		row["NAME"] = ""
		tm := map[string]interface{}{}
		for _, k := range keys {
			if v, ok := row[k]; ok && v != nil {
				tm[k] = v
			}
		}
		if b, err := json.Marshal(tm); err == nil {
			row["TAGS"] = string(b)
		} else {
			row["TAGS"] = "{}"
		}
	}
}

func computeExcludedColumns(sel string) map[string]bool {
	excl := map[string]bool{}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		if strings.HasPrefix(lower, "newtag(") || strings.HasPrefix(lower, "enum(") ||
			strings.HasPrefix(lower, "node_type(") || strings.HasPrefix(lower, "icon_id(") {
			continue
		}
		if strings.Contains(item.Expr, "(") {
			excl[item.Key] = true
		}
	}
	return excl
}

func fillTopSchemas(schemas map[string]interface{}, sel string, hasTimeGroupBy bool, interval int) {
	if schemas == nil {
		return
	}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		if _, exists := schemas[item.Key]; exists {
			continue
		}
		var e map[string]interface{}
		switch {
		case strings.HasPrefix(lower, "newtag("):
			e = map[string]interface{}{"label_type": "", "pre_as": item.Expr, "type": 0, "unit": "", "value_type": "String"}
		case strings.HasPrefix(lower, "node_type("):
			e = map[string]interface{}{"label_type": "", "pre_as": item.Expr, "type": 0, "unit": "", "value_type": "String"}
		case strings.HasPrefix(lower, "enum("):
			e = map[string]interface{}{"label_type": "", "pre_as": "", "type": 0, "unit": "", "value_type": "String"}
		case strings.HasPrefix(lower, "icon_id("):
			e = map[string]interface{}{"label_type": "", "pre_as": item.Expr, "type": 0, "unit": "", "value_type": "Int8"}
		default:
			var n float64
			if _, err := fmt.Sscanf(item.Expr, "%f", &n); err == nil {
				e = map[string]interface{}{"label_type": "", "pre_as": item.Expr, "type": 0, "unit": "", "value_type": "Int8"}
			}
		}
		if e != nil {
			schemas[item.Key] = e
		}
	}
	if hasTimeGroupBy {
		if _, exists := schemas["toi"]; !exists {
			iv := interval
			if iv <= 0 {
				iv = 0
			}
			schemas["toi"] = map[string]interface{}{"label_type": "", "pre_as": fmt.Sprintf("time(time, %d, 1, null)", iv), "type": 0, "unit": "", "value_type": "UInt32"}
		}
	}
}

func fillTopExtraFields(sel string, data []map[string]interface{}) {
	if sel == "" || len(data) == 0 {
		return
	}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		var val interface{}
		fill := false
		switch {
		case strings.HasPrefix(lower, "newtag("):
			v := strings.TrimSpace(item.Expr[len("newTag(") : len(item.Expr)-1])
			val = strings.Trim(v, "'\"")
			fill = true
		case strings.HasPrefix(lower, "node_type("):
			val = "_"
			fill = true
		case strings.HasPrefix(lower, "icon_id("):
			val = clickhouse.IconIDDefault(item.Key)
			fill = true
		case strings.HasPrefix(lower, "enum("):
			val = nil
			fill = true
			if len(data) > 0 {
				if v, ok := data[0][strings.TrimSpace(item.Expr[len("Enum("):len(item.Expr)-1])]; ok {
					val = v
				}
			}
		default:
			var n float64
			if _, err := fmt.Sscanf(item.Expr, "%f", &n); err == nil {
				val = n
				fill = true
			}
		}
		if fill {
			for _, row := range data {
				if _, exists := row[item.Key]; !exists {
					row[item.Key] = val
				}
			}
		}
	}
}

// translateEnumResults translates Enum() display values from English to Chinese.
func translateEnumResults(data []map[string]interface{}) {
	for _, row := range data {
		for k, v := range row {
			if !strings.HasPrefix(k, "Enum(") {
				continue
			}
			if s, ok := v.(string); ok {
				if cn := EnumZHCN(s); cn != "" {
					row[k] = cn
				}
			}
		}
	}
}

func fillListExtraFields(sel string, data []map[string]interface{}) {
	if sel == "" || len(data) == 0 {
		return
	}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		var val interface{}
		fill := false
		switch {
		case strings.HasPrefix(lower, "newtag("):
			v := strings.TrimSpace(item.Expr[len("newTag(") : len(item.Expr)-1])
			val = strings.Trim(v, "'\"")
			fill = true
		case strings.HasPrefix(lower, "node_type("):
			val = "_"
			fill = true
		case strings.HasPrefix(lower, "icon_id("):
			val = clickhouse.IconIDDefault(item.Key)
			fill = true
		case strings.HasPrefix(lower, "enum("):
			val = nil
			fill = true
			if len(data) > 0 {
				if v, ok := data[0][strings.TrimSpace(item.Expr[len("Enum("):len(item.Expr)-1])]; ok {
					val = v
				}
			}
		}
		if fill {
			for _, row := range data {
				if _, exists := row[item.Key]; !exists {
					row[item.Key] = val
				}
			}
		}
	}
}

func isNum(expr string) bool {
	var n float64
	_, err := fmt.Sscanf(expr, "%f", &n)
	return err == nil
}

func buildSQL(sel, tbl, whereCond string, timeStart, timeEnd int64,
	groupBy, origSel string, topN, pageSize, pageIndex, interval int,
	orderBy, sortedBy string, isTop bool,
) (string, bool) {
	if sel == "" {
		return "", false
	}
	extras := []string{}
	if whereCond != "" {
		extras = append(extras, whereCond)
	}

	if isTop {
		sel = cleanSelect(sel)
		if groupBy != "" {
			// HISTORY mode: add time bucket, build GROUP BY with passthrough stripping
			timeExpr := "`time`"
			sel = timeExpr + ", " + sel
			var gbParts []string
			gbParts = append(gbParts, stripPassthroughGroupBy(groupBy, origSel)...)
			gb := "time"
			if len(gbParts) > 0 {
				gb += ", " + strings.Join(gbParts, ", ")
			}
			span := 100
			if interval > 0 {
				span = int((timeEnd - timeStart) / int64(interval))
			}
			if span <= 0 {
				span = 100
			}
			return query.BuildBaseSQL(sel, tbl, extras, timeStart, timeEnd,
				gb, "time", "DESC", span, 0), true
		}
	}

	// Non-HISTORY (List, Top without groupBy)
	limit := pageSize
	if topN > 0 {
		limit = topN
	}
	if limit <= 0 {
		limit = 100
	}
	offset := 0
	if pageIndex > 1 {
		offset = (pageIndex - 1) * pageSize
	}
	return query.BuildBaseSQL(sel, tbl, extras, timeStart, timeEnd,
		"", orderBy, sortedBy, limit, offset), false
}

// ztSupportedSelect reports whether the deepflow-server SQL engine can
// handle the SELECT clause. The local zerotrace-server only implements the
// Count aggregation — Avg/Sum/max/min/PerSecond/Uniq are rejected at parse
// time ("function: Avg(x) not support"), so we pre-check and let the chain
// fall through instead of paying the failed round-trip.
func ztSupportedSelect(sel string) bool {
	lower := strings.ToLower(sel)
	for _, f := range []string{"avg(", "sum(", "max(", "min(", "persecond(", "uniq("} {
		if strings.Contains(lower, f) {
			return false
		}
	}
	return true
}

// cleanSelect strips DeepFlow DSL functions for Top GROUP BY queries.
func cleanSelect(sel string) string {
	if sel == "" {
		return ""
	}
	re := regexp.MustCompile(`(?i)(?:newTag|node_type|Enum|icon_id)\s*\([^)]*\)\s+AS\s+(?:[a-zA-Z_0-9]+|` + "`[^`]+`" + `)\s*,?\s*`)
	sel = re.ReplaceAllString(sel, "")
	re2 := regexp.MustCompile(`(?i)-?\d+\.?\d*\s+AS\s+(?:[a-zA-Z_0-9]+|` + "`[^`]+`" + `)\s*,?\s*`)
	sel = re2.ReplaceAllString(sel, "")
	sel = strings.TrimSpace(sel)
	return strings.TrimRight(sel, ", ")
}

// stripPassthroughGroupBy removes passthrough aliases from GROUP BY.
func stripPassthroughGroupBy(groupBy, sel string) []string {
	if groupBy == "" {
		return nil
	}
	aliases := map[string]bool{}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		if strings.HasPrefix(lower, "newtag(") ||
			strings.HasPrefix(lower, "enum(") ||
			strings.HasPrefix(lower, "node_type(") ||
			strings.HasPrefix(lower, "icon_id(") ||
			isNum(item.Expr) {
			aliases[item.Key] = true
		}
	}
	var result []string
	for _, item := range clickhouse.ParseSelectList(groupBy) {
		col := strings.Trim(item.Key, "`")
		if aliases[col] {
			continue
		}
		result = append(result, fmt.Sprintf("`%s`", col))
	}
	return result
}

// QueryFlowLogDetailCH: ClickHouse direct query.
// QueryFlowLogDetailCH queries ClickHouse directly for FlowLog detail data.
func QueryFlowLogDetailCH(ch *clickhouse.CHService, ctx context.Context, bodyStr string) (*query.QueryFlowLogResult, error) {
	var req struct {
		Database string `json:"DATABASE"`
		Table    string `json:"TABLE"`
		Queries  []struct {
			QueryID string   `json:"QUERY_ID"`
			Roles   []string `json:"ROLES"`
			Select  string   `json:"SELECT"`
		} `json:"QUERIES"`
		TimeStart int64 `json:"time_start"`
		TimeEnd   int64 `json:"time_end"`
	}
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
	resolvedTable := tbl
	if !strings.Contains(tbl, ".") && db == "flow_metrics" {
		resolvedTable = tbl + ".1m"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)

	colMap := map[string]string{
		"auto_service_0":     "if(empty(app_service), toString(auto_service_id_0), app_service)",
		"auto_instance_0":    "if(empty(app_instance), toString(auto_instance_id_0), app_instance)",
		"auto_service_1":     "if(empty(app_service), toString(auto_service_id_1), app_service)",
		"auto_instance_1":    "if(empty(app_instance), toString(auto_instance_id_1), app_instance)",
		"auto_service_id_0":  "auto_service_id_0",
		"auto_service_id_1":  "auto_service_id_1",
		"auto_instance_id_0": "auto_instance_id_0",
		"auto_instance_id_1": "auto_instance_id_1",
		"protocol":           "l7_protocol",
		"_id":                "toString(_id)",
	}
	isFlowLogDetail := db == "flow_log"
	if isFlowLogDetail {
		colMap["event_type"] = "l7_protocol"
		colMap["auto_instance"] = "if(empty(app_instance), toString(auto_instance_id_0), app_instance)"
		colMap["event_desc"] = "request_resource"
	}

	selectCols := "*"
	if len(req.Queries) > 0 && req.Queries[0].Select != "" {
		items := clickhouse.ParseSelectList(req.Queries[0].Select)
		var cols []string
		for _, item := range items {
			lower := strings.ToLower(item.Expr)
			switch {
			case strings.HasPrefix(lower, "newtag("):
				continue
			case strings.HasPrefix(lower, "enum("):
				inner := strings.TrimSpace(item.Expr[len("Enum(") : len(item.Expr)-1])
				cols = append(cols, fmt.Sprintf("`%s` AS `%s`", inner, item.Key))
			case strings.HasPrefix(lower, "icon_id("):
				cols = append(cols, fmt.Sprintf("-13 AS `%s`", item.Key))
			case strings.HasPrefix(lower, "node_type("):
				inner := strings.TrimSpace(item.Expr[len("node_type(") : len(item.Expr)-1])
				cols = append(cols, fmt.Sprintf("toString(`%s`) AS `%s`", inner, item.Key))
			default:
				col := strings.Trim(item.Expr, "`")
				if mapped, ok := colMap[col]; ok {
					cols = append(cols, fmt.Sprintf("%s AS `%s`", mapped, item.Key))
				} else if col != item.Key {
					cols = append(cols, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
				} else {
					cols = append(cols, fmt.Sprintf("`%s`", col))
				}
			}
		}
		if len(cols) > 0 {
			selectCols = strings.Join(cols, ", ")
		}
	}

	var wheres []string
	if req.TimeStart > 0 {
		wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart))
	}
	if req.TimeEnd > 0 {
		wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd))
	}

	sql := fmt.Sprintf("SELECT %s FROM %s", selectCols, fullTable)
	if len(wheres) > 0 {
		sql += " WHERE " + strings.Join(wheres, " AND ")
	}
	sql += " LIMIT 500"

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := ch.Query(qCtx, sql)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	rawData, err := clickhouse.ScanRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if rawData == nil {
		rawData = []map[string]interface{}{}
	}

	// Enum display maps come from the canonical enum package (single source of truth).
	// Extend event_type with string keys used by CH-stored values.
	enumDisplay := enum.FallbackEnumMaps()
	if emap, ok := enumDisplay["event_type"]; ok {
		emap["read"] = "读"
		emap["write"] = "写"
		emap["create"] = "创建"
		emap["delete"] = "删除"
	}

	var processed []map[string]interface{}
	for _, row := range rawData {
		row["_querier_region"] = clickhouse.QuerierRegion
		for k, v := range row {
			if v == nil {
				if k == "response_code" {
					row[k] = 0
				}
				if k == "response_exception" {
					row[k] = ""
				}
				continue
			}
			strVal := fmt.Sprintf("%v", v)
			if strings.HasPrefix(k, "Enum(") && strings.HasSuffix(k, ")") {
				innerKey := k[5 : len(k)-1]
				if emap, ok := enumDisplay[innerKey]; ok {
					if display, ok2 := emap[strVal]; ok2 {
						row[k] = display
					}
				}
			}
			if k == "event_type" {
				enumName := "event_type"
				if isFlowLogDetail {
					enumName = "l7_protocol"
				}
				if emap, ok := enumDisplay[enumName]; ok {
					if display, ok2 := emap[strVal]; ok2 {
						row[k] = display
					}
				}
			}
			if k == "_id" {
				row[k] = strVal
			}
			if k == "start_time" || k == "end_time" {
				switch val := v.(type) {
				case float64:
					row[k] = time.UnixMicro(int64(val)).Format("2006-01-02T15:04:05.000000-07:00")
				case uint64:
					row[k] = time.UnixMicro(int64(val)).Format("2006-01-02T15:04:05.000000-07:00")
				case int64:
					row[k] = time.UnixMicro(val).Format("2006-01-02T15:04:05.000000-07:00")
				}
			}
			if strings.Contains(k, "icon_id") {
				if fv, ok := v.(float64); ok && fv == 0 {
					row[k] = float64(-16)
				}
			}
		}
		processed = append(processed, row)
	}
	return &query.QueryFlowLogResult{Data: processed}, nil
}
