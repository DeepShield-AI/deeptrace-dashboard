package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
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

	return &QueryListResult{
		Data:   data,
		Fields: schemas,
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
			// Column alias: rrt -> rrt_sum/greatest(rrt_count,1) for tables without physical rrt column
			if !strings.Contains(sqlExpr, "rrt_sum") && !strings.Contains(sqlExpr, "rrt_count") {
				sqlExpr = strings.ReplaceAll(sqlExpr, "rrt", "rrt_sum / greatest(rrt_count, 1)")
			}
			if !strings.Contains(sqlExpr, "rtt_sum") && !strings.Contains(sqlExpr, "rtt_count") {
				sqlExpr = strings.ReplaceAll(sqlExpr, "rtt", "rtt_sum / greatest(rtt_count, 1)")
			}
			if isFlowLog {
				cleanExpr := strings.ToLower(strings.ReplaceAll(item.Expr, "`", ""))
				existsInFlowLog := strings.Contains(cleanExpr, "response_duration") ||
					strings.Contains(cleanExpr, "response_code") ||
					strings.Contains(cleanExpr, "response_status") ||
					strings.Contains(cleanExpr, "request_type") ||
					strings.Contains(cleanExpr, "request_domain") ||
					strings.Contains(cleanExpr, "request_resource") ||
					strings.Contains(cleanExpr, "response_exception") ||
					strings.Contains(cleanExpr, "response_result")
				if !existsInFlowLog {
					sqlExpr = "count(*)"
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
		"auto_service_0":  "auto_service_id_0",
		"auto_service_1":  "auto_service_id_1",
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

		mappedCol := colName
		if m, ok := topColMap[colName]; ok {
			mappedCol = m
		} else if m2, ok2 := flowLogColMap[colName]; ok2 {
			mappedCol = m2
		}
		tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mappedCol, colName))
		groupCols = append(groupCols, fmt.Sprintf("`%s`", mappedCol))
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
			}
		}
	}

	if len(data) == 0 {
		return &QueryTopResult{Data: []map[string]interface{}{}}, nil
	}

	var resultRows []map[string]interface{}
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
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(cn[idx+4:])
				cn = strings.Trim(cn, "`")
			}
			if cn != "" {
				if v, ok := row[cn]; ok {
					resultRow[cn] = v
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
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(cn[idx+4:])
				cn = strings.Trim(cn, "`")
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
		hSQL := fmt.Sprintf("SELECT toUnixTimestamp(toStartOfInterval(time, INTERVAL 1 HOUR)) AS toi, %s FROM %s%s GROUP BY toi ORDER BY toi LIMIT 500",
			strings.Join(metricSelects, ", "), fullTable, histWhere)
		histRows, hErr := s.Query(qCtx, hSQL)
		if hErr == nil {
			if histData, hErr2 := ScanRows(histRows); hErr2 == nil {
				resultRow["HISTORY"] = convertHistory(histData, metricExprs)
			}
			histRows.Close()
		}
		resultRows = append(resultRows, resultRow)
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
			}
			schemas[k] = map[string]interface{}{
				"label_type": "", "pre_as": "", "type": tp,
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

// serviceKey and serviceAgg are used by QueryTraceMap for service-level aggregation.
type serviceKey struct{ id, typ float64 }
type serviceAgg struct {
	name           string
	parentIDs      map[serviceKey]bool
	childIDs       map[serviceKey]bool
	total          float64
	responseTotal  float64
	durationSum    float64
	successCount   float64
	serverErrCount float64
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
		// query_condition contains backtick-quoted column filters like:
		// `l7_protocol`!=20 AND `response_status`!=4
		// Strip backticks for ClickHouse compatibility or keep them if they work.
		whereClauses = append(whereClauses, queryCondition)
	}
	whereSQL := strings.Join(whereClauses, " AND ")

	// Query service pairs with aggregated metrics.
	// Query COUNT for unique traces first (total and calculated).
	var traceCountSQL string
	if queryCondition != "" {
		traceCountSQL = fmt.Sprintf(
			"SELECT uniq(trace_id) AS total_traces, uniqIf(trace_id, response_duration > 0) AS calc_traces FROM flow_log.l7_flow_log WHERE %s", whereSQL)
	} else {
		traceCountSQL = fmt.Sprintf(
			"SELECT uniq(trace_id) AS total_traces, uniqIf(trace_id, response_duration > 0) AS calc_traces FROM flow_log.l7_flow_log WHERE %s", whereSQL)
	}
	var totalTraceCount, calcTraceCount int64
	if rows2, err2 := s.Query(qCtx, traceCountSQL); err2 == nil {
		if td, e2 := ScanRows(rows2); e2 == nil && len(td) > 0 {
			totalTraceCount = int64(GetF64(td[0], "total_traces"))
			calcTraceCount = int64(GetF64(td[0], "calc_traces"))
		}
		rows2.Close()
	}

	// Query service pairs with aggregated metrics.
	sqlStr := fmt.Sprintf(`
		SELECT
			auto_service_id_0, auto_service_type_0,
			auto_service_id_1, auto_service_type_1,
			dictGet('flow_tag.biz_service_map', 'name', toUInt64(auto_service_id_0)) AS auto_service_name_0,
			dictGet('flow_tag.biz_service_map', 'name', toUInt64(auto_service_id_1)) AS auto_service_name_1,
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
	// Step 1: Extract unique services and aggregate metrics per service.
	// -----------------------------------------------------------------------

	svcMap := map[serviceKey]*serviceAgg{}

	for _, row := range allData {
		// Client service (_0)
		sk0 := serviceKey{GetF64(row, "auto_service_id_0"), GetF64(row, "auto_service_type_0")}
		// Server service (_1)
		sk1 := serviceKey{GetF64(row, "auto_service_id_1"), GetF64(row, "auto_service_type_1")}

		total := GetF64(row, "total")
		rTotal := GetF64(row, "response_total")
		durSum := GetF64(row, "response_duration_sum")
		succ := GetF64(row, "response_success_count")
		errCnt := GetF64(row, "response_status_server_error_count")
		agg0 := getOrCreate(svcMap, sk0, GetStr(row, "auto_service_name_0"))
		agg0.total += total
		agg0.responseTotal += rTotal
		agg0.durationSum += durSum
		agg0.successCount += succ
		agg0.serverErrCount += errCnt
		agg0.childIDs[sk1] = true

		agg1 := getOrCreate(svcMap, sk1, GetStr(row, "auto_service_name_1"))
		agg1.total += total
		agg1.responseTotal += rTotal
		agg1.durationSum += durSum
		agg1.successCount += succ
		agg1.serverErrCount += errCnt
		agg1.parentIDs[sk0] = true
	}

	// -----------------------------------------------------------------------
	// Step 2: Compute levels (BFS from root services that have no parent).
	// -----------------------------------------------------------------------
	levelOf := map[serviceKey]int{}
	// Find roots: services with no parents (or self-only parents).
	var roots []serviceKey
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
	// BFS from roots
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
	// Step 3: Build nodes.
	// -----------------------------------------------------------------------
	// Map from serviceKey → node index for parent references.
	nodeIdx := map[serviceKey]int{}
	nodes := make([]map[string]interface{}, 0, len(svcMap))

	for sk, agg := range svcMap {
		nodeType := serviceTypeToNodeType(sk.typ)
		iconID := serviceTypeToIconID(sk.typ)
		uid := fmt.Sprintf("self_index=%d,auto_service_id=%v,auto_service_type=%v", len(nodes), sk.id, sk.typ)
		total := agg.total
		durSum := agg.durationSum
		rTotal := agg.responseTotal

		// Compute derived metrics
		avgDur := float64(0)
		if rTotal > 0 {
			avgDur = durSum / rTotal
		}
		successRatio := float64(0)
		if rTotal > 0 {
			successRatio = agg.successCount / rTotal
		}

		node := map[string]interface{}{
			"level":                     levelOf[sk],
			"auto_service_type":         sk.typ,
			"auto_service_id":           sk.id,
			"auto_service":              agg.name,
			"app_service":               agg.name,
			"node_type":                 nodeType,
			"icon_id":                   iconID,
			"uid":                       uid,
			"service_uid":               uid,
			"response_code":             float64(0),
			"response_status":           float64(0),
			"response_exception":        "",
			"biz_response_code":         "",
			"ip":                        "",
			"observation_point":         "",
			"signal_source":             float64(4),
			"inferred_application_type": "",

			// Aggregated metrics
			"total":                              total,
			"response_total":                     rTotal,
			"response_duration_sum":              durSum,
			"response_success_count":             agg.successCount,
			"response_status_server_error_count": agg.serverErrCount,
			"avg_response_duration":              avgDur,
			"avg_success_ratio":                  successRatio,
			"avg_response_ratio":                 float64(1),

			// These fields are present in cloud response
			"sum_request":        float64(0),
			"endpoints_0":        []interface{}{},
			"endpoints_1":        []interface{}{},
			"endpoint_stats_0":   []interface{}{},
			"endpoint_stats_1":   []interface{}{},
			"trace_ids":          []interface{}{},
			"abnormal_trace_ids": []interface{}{},
			"gprocess_ids":       []interface{}{},

			// Parent info (populated below)
			"parent_node_infos": []interface{}{},
		}
		nodeIdx[sk] = len(nodes)
		nodes = append(nodes, node)
	}

	// -----------------------------------------------------------------------
	// Step 4: Build parent_node_infos edges.
	// -----------------------------------------------------------------------
	for _, row := range allData {
		sk0 := serviceKey{GetF64(row, "auto_service_id_0"), GetF64(row, "auto_service_type_0")}
		sk1 := serviceKey{GetF64(row, "auto_service_id_1"), GetF64(row, "auto_service_type_1")}
		if sk0 == sk1 {
			continue
		}
		idx1, ok := nodeIdx[sk1]
		if !ok {
			continue
		}
		par := nodes[idx1]["parent_node_infos"].([]interface{})

		total := GetF64(row, "total")
		rTotal := GetF64(row, "response_total")
		durSum := GetF64(row, "response_duration_sum")
		succ := GetF64(row, "response_success_count")
		errCnt := GetF64(row, "response_status_server_error_count")

		parentInfo := map[string]interface{}{
			"pseudo_link":                        0,
			"parent_index":                       nodeIdx[sk0],
			"total":                              total,
			"response_total":                     rTotal,
			"response_duration_sum":              durSum,
			"response_status_server_error_count": errCnt,
			"response_success_count":             succ,
			"uniq_parent_span_infos": []interface{}{
				map[string]interface{}{
					"signal_source":       GetF64(row, "signal_source"),
					"auto_service_type_0": GetF64(row, "auto_service_type_0"),
					"auto_service_type_1": GetF64(row, "auto_service_type_1"),
					"auto_service_id_0":   GetF64(row, "auto_service_id_0"),
					"auto_service_id_1":   GetF64(row, "auto_service_id_1"),
					"client_icon_id":      serviceTypeToIconID(GetF64(row, "auto_service_type_0")),
					"server_icon_id":      serviceTypeToIconID(GetF64(row, "auto_service_type_1")),
					"observation_point":   "",
					"ip_0":                "",
					"ip_1":                "",
					"app_service_0":       GetStr(row, "auto_service_name_0"),
					"app_service_1":       GetStr(row, "auto_service_name_1"),
					"auto_service_0":      GetStr(row, "auto_service_name_0"),
					"auto_service_1":      GetStr(row, "auto_service_name_1"),
					"client_node_type":    serviceTypeToNodeType(GetF64(row, "auto_service_type_0")),
					"server_node_type":    serviceTypeToNodeType(GetF64(row, "auto_service_type_1")),
					"endpoints":           []interface{}{},
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

// serviceTypeToNodeType maps auto_service_type to node_type string.
func serviceTypeToNodeType(typ float64) string {
	switch int(typ) {
	case 1:
		return "vm"
	case 10:
		return "pod"
	case 11:
		return "pod_service"
	case 15:
		return "rds_instance"
	case 103:
		return "biz_service"
	case 104:
		return "biz_service_group"
	case 105:
		return "alb"
	case 255:
		return "other"
	default:
		return "biz_service"
	}
}

// serviceTypeToIconID maps auto_service_type to icon_id.
func serviceTypeToIconID(typ float64) float64 {
	switch int(typ) {
	case 1:
		return -19 // VM
	case 10:
		return -14 // Pod
	case 11:
		return -16 // Pod Service
	case 15:
		return -36 // RDS
	case 103:
		return -45 // Business Service
	case 104:
		return -46 // Business Service Group
	case 105:
		return -47 // ALB
	default:
		return -45 // default biz_service icon
	}
}

// getOrCreate returns an existing serviceAgg or creates a new one.
func getOrCreate(m map[serviceKey]*serviceAgg, key serviceKey, name string) *serviceAgg {
	if a, ok := m[key]; ok {
		if name != "" && a.name == "" {
			a.name = name
		}
		return a
	}
	a := &serviceAgg{
		name:          name,
		parentIDs:     map[serviceKey]bool{},
		childIDs:      map[serviceKey]bool{},
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
