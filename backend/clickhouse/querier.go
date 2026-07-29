package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"deeptrace-backend/engine"
)

// metricExpr holds a parsed metric expression for Top queries.
type metricExpr struct {
	key string
	sql string
}

// ---------------------------------------------------------------------------
// Data-returning query methods (no HTTP coupling)
// ---------------------------------------------------------------------------

// QueryListResult holds the result of a List query.
type QueryListResult struct {
	Data   []map[string]interface{}
	Fields map[string]interface{} // SCHEMAS
	Count  int                    // total matching records (not just returned page)
}

// QueryTopResult holds the result of a Top query.
type QueryTopResult struct {
	Data   []map[string]interface{}
	Fields map[string]interface{}
}

// QueryFlowLogResult holds the result of a FlowLogDetail query.
type QueryFlowLogResult struct {
	Data []map[string]interface{}
}

// QueryTraceMapResult holds the result of a TraceMap query.
type QueryTraceMapResult struct {
	Data             []map[string]interface{}
	TotalTraces      int
	CalculatedTraces int
}

// ---------------------------------------------------------------------------
// QueryList — returns structured List data from ClickHouse
// ---------------------------------------------------------------------------

func (s *CHService) QueryList(ctx context.Context, req *QuerierRequest) (*QueryListResult, error) {
	sql, err := BuildSelectSQL(*req)
	if err != nil {
		return nil, fmt.Errorf("build sql: %w", err)
	}
	log.Printf("CH List: %s", sql)

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := s.Query(qCtx, sql)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	data, err := ScanRows(rows)
	if err != nil {
		log.Printf("CH scan error: %v", err)
		return nil, fmt.Errorf("scan: %w", err)
	}
	if data == nil {
		data = []map[string]interface{}{}
	}

	// Build pre_as map from SELECT expressions.
	preAsMap := map[string]string{}
	if len(req.Queries) > 0 && req.Queries[0].Select != "" {
		for _, item := range ParseSelectList(req.Queries[0].Select) {
			if item.Key != item.Expr {
				preAsMap[item.Key] = strings.ReplaceAll(item.Expr, "`", "")
			}
		}
	}

	// Build SCHEMAS from first row.
	schemas := map[string]interface{}{}
	if len(data) > 0 {
		for k, v := range data[0] {
			vt, tp := "String", 0
			switch v.(type) {
			case float64:
				vt, tp = "Float64", 1
			case float32:
				vt, tp = "Float64", 1
			case int, int64, uint64:
				vt, tp = "UInt64", 1
				// icon_id is always a tag (even when int like -42).
				if strings.Contains(strings.ToLower(k), "icon_id") {
					tp = 0
					vt = "String"
				}
			}
			preAs := ""
			if p, ok := preAsMap[k]; ok {
				preAs = p
			}
			schemas[k] = map[string]interface{}{
				"label_type": "", "pre_as": preAs, "type": tp,
				"unit": "", "value_type": vt,
			}
		}
	}

	// Compute total count via separate count query.
	count := len(data)
	if idx := strings.Index(sql, " FROM "); idx >= 0 {
		countSQL := "SELECT count(*) AS cnt" + sql[idx:]
		if oIdx := strings.Index(countSQL, " ORDER BY "); oIdx >= 0 {
			countSQL = countSQL[:oIdx]
		}
		if lIdx := strings.Index(countSQL, " LIMIT "); lIdx >= 0 {
			countSQL = countSQL[:lIdx]
		}
		cRows, cErr := s.Query(ctx, countSQL)
		if cErr == nil {
			if cData, cScanErr := ScanRows(cRows); cScanErr == nil && len(cData) > 0 {
				if cv, ok := cData[0]["cnt"]; ok {
					if f, ok2 := cv.(float64); ok2 {
						count = int(f)
					}
				}
			}
			if cRows != nil {
				cRows.Close()
			}
		}
	}
	return &QueryListResult{
		Data:   data,
		Fields: schemas,
		Count:  count,
	}, nil
}

// ---------------------------------------------------------------------------
// QueryTop — returns structured TopN data from ClickHouse
// ---------------------------------------------------------------------------

func (s *CHService) QueryTop(ctx context.Context, req *QuerierRequest) (*QueryTopResult, error) {
	db := req.Database
	table := req.Table
	if db == "" {
		db = "flow_log"
	}
	if table == "" {
		table = "l7_flow_log"
	}
	resolvedTable := table
	if !strings.Contains(table, ".") && db == "flow_metrics" {
		if req.DataSource != "" {
			resolvedTable = table + "." + req.DataSource
		} else {
			resolvedTable = table + ".1m"
		}
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)

	if len(req.Queries) == 0 {
		return nil, fmt.Errorf("no queries")
	}
	q := req.Queries[0]
	items := ParseSelectList(q.Select)

	var metricExprs []metricExpr
	isFlowLog := db == "flow_log"

	for _, item := range items {
		lower := strings.ToLower(item.Expr)

		if strings.HasPrefix(lower, "percentile(") {
			inner := item.Expr[len("Percentile(") : len(item.Expr)-1]
			commaIdx := strings.LastIndex(inner, ",")
			if commaIdx > 0 {
				field := strings.TrimSpace(inner[:commaIdx])
				pct := strings.TrimSpace(inner[commaIdx+1:])
				metricExprs = append(metricExprs, metricExpr{
					item.Key, fmt.Sprintf("quantile(%s)(`%s`)", pct, strings.ReplaceAll(field, "`", "")),
				})
			}
			continue
		}

		if strings.HasPrefix(lower, "newtag(") ||
			strings.HasPrefix(lower, "icon_id(") ||
			strings.HasPrefix(lower, "node_type(") ||
			strings.HasPrefix(lower, "enum(") {
			continue
		}

		if _, err := fmt.Sscanf(item.Expr, "%f", new(float64)); err == nil {
			continue
		}

		if IsAggExpr(item.Expr) {
			sqlExpr := NormalizeExpr(item.Expr)

			if isFlowLog {
				// flow_log table: override metricMaps designed for flow_metrics.
				// rrt/rtt is stored as response_duration in flow_log tables.
				sqlExpr = strings.ReplaceAll(sqlExpr, "rrt_sum / nullif(rrt_count, 0)", "response_duration")
				sqlExpr = strings.ReplaceAll(sqlExpr, "rtt_sum / nullif(rtt_count, 0)", "response_duration")
				// server_error/client_error columns don't exist; use response_status.
				sqlExpr = strings.ReplaceAll(sqlExpr, "nullif(server_error, 0) / nullif(request, 0)", "if(response_status >= 500, 1, 0)")
				sqlExpr = strings.ReplaceAll(sqlExpr, "nullif(client_error, 0) / nullif(request, 0)", "if(response_status >= 400 AND response_status < 500, 1, 0)")
			// request column doesn't exist in flow_log; each row is one request.
			sqlExpr = strings.ReplaceAll(sqlExpr, "avg(request)", "count(*)")
			sqlExpr = strings.ReplaceAll(sqlExpr, "sum(request)", "count(*)")
			sqlExpr = strings.ReplaceAll(sqlExpr, "`request`", "1")

				cleanExpr := strings.ToLower(strings.ReplaceAll(item.Expr, "`", ""))
				if !strings.Contains(cleanExpr, "rrt") && !strings.Contains(cleanExpr, "rtt") &&
					!strings.Contains(cleanExpr, "response_duration") &&
					!strings.Contains(cleanExpr, "response_code") &&
					!strings.Contains(cleanExpr, "response_status") &&
					!strings.Contains(cleanExpr, "request") &&
					!strings.Contains(cleanExpr, "request_type") &&
					!strings.Contains(cleanExpr, "request_domain") &&
					!strings.Contains(cleanExpr, "request_resource") &&
					!strings.Contains(cleanExpr, "response_exception") &&
					!strings.Contains(cleanExpr, "response_result") &&
					!strings.Contains(cleanExpr, "server_error") &&
					!strings.Contains(cleanExpr, "client_error") {
					sqlExpr = "count(*)"
				}
			} else {
				// flow_metrics: rrt → rrt_sum/greatest(rrt_count,1)
				if !strings.Contains(sqlExpr, "rrt_sum") && !strings.Contains(sqlExpr, "rrt_count") {
					sqlExpr = strings.ReplaceAll(sqlExpr, "rrt", "rrt_sum / greatest(rrt_count, 1)")
				}
				if !strings.Contains(sqlExpr, "rtt_sum") && !strings.Contains(sqlExpr, "rtt_count") {
					sqlExpr = strings.ReplaceAll(sqlExpr, "rtt", "rtt_sum / greatest(rtt_count, 1)")
				}
			}
			metricExprs = append(metricExprs, metricExpr{item.Key, sqlExpr})
		}
	}

	if len(metricExprs) == 0 {
		return nil, fmt.Errorf("no metric expressions found")
	}

	var wheres []string
	if req.TimeStart > 0 {
		wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart))
	}
	if req.TimeEnd > 0 {
		wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd))
	}
	if q.Where != "" {
		cleanWhere := cleanWhereClause(q.Where)
		if cleanWhere != "" {
			wheres = append(wheres, cleanWhere)
		}
	}
	whereClause := ""
	if len(wheres) > 0 {
		whereClause = " WHERE " + strings.Join(wheres, " AND ")
	}

	var metricSelects []string
	for _, m := range metricExprs {
		metricSelects = append(metricSelects, fmt.Sprintf("%s AS `%s`", m.sql, m.key))
	}

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var selectCols []string
	if req.Interval > 0 {
		selectCols = append(selectCols, "toUnixTimestamp(toStartOfInterval(time, toIntervalSecond(1))) AS `time`")
	}
	selectCols = append(selectCols, metricSelects...)
	limitPart := " LIMIT 1"
	groupPart := ""
	if req.Interval > 0 {
		limitPart = ""
		groupPart = " GROUP BY `time` ORDER BY `time`"
	}
	querySQL := fmt.Sprintf("SELECT %s FROM %s%s%s%s", strings.Join(selectCols, ", "), fullTable, whereClause, groupPart, limitPart)
	log.Printf("CH Top SQL: %s", querySQL)

	rows, err := s.Query(qCtx, querySQL)
	if err != nil {
		log.Printf("CH query error: %v", err)
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	data, err := ScanRows(rows)
	if err != nil {
		log.Printf("CH scan error: %v", err)
		return nil, fmt.Errorf("scan: %w", err)
	}
	log.Printf("CH QueryTop OK: %d rows", len(data))

	topColMap := map[string]string{
		"auto_service":    "app_service",
		"auto_instance":   "app_instance",
		"auto_instance_0": "auto_instance_id_0",
		"auto_instance_1": "auto_instance_id_1",
		"chost":           "l3_device_id",
		"chost_id":        "l3_device_id",
		"vpc":             "epc_id",
		"vpc_id":          "epc_id",
		"pod_service":     "pod_service_id",
		"pod_service_id":  "pod_service_id",
		"pod_group":       "pod_group_id",
		"pod_group_id":    "pod_group_id",
		"pod_cluster":     "pod_cluster_id",
		"pod_cluster_id":  "pod_cluster_id",
		"pod_ns":          "pod_ns_id",
		"pod_ns_id":       "pod_ns_id",
	}

	// flow_log-specific column mappings for DeepFlow field names.
	flowLogColMap := map[string]string{}
	if isFlowLog {
		flowLogColMap = map[string]string{
			"event_type":    "l7_protocol",
			"auto_instance": "if(empty(app_instance), toString(auto_instance_id_0), app_instance)",
			"event_desc":    "request_resource",
			// auto_service_0/1: resolve name via dictGet(device_map) matching cloud behavior.
			"auto_service_0": "if(auto_service_type_0 IN (0, 255), if(any(is_ipv4) = 1, IPv4NumToString(any(ip4_0)), IPv6NumToString(any(ip6_0))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_0), toUInt64(auto_service_id_0)), ''))",
			"auto_service_1": "if(auto_service_type_1 IN (0, 255), if(any(is_ipv4) = 1, IPv4NumToString(any(ip4_1)), IPv6NumToString(any(ip6_1))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_1), toUInt64(auto_service_id_1)), ''))",
			// Virtual ZT columns: map to real ClickHouse columns for flow_log.
			"client_node_type": "auto_service_type_0",
			"server_node_type": "auto_service_type_1",
		}
	}

	// flow_log columns that don't exist in raw ClickHouse: skip from tags/group.
	flowLogSkipCols := map[string]bool{}
	if isFlowLog {
		flowLogSkipCols = map[string]bool{
			"is_internet_0": true, "is_internet_1": true,
		}
	}

	// If tags present, do grouped aggregation.
	var tagCols []string
	var groupCols []string
	for _, t := range q.Tags {
		rawExpr, colName := t, t
		if idx := strings.LastIndex(strings.ToUpper(t), " AS "); idx >= 0 {
			rawExpr = strings.TrimSpace(t[:idx])
			colName = strings.TrimSpace(t[idx+4:])
			colName = strings.Trim(colName, "`")
		}
		if colName == "" {
			continue
		}
		lowerExpr := strings.ToLower(strings.TrimSpace(rawExpr))
		if strings.HasPrefix(lowerExpr, "node_type(") ||
			strings.HasPrefix(lowerExpr, "icon_id(") ||
			strings.HasPrefix(lowerExpr, "newtag(") ||
			strings.HasPrefix(lowerExpr, "enum(") {
			continue
		}
		if _, err := fmt.Sscanf(rawExpr, "%f", new(float64)); err == nil {
			continue
		}
		// Skip virtual columns that don't exist in raw ClickHouse.
		if isFlowLog && flowLogSkipCols[colName] {
			continue
		}
		mappedCol := colName
		if m, ok := topColMap[colName]; ok {
			mappedCol = m
		} else if m2, ok2 := flowLogColMap[colName]; ok2 {
			mappedCol = m2
		}
		// For flow_log, dictGet/IP expressions go in SELECT (wrapped in any() if needed) but NOT in GROUP BY.
		if isFlowLog && (strings.Contains(mappedCol, "dictGet") || strings.Contains(mappedCol, "IPv4")) {
			// Expression already includes aggregate functions (any()), just use as-is.
			if strings.Contains(mappedCol, "any(") {
				tagCols = append(tagCols, fmt.Sprintf("%s AS `%s`", mappedCol, colName))
			} else {
				tagCols = append(tagCols, fmt.Sprintf("any(%s) AS `%s`", mappedCol, colName))
			}
			// Don't add to groupCols — the ID columns already cover the grouping.
		} else {
			tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mappedCol, colName))
			groupCols = append(groupCols, fmt.Sprintf("`%s`", mappedCol))
		}
	}

	// Also process GROUP_BY (flat Top format) into tag/group cols.
	for _, gb := range strings.Split(q.GroupBy, ",") {
		gb = strings.TrimSpace(gb)
		gb = strings.Trim(gb, "`")
		if gb == "" {
			continue
		}
		// Skip if already in tagCols from TAGS.
		already := false
		for _, tc := range tagCols {
			if strings.Contains(tc, "`"+gb+"`") {
				already = true
				break
			}
		}
		if already {
			continue
		}
		// Skip passthrough DSL functions and numeric literals.
		lowerGb := strings.ToLower(gb)
		if strings.HasPrefix(lowerGb, "node_type(") ||
			strings.HasPrefix(lowerGb, "icon_id(") ||
			strings.HasPrefix(lowerGb, "newtag(") ||
			strings.HasPrefix(lowerGb, "enum(") {
			continue
		}
		if _, err := fmt.Sscanf(gb, "%f", new(float64)); err == nil {
			continue
		}
		if isFlowLog && flowLogSkipCols[gb] {
			continue
		}
		mappedCol := gb
		if m, ok := topColMap[gb]; ok {
			mappedCol = m
		} else if m2, ok2 := flowLogColMap[gb]; ok2 {
			mappedCol = m2
		}
		// For flow_log, dictGet/IP expressions go in SELECT (wrapped in any() if needed) but NOT in GROUP BY.
		if isFlowLog && (strings.Contains(mappedCol, "dictGet") || strings.Contains(mappedCol, "IPv4")) {
			if strings.Contains(mappedCol, "any(") {
				tagCols = append(tagCols, fmt.Sprintf("%s AS `%s`", mappedCol, gb))
			} else {
				tagCols = append(tagCols, fmt.Sprintf("any(%s) AS `%s`", mappedCol, gb))
			}
		} else {
			tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mappedCol, gb))
			groupCols = append(groupCols, fmt.Sprintf("`%s`", mappedCol))
		}
	}

	if len(tagCols) > 0 {
		groupSQL := fmt.Sprintf("SELECT %s, %s FROM %s%s GROUP BY %s",
			strings.Join(metricSelects, ", "), strings.Join(tagCols, ", "),
			fullTable, whereClause, strings.Join(groupCols, ", "))
		if req.PageSize > 0 {
			groupSQL += fmt.Sprintf(" LIMIT %d", req.PageSize)
		} else {
			groupSQL += " LIMIT 50"
		}
		log.Printf("CH Top grouped SQL: %s", groupSQL)

		rows.Close()
		gRows, gErr := s.Query(qCtx, groupSQL)
		if gErr == nil {
			defer gRows.Close()
			if gData, gErr2 := ScanRows(gRows); gErr2 == nil {
				data = gData
				log.Printf("CH Top grouped: %d rows", len(data))
			} else {
				log.Printf("⚠️  CH Top grouped scan error: %v", gErr2)
			}
		} else {
			log.Printf("⚠️  CH Top grouped query error: %v", gErr)
		}
	}

	if len(data) == 0 {
		return &QueryTopResult{Data: []map[string]interface{}{}}, nil
	}

	var resultRows []map[string]interface{}
	seenUID := map[string]bool{}
	for _, row := range data {
		var uidParts []string
		for _, tc := range q.Tags {
			cn := tc
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(cn[idx+4:])
				cn = strings.Trim(cn, "`")
			}
			if cn != "" {
				if v, ok := row[cn]; ok {
					uidParts = append(uidParts, fmt.Sprintf("%s=%v", cn, v))
				}
			}
		}
		uid := "_"
		if len(uidParts) > 0 {
			uid = strings.Join(uidParts, ",")
		}

		resultRow := map[string]interface{}{"_querier_region": "本地", "UID": uid}
		for _, tc := range q.Tags {
			cn := tc
			rawExpr := strings.TrimSpace(tc)
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(tc[idx+4:])
				cn = strings.Trim(cn, "`")
				rawExpr = strings.TrimSpace(tc[:idx])
			}
			if cn == "" {
				continue
			}
			if v, ok := row[cn]; ok {
				resultRow[cn] = v
			} else {
				// Compute default from DSL tag expression for CH path (no deepflow-server resolution).
				lowerExpr := strings.ToLower(rawExpr)
				if strings.HasPrefix(lowerExpr, "node_type(") || strings.HasPrefix(lowerExpr, "newtag(") {
					s := strings.Index(rawExpr, "(")
					e := strings.LastIndex(rawExpr, ")")
					if s >= 0 && e > s {
						resultRow[cn] = strings.Trim(rawExpr[s+1:e], "`\x27 ")
					}
				} else if strings.HasPrefix(lowerExpr, "-42") {
					resultRow[cn] = int(-42)
				} else if n, _ := fmt.Sscanf(rawExpr, "%d", new(int)); n == 1 {
					var num int
					fmt.Sscanf(rawExpr, "%d", &num)
					resultRow[cn] = num
				}
			}
		}
		for _, m := range metricExprs {
			if v, ok := row[m.key]; ok {
				resultRow[m.key] = v
			}
		}

		// Per-group history.
		histWheres := make([]string, len(wheres))
		copy(histWheres, wheres)
		for _, tc := range q.Tags {
			cn := tc
			rawExpr := strings.TrimSpace(tc)
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(cn[idx+4:])
				cn = strings.Trim(cn, "`")
				rawExpr = strings.TrimSpace(rawExpr[:idx])
			}
			// Skip DSL function tags: they don't map to physical ClickHouse columns.
			lowerExpr := strings.ToLower(rawExpr)
			if strings.HasPrefix(lowerExpr, "node_type(") ||
				strings.HasPrefix(lowerExpr, "icon_id(") ||
				strings.HasPrefix(lowerExpr, "newtag(") ||
				strings.HasPrefix(lowerExpr, "enum(") {
				continue
			}
			if cn != "" {
				if v, ok := row[cn]; ok {
					mappedCN := cn
					if m, ok := topColMap[cn]; ok {
						mappedCN = m
					}
					histWheres = append(histWheres, fmt.Sprintf("`%s` = '%v'", mappedCN, v))
				}
			}
		}
		histWhere := ""
		if len(histWheres) > 0 {
			histWhere = " WHERE " + strings.Join(histWheres, " AND ")
		}
		intervalSec := req.Interval
		if intervalSec <= 0 {
			intervalSec = 300
		}
		hSQL := fmt.Sprintf("SELECT toUnixTimestamp(toStartOfInterval(time, INTERVAL %d SECOND)) AS toi, %s FROM %s%s GROUP BY toi ORDER BY toi LIMIT 500",
			intervalSec, strings.Join(metricSelects, ", "), fullTable, histWhere)
		histRows, hErr := s.Query(qCtx, hSQL)
		if hErr == nil {
			if histData, hErr2 := ScanRows(histRows); hErr2 == nil {
				resultRow["HISTORY"] = fillNullHistory(convertHistory(histData, metricExprs), int64(req.Interval), req.TimeStart, req.TimeEnd, req.Fill, metricExprs)
			}
			histRows.Close()
		}
		if seenUID[uid] {
			continue
		}
		seenUID[uid] = true
		resultRows = append(resultRows, resultRow)
	}

	// Build pre_as map from SELECT expressions (same as List handler).
	preAsMap := map[string]string{}
	if len(req.Queries) > 0 && req.Queries[0].Select != "" {
		for _, item := range ParseSelectList(req.Queries[0].Select) {
			if item.Key != item.Expr {
				preAsMap[item.Key] = strings.ReplaceAll(item.Expr, "`", "")
			}
		}
	}

	// Build SCHEMAS from first data row.
	schemas := map[string]interface{}{}
	if len(resultRows) > 0 {
		for k, v := range resultRows[0] {
			vt, tp := "String", 0
			switch v.(type) {
			case float64:
				vt, tp = "Float64", 1
			case float32:
				vt, tp = "Float64", 1
			case int, int64, uint64:
				vt, tp = "UInt64", 1
			// icon_id is always a tag (even when value is int like -42).
			if strings.Contains(strings.ToLower(k), "icon_id") {
				tp = 0
				vt = "String"
			}
			}
			preAs := ""
			if p, ok := preAsMap[k]; ok {
				preAs = p
			}
			schemas[k] = map[string]interface{}{
				"label_type": "", "pre_as": preAs, "type": tp,
				"unit": "", "value_type": vt,
			}
		}
	}
	return &QueryTopResult{
		Data:   resultRows,
		Fields: schemas,
	}, nil
}

// ---------------------------------------------------------------------------
// QueryFlowLogDetail — returns flow log detail data from ClickHouse
// ---------------------------------------------------------------------------

func (s *CHService) QueryFlowLogDetail(ctx context.Context, bodyStr string) (*QueryFlowLogResult, error) {
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
	// flow_log-specific column mappings.
	isFlowLogDetail := db == "flow_log"
	if isFlowLogDetail {
		colMap["event_type"] = "l7_protocol"
		colMap["auto_instance"] = "if(empty(app_instance), toString(auto_instance_id_0), app_instance)"
		colMap["event_desc"] = "request_resource"
	}

	selectCols := "*"
	if len(req.Queries) > 0 && req.Queries[0].Select != "" {
		items := ParseSelectList(req.Queries[0].Select)
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

	rows, err := s.Query(qCtx, sql)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	rawData, err := ScanRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if rawData == nil {
		rawData = []map[string]interface{}{}
	}

	enumDisplay := map[string]map[string]string{
		"response_status":   {"0": "正常", "1": "异常", "2": "超时", "3": "服务端异常", "4": "客户端异常", "5": "取消"},
		"observation_point": {"c": "客户端网卡", "s": "服务端网卡", "c-p": "客户侧网络", "s-p": "服务侧网络", "c-app": "客户端应用", "s-app": "服务端应用", "app": "应用", "rest": "其他"},
		"l7_protocol":       {"20": "HTTP", "21": "Dubbo", "41": "gRPC", "60": "MySQL", "61": "PostgreSQL", "68": "Redis", "80": "DNS", "100": "TLS", "120": "FastCGI"},
		"is_tls":            {"0": "否", "1": "是"},
		"is_async":          {"0": "否", "1": "是"},
		"status":            {"0": "正常", "1": "异常", "2": "超时"},
		"protocol":          {"6": "TCP", "17": "UDP"},
		"close_type":        {"0": "TCP 连接超时", "1": "TCP 连接重置", "2": "TCP 服务端断开", "3": "TCP 客户端断开", "4": "TCP 服务端 fin", "5": "周期性上报"},
		"event_type": {"0": "读", "1": "写", "2": "创建", "3": "删除", "4": "修改权限", "5": "修改属性", "6": "修改名称", "7": "打开", "8": "关闭", "9": "读目录",
			"read": "读", "write": "写", "create": "创建", "delete": "删除"},
	}

	var processed []map[string]interface{}
	for _, row := range rawData {
		row["_querier_region"] = "本地"
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
			// Convert event_type to human-readable name.
			// For flow_log (l7_flow_log), event_type is an alias for l7_protocol.
			// For event (file_event), event_type has its own enum values (read/write â è¯»/å).
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

	return &QueryFlowLogResult{Data: processed}, nil
}

// endpointStat holds per-endpoint aggregated stats from Query B.
type endpointStat struct {
	total             float64
	bizCode           string
	responseCode      float64
	responseException string
	responseStatus    float64
}

// tmKey and tmAgg are used by QueryTraceMap for service-level aggregation.
type tmKey struct{ id, typ float64 }
type tmAgg struct {
	name           string
	parentIDs      map[tmKey]bool
	childIDs       map[tmKey]bool
	total          float64
	responseTotal  float64
	durationSum    float64
	successCount   float64
	serverErrCount float64
	signalSource   float64
	obsPoint       string
	ip             string
	serverEndpoints []string      // endpoints this service serves (as server, endpoints_1)
	clientEndpoints []string      // endpoints this service calls (as client, endpoints_0)
	gprocessIDs     map[string]interface{}
	epStats         map[string]endpointStat
}

// ---------------------------------------------------------------------------
// QueryTraceMap — returns TraceMap node data from ClickHouse
// ---------------------------------------------------------------------------

func (s *CHService) QueryTraceMap(ctx context.Context, timeStart, timeEnd int64, queryCondition string) (*QueryTraceMapResult, error) {
	qCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Build WHERE clause with time range, base filters, and query_condition.
	var whereClauses []string
	whereClauses = append(whereClauses, fmt.Sprintf("time >= %d", timeStart))
	whereClauses = append(whereClauses, fmt.Sprintf("time <= %d", timeEnd))
	if queryCondition != "" {
		whereClauses = append(whereClauses, queryCondition)
	}
	whereSQL := strings.Join(whereClauses, " AND ")

	// Query unique trace counts (total and calculated/completed).
	traceCountSQL := fmt.Sprintf(
		"SELECT uniq(trace_id) AS total_traces, uniqIf(trace_id, response_duration > 0) AS calc_traces FROM flow_log.l7_flow_log WHERE %s", whereSQL)
	var totalTraceCount, calcTraceCount int64
	if rows2, err2 := s.Query(qCtx, traceCountSQL); err2 == nil {
		if td, e2 := ScanRows(rows2); e2 == nil && len(td) > 0 {
			totalTraceCount = int64(GetF64(td[0], "total_traces"))
			calcTraceCount = int64(GetF64(td[0], "calc_traces"))
		}
		rows2.Close()
	}

	// Query service pairs with aggregated metrics AND detail columns.
	// We use any() for columns that are consistent per service pair (ip, observation_point)
	// and groupArray for multi-valued fields (endpoints, trace_ids).
	sqlStr := fmt.Sprintf(`
		SELECT
			auto_service_id_0, auto_service_type_0,
			auto_service_id_1, auto_service_type_1,
			dictGet('flow_tag.biz_service_map', 'name', toUInt64(auto_service_id_0)) AS auto_service_name_0,
			dictGet('flow_tag.biz_service_map', 'name', toUInt64(auto_service_id_1)) AS auto_service_name_1,
			any(signal_source) AS signal_source,
			any(observation_point) AS observation_point,
			any(ip4_0) AS ip4_0,
			any(ip4_1) AS ip4_1,
			any(request_resource) AS sample_endpoint,
			groupArray(DISTINCT request_resource) AS endpoints_arr,
			groupArray(DISTINCT trace_id) AS trace_ids_arr,
			groupArray(DISTINCT gprocess_id_0) AS gprocess_ids_0_arr,
			groupArray(DISTINCT gprocess_id_1) AS gprocess_ids_1_arr,
			any(biz_code) AS biz_code,
			any(response_exception) AS response_exception_val,
			Count(*) AS total,
			Count(response_duration) AS response_total,
			Sum(response_duration) AS response_duration_sum,
			CountIf(response_status = 0) AS response_success_count,
			CountIf(response_status = 3 OR response_status = 5) AS response_status_server_error_count,
			Avg(response_duration) AS avg_response_duration
		FROM flow_log.l7_flow_log
		WHERE %s
		GROUP BY
			auto_service_id_0, auto_service_type_0,
			auto_service_id_1, auto_service_type_1
		ORDER BY total DESC`, whereSQL)

	log.Printf("CH TraceMap: %s", sqlStr[:min(len(sqlStr), 300)])

	rows, err := s.Query(qCtx, sqlStr)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	allData, err := ScanRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if len(allData) == 0 {
		return &QueryTraceMapResult{}, nil
	}

	// -----------------------------------------------------------------------
	// Query B: Endpoint-level data (per service pair + request_resource).
	// Provides per-endpoint stats for endpoint_stats in edges and nodes.
	// -----------------------------------------------------------------------
	endpointSQL := fmt.Sprintf(`
		SELECT
			auto_service_id_0, auto_service_type_0,
			auto_service_id_1, auto_service_type_1,
			request_resource,
			Count(*) AS ep_total,
			any(biz_code) AS biz_code,
			any(response_code) AS response_code,
			any(response_exception) AS response_exception,
			any(response_status) AS response_status
		FROM flow_log.l7_flow_log
		WHERE %s
		GROUP BY
			auto_service_id_0, auto_service_type_0,
			auto_service_id_1, auto_service_type_1,
			request_resource
		ORDER BY ep_total DESC`, whereSQL)

	log.Printf("CH TraceMap endpoints: %s", endpointSQL[:min(len(endpointSQL), 300)])

	epRows, epErr := s.Query(qCtx, endpointSQL)
	var endpointData []map[string]interface{}
	if epErr == nil {
		endpointData, epErr = ScanRows(epRows)
		if epRows != nil {
			epRows.Close()
		}
	}

	// Build endpoint lookup: service_pair_key -> []endpointInfo
	type endpointInfo struct {
		name              string
		total             float64
		bizCode           string
		responseCode      float64
		responseException string
		responseStatus    float64
	}
	epByPair := map[string][]endpointInfo{}
	for _, epRow := range endpointData {
		key := fmt.Sprintf("%v|%v|%v|%v",
			GetF64(epRow, "auto_service_id_0"), GetF64(epRow, "auto_service_type_0"),
			GetF64(epRow, "auto_service_id_1"), GetF64(epRow, "auto_service_type_1"))
		epName := GetStr(epRow, "request_resource")
		if epName == "" {
			continue
		}
		epByPair[key] = append(epByPair[key], endpointInfo{
			name:              epName,
			total:             GetF64(epRow, "ep_total"),
			bizCode:           GetStr(epRow, "biz_code"),
			responseCode:      GetF64(epRow, "response_code"),
			responseException: GetStr(epRow, "response_exception"),
			responseStatus:    GetF64(epRow, "response_status"),
		})
	}

	// -----------------------------------------------------------------------
	// Step 1: Extract unique services and aggregate metrics per service.
	// -----------------------------------------------------------------------

	svcMap := map[tmKey]*tmAgg{}

	for _, row := range allData {
		sk0 := tmKey{GetF64(row, "auto_service_id_0"), GetF64(row, "auto_service_type_0")}
		sk1 := tmKey{GetF64(row, "auto_service_id_1"), GetF64(row, "auto_service_type_1")}

		if sk0 == sk1 {
			continue // self-loop: same service, no valid edge
		}
		total := GetF64(row, "total")
		rTotal := GetF64(row, "response_total")
		durSum := GetF64(row, "response_duration_sum")
		succ := GetF64(row, "response_success_count")
		errCnt := GetF64(row, "response_status_server_error_count")

		// Endpoints from this row (for the client service, these are endpoints_1 in parent info)
		endpoints := strList(GetArr(row, "endpoints_arr"))

		svcName0 := GetStr(row, "auto_service_name_0")
		if svcName0 == "" {
			svcName0 = GetStr(row, "ip4_0")
		}
		agg0 := getOrCreate(svcMap, sk0, svcName0)
		// Metrics: only tracked on server side (incoming edges). Client side tracks
		// childIDs for BFS leveling and endpoint/gprocess collection only.
		agg0.childIDs[sk1] = true
		agg0.signalSource = GetF64(row, "signal_source")
		agg0.obsPoint = GetStr(row, "observation_point")
		if agg0.ip == "" {
			agg0.ip = GetStr(row, "ip4_0")
		}
		// Collect gprocess IDs for client side (skip zero/empty IDs)
		for _, gpid := range strList(GetArr(row, "gprocess_ids_0_arr")) {
			if gpid != "" && gpid != "0" {
				agg0.gprocessIDs[gpid] = struct{}{}
			}
		}

		svcName1 := GetStr(row, "auto_service_name_1")
		if svcName1 == "" {
			svcName1 = GetStr(row, "ip4_1")
		}
		agg1 := getOrCreate(svcMap, sk1, svcName1)
		agg1.total += total
		agg1.responseTotal += rTotal
		agg1.durationSum += durSum
		agg1.successCount += succ
		agg1.serverErrCount += errCnt
		agg1.parentIDs[sk0] = true
		agg1.signalSource = GetF64(row, "signal_source")
		agg1.obsPoint = GetStr(row, "observation_point")
		if agg1.ip == "" {
			agg1.ip = GetStr(row, "ip4_1")
		}
		// Collect gprocess IDs for server side (skip zero/empty IDs)
		for _, gpid := range strList(GetArr(row, "gprocess_ids_1_arr")) {
			if gpid != "" && gpid != "0" {
				agg1.gprocessIDs[gpid] = struct{}{}
			}
		}
		// Collect endpoints for server side (endpoints this service serves)
		agg1.serverEndpoints = append(agg1.serverEndpoints, endpoints...)
		// Collect endpoints for client side (endpoints this service calls)
		agg0.clientEndpoints = append(agg0.clientEndpoints, endpoints...)

		// Collect per-endpoint stats from Query B data (for both client and server side)
		pairKey := fmt.Sprintf("%v|%v|%v|%v",
			GetF64(row, "auto_service_id_0"), GetF64(row, "auto_service_type_0"),
			GetF64(row, "auto_service_id_1"), GetF64(row, "auto_service_type_1"))
		for _, epInfo := range epByPair[pairKey] {
			if epInfo.name != "" {
				// Server side: per-endpoint stats for what this service serves
				if _, exists := agg1.epStats[epInfo.name]; !exists {
					agg1.epStats[epInfo.name] = endpointStat{
						total:             epInfo.total,
						bizCode:           epInfo.bizCode,
						responseCode:      epInfo.responseCode,
						responseException: epInfo.responseException,
						responseStatus:    epInfo.responseStatus,
					}
				}
				// Client side: per-endpoint stats for what this service calls
				if _, exists := agg0.epStats[epInfo.name]; !exists {
					agg0.epStats[epInfo.name] = endpointStat{
						total:             epInfo.total,
						bizCode:           epInfo.bizCode,
						responseCode:      epInfo.responseCode,
						responseException: epInfo.responseException,
						responseStatus:    epInfo.responseStatus,
					}
				}
			}
		}
	}

	// -----------------------------------------------------------------------
	// Step 2: Compute levels (BFS from root services that have no parent).
	// -----------------------------------------------------------------------
	levelOf := map[tmKey]int{}
	var roots []tmKey
	for sk, agg := range svcMap {
		hasRealParent := false
		for p := range agg.parentIDs {
			if p != sk {
				hasRealParent = true
				break
			}
		}
		if !hasRealParent {
			roots = append(roots, sk)
		}
	}
	queue := roots
	for _, sk := range roots {
		levelOf[sk] = 0
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		agg := svcMap[cur]
		for child := range agg.childIDs {
			if child == cur {
				continue
			}
			newLevel := levelOf[cur] + 1
			if existing, ok := levelOf[child]; !ok || newLevel > existing {
				levelOf[child] = newLevel
				queue = append(queue, child)
			}
		}
	}

	// -----------------------------------------------------------------------
	// Step 3: Build nodes with cloud-compatible format.
	// -----------------------------------------------------------------------
	nodeIdx := map[tmKey]int{}
	nodes := make([]map[string]interface{}, 0, len(svcMap))

	for sk, agg := range svcMap {
		nodeType := engine.NodeTypeFor(int(sk.typ))
		iconID := engine.IconFor(int(sk.typ))
		// service_uid: cloud format "auto_service_id=X,auto_service_type=Y[,app_service=,ip=Z]"
		serviceUID := fmt.Sprintf("auto_service_id=%v,auto_service_type=%v", sk.id, sk.typ)
		uidSuffix := serviceUID
		// For type 255 (IP-based services with no real name), cloud adds app_service=,ip=IP
		if sk.id == 0 && sk.typ == 255 && agg.ip != "" {
			serviceUID += fmt.Sprintf(",app_service=,ip=%s", agg.ip)
			uidSuffix = serviceUID
		}
		// uid: cloud includes self_index for internal linking
		uid := fmt.Sprintf("self_index=%d,%s", len(nodes), uidSuffix)
		total := agg.total
		durSum := agg.durationSum
		rTotal := agg.responseTotal

		avgDur := float64(0)
		if rTotal > 0 {
			avgDur = durSum / rTotal
		}
		successRatio := float64(0)
		if rTotal > 0 {
			successRatio = agg.successCount / rTotal
		}

		// Deduplicate server endpoints (endpoints this service serves → endpoints_1)
		seenServer := map[string]bool{}
		var uniqueServerEPs []string
		for _, ep := range agg.serverEndpoints {
			if ep != "" && !seenServer[ep] {
				seenServer[ep] = true
				uniqueServerEPs = append(uniqueServerEPs, ep)
			}
		}
		// Deduplicate client endpoints (endpoints this service calls → endpoints_0)
		seenClient := map[string]bool{}
		var uniqueClientEPs []string
		for _, ep := range agg.clientEndpoints {
			if ep != "" && !seenClient[ep] {
				seenClient[ep] = true
				uniqueClientEPs = append(uniqueClientEPs, ep)
			}
		}
		// Limit endpoints to top 5 by total count (matching cloud behavior).
		sort.Slice(uniqueServerEPs, func(i, j int) bool {
			ti := float64(0); if s, ok := agg.epStats[uniqueServerEPs[i]]; ok { ti = s.total }
			tj := float64(0); if s, ok := agg.epStats[uniqueServerEPs[j]]; ok { tj = s.total }
			return ti > tj
		})
		if len(uniqueServerEPs) > 5 { uniqueServerEPs = uniqueServerEPs[:5] }

		// Build endpoint_stats for server side (endpoints_1)
		var serverEndpointStats []interface{}
		for _, ep := range uniqueServerEPs {
			bizCode := ""
			respCode := float64(0)
			respExc := ""
			respStat := float64(0)
			var epTotal float64
			if stats, ok := agg.epStats[ep]; ok {
				bizCode = stats.bizCode
				respCode = stats.responseCode
				respExc = stats.responseException
				respStat = stats.responseStatus
				epTotal = stats.total
			}
			serverEndpointStats = append(serverEndpointStats, map[string]interface{}{
				"biz_response_code":    bizCode,
				"response_exception":   respExc,
				"response_code":        respCode,
				"total":                epTotal,
				"response_status":      respStat,
			})
		}
		if serverEndpointStats == nil {
			serverEndpointStats = []interface{}{}
		}
		// Limit endpoints to top 5 by total count (matching cloud behavior).
		sort.Slice(uniqueClientEPs, func(i, j int) bool {
			ti := float64(0); if s, ok := agg.epStats[uniqueClientEPs[i]]; ok { ti = s.total }
			tj := float64(0); if s, ok := agg.epStats[uniqueClientEPs[j]]; ok { tj = s.total }
			return ti > tj
		})
		if len(uniqueClientEPs) > 5 { uniqueClientEPs = uniqueClientEPs[:5] }

		// Build endpoint_stats for client side (endpoints_0)
		var clientEndpointStats []interface{}
		for _, ep := range uniqueClientEPs {
			bizCode := ""
			respCode := float64(0)
			respExc := ""
			respStat := float64(0)
			var epTotal float64
			if stats, ok := agg.epStats[ep]; ok {
				bizCode = stats.bizCode
				respCode = stats.responseCode
				respExc = stats.responseException
				respStat = stats.responseStatus
				epTotal = stats.total
			}
			clientEndpointStats = append(clientEndpointStats, map[string]interface{}{
				"biz_response_code":    bizCode,
				"response_exception":   respExc,
				"response_code":        respCode,
				"total":                epTotal,
				"response_status":      respStat,
			})
		}
		if clientEndpointStats == nil {
			clientEndpointStats = []interface{}{}
		}
		var serverEPsList []interface{}
		for _, ep := range uniqueServerEPs {
			serverEPsList = append(serverEPsList, ep)
		}
		var clientEPsList []interface{}
		for _, ep := range uniqueClientEPs {
			clientEPsList = append(clientEPsList, ep)
		}

		// Build trace_ids map (cloud format: {"trace_id": {}, "trace_id": {}})
		traceIDsMap := map[string]interface{}{}
		// From the edge data, we collect trace IDs per node
		// (populated from parent_node_infos in step 4)

		node := map[string]interface{}{
			"level":                     levelOf[sk],
			"signal_source":             agg.signalSource,
			"response_code":             float64(0),
			"response_status":           float64(0),
			"response_exception":        "",
			"biz_response_code":         "",
			"auto_service_type":         sk.typ,
			"auto_service_id":           sk.id,
			"icon_id":                   iconID,
			"ip":                        agg.ip,
			"uid":                       uid,
			"node_type":                 nodeType,
			"app_service":               agg.name,
			"service_uid":               serviceUID,
			"auto_service":              agg.name,
			// (auto_service kept as-is; IP fallback is done in cloud but requires IP per edge)
			"_querier_region":           "本地",
			"observation_point":         agg.obsPoint,
			"parent_node_infos":         []interface{}{},

			// Endpoints at node level
			"endpoints_0":               clientEPsList,
			"endpoints_1":               serverEPsList,
			"endpoint_stats_0":          clientEndpointStats,
			"endpoint_stats_1":          serverEndpointStats,

			// Trace IDs as dict (cloud format)
			"trace_ids":                 traceIDsMap,
			"abnormal_trace_ids":        map[string]interface{}{},
			"gprocess_ids":              agg.gprocessIDs,

			// Aggregated metrics
			"total":                              total,
			"response_total":                     rTotal,
			"response_duration_sum":              durSum,
			"response_success_count":             agg.successCount,
			"response_status_server_error_count": agg.serverErrCount,
			"sum_request":                        total,
			"avg_response_duration":              avgDur,
			"avg_success_ratio":                  successRatio,
			"avg_response_ratio":                 float64(1.0),
			"inferred_application_type":          nil,
		}
		// Null-conversion: empty maps/lists → nil to match cloud format
		if m, ok := node["gprocess_ids"].(map[string]interface{}); ok && len(m) == 0 {
			node["gprocess_ids"] = nil
		}
		if m, ok := node["trace_ids"].(map[string]interface{}); ok && len(m) == 0 {
			node["trace_ids"] = nil
		}
		if m, ok := node["abnormal_trace_ids"].(map[string]interface{}); ok && len(m) == 0 {
			node["abnormal_trace_ids"] = nil
		}
		// endpoints_0 / endpoint_stats_0 null when no client endpoints
		if arr, ok := node["endpoints_0"].([]interface{}); ok && len(arr) == 0 {
			node["endpoints_0"] = nil
		}
		if arr, ok := node["endpoint_stats_0"].([]interface{}); ok && len(arr) == 0 {
			node["endpoint_stats_0"] = nil
		}

		nodeIdx[sk] = len(nodes)
		nodes = append(nodes, node)
	}

	// -----------------------------------------------------------------------
	// Step 4: Build parent_node_infos edges (cloud-compatible format).
	// -----------------------------------------------------------------------
for _, row := range allData {
		sk0 := tmKey{GetF64(row, "auto_service_id_0"), GetF64(row, "auto_service_type_0")}
		sk1 := tmKey{GetF64(row, "auto_service_id_1"), GetF64(row, "auto_service_type_1")}
		if sk0 == sk1 {
			continue
		}
		idx1, ok := nodeIdx[sk1]
		if !ok {
			continue
		}
		par := nodes[idx1]["parent_node_infos"].([]interface{})

		if sk0 == sk1 {
			continue // self-loop: same service, no valid edge
		}
		total := GetF64(row, "total")
		rTotal := GetF64(row, "response_total")
		durSum := GetF64(row, "response_duration_sum")
		succ := GetF64(row, "response_success_count")
		errCnt := GetF64(row, "response_status_server_error_count")

		avgDur := float64(0)
		if rTotal > 0 {
			avgDur = durSum / rTotal
		}
		successRatio := float64(0)
		if rTotal > 0 {
			successRatio = succ / rTotal
		}

		// Endpoints from this service pair (from Query B endpoint data)
		pairKey := fmt.Sprintf("%v|%v|%v|%v",
			GetF64(row, "auto_service_id_0"), GetF64(row, "auto_service_type_0"),
			GetF64(row, "auto_service_id_1"), GetF64(row, "auto_service_type_1"))
		epInfos := epByPair[pairKey]
		var endpointsList []interface{}
		var endpointStats []interface{}
		for _, ep := range epInfos {
			if ep.name == "" {
				continue
			}
			endpointsList = append(endpointsList, ep.name)
			endpointStats = append(endpointStats, map[string]interface{}{
				"biz_response_code":  ep.bizCode,
				"response_exception": ep.responseException,
				"response_code":      ep.responseCode,
				"total":              ep.total,
				"response_status":    ep.responseStatus,
			})
		}
		if endpointStats == nil {
			endpointStats = []interface{}{}
		}

		// Trace IDs for this edge (from groupArray)
		traceIDStrs := strList(GetArr(row, "trace_ids_arr"))
		var abnormalTraceIDs []string
		if errCnt > 0 {
			for _, tid := range traceIDStrs {
				if tid != "" {
					abnormalTraceIDs = append(abnormalTraceIDs, tid)
				}
			}
		}
		// Build lists for edge-level (uniq_parent_span_infos uses lists)
		traceIDsList := []interface{}{}
		for _, tid := range traceIDStrs {
			if tid != "" {
				traceIDsList = append(traceIDsList, tid)
			}
		}
		abnormalIDsList := []interface{}{}
		for _, tid := range abnormalTraceIDs {
			if tid != "" {
				abnormalIDsList = append(abnormalIDsList, tid)
			}
		}

		// Add trace IDs to the server node's trace_ids (node level uses dict)
		for _, tid := range traceIDsList {
			if existing, ok := nodes[idx1]["trace_ids"].(map[string]interface{}); ok {
				existing[tid.(string)] = map[string]interface{}{}
			}
		}
		// Add abnormal trace IDs to the server node (node level uses dict)
		for _, tid := range abnormalIDsList {
			if existing, ok := nodes[idx1]["abnormal_trace_ids"].(map[string]interface{}); ok {
				existing[tid.(string)] = float64(1)
			}
		}

		// Build observation_point display (use sample from data or empty)
		obsPt := GetStr(row, "observation_point")

		// Null-conversion: empty edge lists → nil (matches cloud format)
		if len(traceIDsList) == 0 {
			traceIDsList = nil
		}
		if len(abnormalIDsList) == 0 {
			abnormalIDsList = nil
		}

		parentInfo := map[string]interface{}{
			"pseudo_link":                        0,
			"parent_index":                       nodeIdx[sk0],
			"total":                              total,
			"response_total":                     rTotal,
			"response_duration_sum":              durSum,
			"response_status_server_error_count": errCnt,
			"response_success_count":             succ,
			"sum_request":                        total,
			"avg_response_duration":              avgDur,
			"avg_response_ratio":                 float64(1.0),
			"avg_success_ratio":                  successRatio,
			"uniq_parent_span_infos": []interface{}{
				map[string]interface{}{
					"signal_source":              GetF64(row, "signal_source"),
					"auto_service_type_0":        GetF64(row, "auto_service_type_0"),
					"auto_service_type_1":        GetF64(row, "auto_service_type_1"),
					"auto_service_id_0":          GetF64(row, "auto_service_id_0"),
					"auto_service_id_1":          GetF64(row, "auto_service_id_1"),
					"client_icon_id":             engine.IconFor(int(GetF64(row, "auto_service_type_0"))),
					"server_icon_id":             engine.IconFor(int(GetF64(row, "auto_service_type_1"))),
					"observation_point":          obsPt,
					"ip_0":                       GetStr(row, "ip4_0"),
					"ip_1":                       GetStr(row, "ip4_1"),
					"app_service_0":              GetStr(row, "auto_service_name_0"),
					"app_service_1":              GetStr(row, "auto_service_name_1"),
					"auto_service_0":             GetStr(row, "auto_service_name_0"),
					"auto_service_1":             GetStr(row, "auto_service_name_1"),
					"client_node_type":           engine.NodeTypeFor(int(GetF64(row, "auto_service_type_0"))),
					"server_node_type":           engine.NodeTypeFor(int(GetF64(row, "auto_service_type_1"))),
					"_querier_region":            "本地",
					"endpoints":                  endpointsList,
					"endpoint_stats":             endpointStats,
					"trace_ids":                  traceIDsList,
					"abnormal_trace_ids":         abnormalIDsList,
					"inferred_application_type_0": nil,
					"inferred_application_type_1": nil,
				},
			},
		}
		nodes[idx1]["parent_node_infos"] = append(par, parentInfo)
	}

return &QueryTraceMapResult{
		Data:             nodes,
		TotalTraces:      int(totalTraceCount),
		CalculatedTraces: int(calcTraceCount),
	}, nil
}

// strList converts []interface{} of strings to []string for easier processing.


func strList(arr []interface{}) []string {
	if arr == nil {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			result = append(result, s)
		} else {
			result = append(result, fmt.Sprintf("%v", v))
		}
	}
	return result
}



// getOrCreate returns an existing serviceAgg or creates a new one.
func getOrCreate(m map[tmKey]*tmAgg, key tmKey, name string) *tmAgg {
	if a, ok := m[key]; ok {
		if name != "" && a.name == "" {
			a.name = name
		}
		return a
	}
	a := &tmAgg{
		name:          name,
		parentIDs:     map[tmKey]bool{},
		childIDs:      map[tmKey]bool{},
		gprocessIDs:    map[string]interface{}{},
		epStats:        map[string]endpointStat{},
	}
	m[key] = a
	return a
}

// convertHistory transforms history query data for Top result rows.
func convertHistory(hist []map[string]interface{}, metrics []metricExpr) []map[string]interface{} {
	var result []map[string]interface{}
	for _, row := range hist {
		entry := map[string]interface{}{}
		if toi, ok := row["toi"]; ok {
			entry["toi"] = toi
		}
		for _, m := range metrics {
			if v, ok := row[m.key]; ok {
				entry[m.key] = v
			}
		}
		result = append(result, entry)
	}
	return result
}

func quoteCH(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}


// fillNullHistory fills gaps in time-series history data with null entries.
func fillNullHistory(hist []map[string]interface{}, interval, timeStart, timeEnd int64, fill string, metrics []metricExpr) []map[string]interface{} {
	if len(hist) == 0 || interval <= 0 || timeStart >= timeEnd {
		return hist
	}
	// Build lookup map: timestamp → row.
	histMap := map[int64]map[string]interface{}{}
	for _, h := range hist {
		if toi, ok := h["toi"].(float64); ok {
			histMap[int64(toi)] = h
		}
	}
	// Align start/end to interval boundaries.
	start := (timeStart / interval) * interval
	end := (timeEnd / interval) * interval
	var result []map[string]interface{}
	for t := start; t <= end; t += interval {
		if row, ok := histMap[t]; ok {
			result = append(result, row)
		} else {
			entry := map[string]interface{}{"toi": float64(t)}
			for _, m := range metrics {
				if fill == "0" {
					entry[m.key] = float64(0)
				} else if fill == "none" {
					// Skip — don't create entry for missing data
					continue
				} else {
					// "null" or empty — use nil
					entry[m.key] = nil
				}
			}
			result = append(result, entry)
		}
	}
	return result
}
