package fastlist

import (
	"deeptrace-backend/query"
	"deeptrace-backend/query/flowlog"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/query/showmetrics"
	"deeptrace-backend/query/showtagvalues"
)


type Deps struct {
	CH       *clickhouse.CHService
	Cache    interface{}
	Zerotrace *client.ZerotraceService
}
type FastListRequest struct {
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

type FastListDebugInfo struct {
	requestBody []byte
	db          string
	table       string
	selStr      string
	sel         string // full SELECT clause including Count(row)
	extras      []string
	limit       int
	offset      int
	sql         string // final SQL sent to ZT
	queryStart  time.Time
	queryEnd    time.Time
	result      *client.QueryResult
	err         error
}


// AND/OR condition tree (sent by the frontend in QuerierJs format).
func FlattenFastListConditions(conds []interface{}, db string) []string {
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
		if _, skip := clickhouse.FastListSkipCols[col]; skip {
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
				result = append(result, FlattenFastListConditions(children, db)...)
			}
		}
	}
	return result
}

// Column info for a tag or metric in the QuerierJs intermediate response.
type QuerierColumn struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	IsResource  bool   `json:"isResource"`
	Type        int    `json:"type,omitempty"`
	Unit        string `json:"unit,omitempty"`
}

// E.g., Enum(observation_point) → observation_point, node_type(x) → x.
func NormalizeFastListSelect(sel string) string {
	result := sel
	for _, fn := range []string{"Enum", "node_type", "icon_id", "newTag"} {
		lower := strings.ToLower(result)
		idx := strings.Index(lower, strings.ToLower(fn)+"(")
		for idx >= 0 {
			end := idx + len(fn) + 1
			depth := 1
			for end < len(result) && depth > 0 {
				if result[end] == '(' { depth++ }
				if result[end] == ')' { depth-- }
				end++
			}
			inner := strings.TrimSpace(result[idx+len(fn)+1 : end-1])
			result = result[:idx] + inner + result[end:]
			lower = strings.ToLower(result)
			idx = strings.Index(lower, strings.ToLower(fn)+"(")
		}
	}
	return result
}


func ChQueryFastList(ch *clickhouse.CHService, db, tbl, selStr string, extras []string,
	timeStart, timeEnd int64, limit, offset int) []interface{} {
	if ch == nil || !ch.Enabled() {
		return nil
	}
	// Build a ClickHouse-compatible SQL.
	chSel := NormalizeFastListSelect(selStr)
	// Deduplicate after stripping DSL functions (Enum(x) and x both become x).
	seenSel := map[string]bool{}
	var dedupParts []string
	for _, p := range strings.Split(chSel, ",") {
		p = strings.TrimSpace(p)
		if !seenSel[p] {
			seenSel[p] = true
			dedupParts = append(dedupParts, p)
		}
	}
	chSel = strings.Join(dedupParts, ", ")
	// Map virtual tag names to real ClickHouse columns (matching ZT behavior).
	chSelParts := strings.Split(chSel, ",")
	for i, p := range chSelParts {
		p = strings.TrimSpace(p)
		// Virtual tag → real ID column mapping (same as topColMap/flowLogColMap for flow_log).
		switch p {
		case "pod_node_1": chSelParts[i] = "pod_node_id_1"
		case "pod_node_0": chSelParts[i] = "pod_node_id_0"
		case "pod_ns_1": chSelParts[i] = "pod_ns_id_1"
		case "pod_ns_0": chSelParts[i] = "pod_ns_id_0"
		case "pod_cluster_1": chSelParts[i] = "pod_cluster_id_1"
		case "pod_cluster_0": chSelParts[i] = "pod_cluster_id_0"
		case "pod_service_1": chSelParts[i] = "pod_service_id_1"
		case "pod_service_0": chSelParts[i] = "pod_service_id_0"
		case "pod_group_1": chSelParts[i] = "pod_group_id_1"
		case "pod_group_0": chSelParts[i] = "pod_group_id_0"
		case "pod_1": chSelParts[i] = "pod_id_1"
		case "pod_0": chSelParts[i] = "pod_id_0"
		case "region_1": chSelParts[i] = "region_id_1"
		case "region_0": chSelParts[i] = "region_id_0"
		case "az_1": chSelParts[i] = "az_id_1"
		case "az_0": chSelParts[i] = "az_id_0"
		case "chost_1": chSelParts[i] = "l3_device_id_1"
		case "chost_0": chSelParts[i] = "l3_device_id_0"
		case "vpc_1": chSelParts[i] = "epc_id_1"
		case "vpc_0": chSelParts[i] = "epc_id_0"
		case "subnet_1": chSelParts[i] = "subnet_id_1"
		case "subnet_0": chSelParts[i] = "subnet_id_0"
		case "router_1": chSelParts[i] = "router_id_1"
		case "router_0": chSelParts[i] = "router_id_0"
		case "lb_1": chSelParts[i] = "lb_id_1"
		case "lb_0": chSelParts[i] = "lb_id_0"
		case "gprocess_1": chSelParts[i] = "gprocess_id_1"
		case "gprocess_0": chSelParts[i] = "gprocess_id_0"
		case "service_1": chSelParts[i] = "service_id_1"
		case "service_0": chSelParts[i] = "service_id_0"
		}
	}
	chSel = strings.Join(chSelParts, ", ")
	sel := fmt.Sprintf("%s, count(*) AS count_row", chSel)
	groupBy := chSel
	// Build SQL with `db`.`table` prefix for ClickHouse.
	fullTable := fmt.Sprintf("`%s`.`%s`", db, tbl)
	var clauses []string
	if timeStart > 0 { clauses = append(clauses, fmt.Sprintf("time >= %d", timeStart)) }
	if timeEnd > 0 { clauses = append(clauses, fmt.Sprintf("time <= %d", timeEnd)) }
	// Strip ZT-only virtual columns not present in CH (e.g., role).
	var chExtras []string
	for _, e := range extras {
		skip := false
		for _, vcol := range []string{"role"} {
			if strings.Contains(e, "`"+vcol+"`") { skip = true; break }
		}
		if !skip { chExtras = append(chExtras, e) }
	}
	clauses = append(clauses, chExtras...)
	whereClause := ""
	if len(clauses) > 0 { whereClause = " WHERE " + strings.Join(clauses, " AND ") }
	sql := fmt.Sprintf("SELECT %s FROM %s%s GROUP BY %s ORDER BY `count_row` DESC LIMIT %d", sel, fullTable, whereClause, groupBy, limit)
	if offset > 0 { sql += fmt.Sprintf(" OFFSET %d", offset) }
	log.Printf("🔍 CH fast_list fallback: db=%s sql=%s", db, sql)

	rows, err := showmetrics.HTTPQuery(sql)
	if err != nil {
		log.Printf("⚠️  CH fast_list error: %v", err)
		return nil
	}

	// Detect Enum(column) patterns in original SELECT for post-translation.
	var enumCols []string
	enumRe := regexp.MustCompile(`Enum\(([^)]+)\)`)
	for _, match := range enumRe.FindAllStringSubmatch(selStr, -1) {
		enumCols = append(enumCols, strings.TrimSpace(match[1]))
	}

	// Load int_enum_map for translation.
	enumCache := map[string]map[string]string{}
	for _, col := range enumCols {
		eRes, eErr := showmetrics.HTTPQuery(fmt.Sprintf("SELECT toString(value), name_zh FROM flow_tag.int_enum_map WHERE tag_name='%s'", col))
		m := map[string]string{}
		if eErr == nil {
			for _, er := range eRes {
				k := fmt.Sprintf("%v", er["toString(value)"])
				v := showtagvalues.GetSVStr(er, "name_zh")
				m[k] = v
			}
		}
		if len(m) == 0 {
			if fb, ok := showtagvalues.BuiltinEnumFallback[col]; ok {
				m = fb
			}
		}
		enumCache[col] = m
	}

	result := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		r := make(map[string]interface{})
		for k, v := range row {
			r[k] = v
		}
		// Add Enum(column) display name columns.
		for _, ec := range enumCols {
			enumKey := "Enum(" + ec + ")"
			if _, exists := r[enumKey]; !exists {
				if raw, ok := r[ec]; ok {
					rawStr := fmt.Sprintf("%v", raw)
					if m, ok := enumCache[ec]; ok {
						if display, ok2 := m[rawStr]; ok2 {
							r[enumKey] = display
						} else if f, err := strconv.ParseFloat(rawStr, 64); err == nil {
							if display, ok2 := m[fmt.Sprintf("%.0f", f)]; ok2 {
								r[enumKey] = display
							} else {
								r[enumKey] = raw
							}
						} else {
							r[enumKey] = raw
						}
					} else {
						r[enumKey] = raw
					}
				}
			}
		}
		r["_querier_region"] = "本地"
		result = append(result, r)
	}
	return result
}

// buildFastListDebug constructs the 4-entry _debug array matching the cloud's internal pipeline:
//
//	[0] QuerierJs发送请求 — frontend request as seen by the querier middleware
//	[1] QuerierJs收到响应 — querier middleware's SQL generation output
//	[2] Statistics发送请求 — request sent to the Statistics service
func QueryFastList(ch *clickhouse.CHService, zt *client.ZerotraceService, selStr string, body []byte, debug bool) ([]interface{}, *FastListDebugInfo) {
	var req FastListRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil
	}
	db := req.DB
	if db == "" {
		db = "flow_log"
	}
	tbl := req.Table
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	selStart := strings.Index(selStr, "fast_list/")
	if selStart < 0 {
		return nil, nil
	}
	selStr = selStr[selStart+len("fast_list/"):]
	if idx := strings.IndexByte(selStr, '?'); idx >= 0 {
		selStr = selStr[:idx]
	}
	if selStr == "" {
		return nil, nil
	}

	var extras []string
	if req.Where != nil {
		for _, rs := range req.Where.ResourceSets {
			extras = append(extras, FlattenFastListConditions(rs.Condition, db)...)
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 999
	}

	// Resolve table name: for flow_metrics, append data_source suffix.
	resolvedTbl := tbl
	if db == "flow_metrics" && !strings.Contains(tbl, ".") {
		ds := req.DataSource
		if ds == "" {
			ds = "1m"
		}
		resolvedTbl = tbl + "." + ds
	}

	// Build debug info even when ZT is unavailable, so debug mode always produces output.
	di := &FastListDebugInfo{
		requestBody: body,
		db:          db,
		table:       resolvedTbl,
		selStr:      selStr,
		extras:      extras,
		limit:       limit,
		offset:      req.Offset,
	}
	di.sel = fmt.Sprintf("%s, Count(row) AS count_row", selStr)
	di.sql = query.BuildBaseSQL(di.sel, resolvedTbl, extras, req.TimeStart, req.TimeEnd,
		selStr, "count_row", "DESC", limit, req.Offset)

	// zt already passed as parameter
	if zt == nil || !zt.Available() {
		if debug {
			di.err = fmt.Errorf("zerotrace-server not configured or unavailable")
			return nil, di
		}
		return nil, nil
	}

	log.Printf("🔍 ZT fast_list: db=%s sql=%s", db, di.sql)
	di.queryStart = time.Now()
	rows, err := zt.QueryRaw(db, di.sql)
	di.queryEnd = time.Now()
	di.result = rows
	di.err = err

	if err != nil {
		log.Printf("⚠️  fast_list ZT error: %v", err)
		// Fallback: query ClickHouse directly when ZT aggregation fails.
		if chData := ChQueryFastList(ch, db, resolvedTbl, selStr, extras, req.TimeStart, req.TimeEnd, limit, req.Offset); chData != nil {
			if !debug {
				return chData, nil
			}
			data := chData
			di.err = nil
			di.result = &client.QueryResult{
				Columns: []string{selStr, "count_row"},
				Values:  make([][]interface{}, len(data)),
			}
			for i, row := range data {
				m, _ := row.(map[string]interface{})
				var vals []interface{}
				for _, col := range strings.Split(selStr, ",") {
					vals = append(vals, m[strings.TrimSpace(col)])
				}
				vals = append(vals, m["count_row"])
				di.result.Values[i] = vals
			}
			return data, di
		}
		if debug {
			return nil, di
		}
		return nil, nil
	}

	data := make([]interface{}, 0, len(rows.Values))
	for _, row := range rows.Values {
		r := make(map[string]interface{})
		for i, col := range rows.Columns {
			if i >= len(row) {
				continue
			}
			val := row[i]
			if strings.HasPrefix(col, "Enum(") {
				if s, ok := val.(string); ok {
					if cn := flowlog.EnumZHCN(s); cn != "" {
						val = cn
					}
				}
			}
			r[col] = val
		}
		r["_querier_region"] = "本地"
		data = append(data, r)
	}

	if debug {
		return data, di
	}
	return data, nil
}


// chQueryFastList queries ClickHouse directly for fast_list when ZT doesn't support the query.
// normalizeFastListSelect strips DeepFlow DSL functions for CH compatibility.
// E.g., Enum(observation_point) → observation_point, node_type(x) → x.

func BuildFastListDebug(body []byte, di *FastListDebugInfo) []interface{} {
	nowMs := di.queryStart.UnixMilli()

	// Parse the raw request for reuse.
	var req FastListRequest
	json.Unmarshal(body, &req) // safe: already parsed in queryFastList

	// Split selectors into a slice for groupBy.
	selParts := strings.Split(di.selStr, ",")

	// Build the QuerierJs-style nested conditions.
	resourceSets := make([]interface{}, 0)
	if req.Where != nil {
		for _, rs := range req.Where.ResourceSets {
			conditions := rs.Condition
			rsEntry := map[string]interface{}{
				"id": rs.ID,
				"condition": conditions,
				"groupBy": selParts,
			}
			resourceSets = append(resourceSets, rsEntry)
		}
	}

	// Build paths with same structure as request.
	paths := make([]interface{}, 0)
	if req.Where != nil && req.Where.Paths != nil {
		for _, p := range req.Where.Paths {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		paths = append(paths, map[string]string{"endPoint": "R1"})
	}

	// ---- Entry 0: QuerierJs发送请求 ----
	entry0 := map[string]interface{}{
		"name":   "QuerierJs发送请求",
		"url":    "/querier-params/",
		"method": "post",
		"headers": map[string]string{
			"X-Trace-Id":   fmt.Sprintf("trace-%x", time.Now().UnixNano()),
			"x-user-id":    "1",
			"x-user-type":  "1",
			"x-org-id":     "1",
			"Accept":       "application/json, text/plain, */*",
			"Content-Type": "application/json",
		},
		"time": nowMs,
		"body": map[string]interface{}{
			"conditions": map[string]interface{}{
				"RESOURCE_SETS": resourceSets,
			},
			"selects": map[string]interface{}{
				"TAGS":    []interface{}{},
				"METRICS": []interface{}{map[string]interface{}{"func": "count", "key": "row", "params": []interface{}{}}},
			},
			"groupBy":  []interface{}{},
			"tableName": di.table,
			"paths":    paths,
			"db":       di.db,
		},
	}

	// Build the SQL resource structure for entries 1 and 2.
	// The WHERE clause strips backtick quotes that may have been added by BuildBaseSQL.
	whereClause := strings.Join(di.extras, " AND ")

	// Build TAGS from the select string (everything except Count(row)).
	tagNames := make([]string, 0)
	metricNames := make([]string, 0)
	for _, part := range selParts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "Enum(") || strings.Contains(part, "signal_source") || !strings.Contains(part, "(") {
			tagNames = append(tagNames, part)
		}
	}
	metricNames = append(metricNames, "Count(`row`) AS `Count(行数)`")

	// Determine GROUP_BY: use the raw column names (strip Enum() wrappers),
	// deduplicated (Enum(x),x → x).
	seen := make(map[string]bool)
	groupByParts := make([]string, 0)
	for _, p := range selParts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "Enum(") && strings.HasSuffix(p, ")") {
			p = p[5 : len(p)-1]
		}
		if !seen[p] {
			seen[p] = true
			groupByParts = append(groupByParts, p)
		}
	}
	groupBy := strings.Join(groupByParts, ", ")

	// Build SELECT with newTag for the QuerierJs intermediate format.
	querierSelect := fmt.Sprintf("newTag('R1') as query_id, Count(`row`) AS `Count(行数)`, %s", di.selStr)

	sqlResource := map[string]interface{}{
		"QUERY_ID": "R1",
		"ROLES":    []string{"R"},
		"WHERE":    whereClause,
		"SELECT":   querierSelect,
		"TAGS":     tagNames,
		"METRICS":  metricNames,
		"GROUP_BY": groupBy,
	}

	sqlPath := map[string]interface{}{
		"QUERY_ID": "R1-R1",
		"ROLES":    []string{"R", "R"},
		"WHERE":    whereClause,
		"SELECT":   fmt.Sprintf("newTag('R1-R1') as query_id, Count(`row`) AS `Count(行数)`, %s", di.selStr),
		"TAGS":     tagNames,
		"CTAGS":    tagNames,
		"STAGS":    tagNames,
		"METRICS":  metricNames,
		"GROUP_BY": groupBy,
	}

	// Build return tag info from columns.
	returnTags := make([]interface{}, 0)
	for _, t := range tagNames {
		returnTags = append(returnTags, QuerierColumn{
			Name:        t,
			DisplayName: t,
			IsResource:  false,
		})
	}
	returnMetrics := make([]interface{}, 0)
	returnMetrics = append(returnMetrics, QuerierColumn{
		Name:        "Count(行数)",
		DisplayName: "Count(行数)",
		Type:        8,
		Unit:        "个",
	})

	uuid := fmt.Sprintf("query-%x", time.Now().UnixNano())

	// ---- Entry 1: QuerierJs收到响应 ----
	entry1 := map[string]interface{}{
		"name":   "QuerierJs收到响应",
		"url":    "/querier-params/",
		"status": 200,
		"time":   nowMs + 5,
		"data": map[string]interface{}{
			"OPT_STATUS": "SUCCESS",
			"DATA": map[string]interface{}{
				"resource": []interface{}{map[string]interface{}{
					"sql":          sqlResource,
					"returnTags":    returnTags,
					"returnMetrics": returnMetrics,
				}},
				"path": []interface{}{map[string]interface{}{
					"sql":          sqlPath,
					"returnTags":    returnTags,
					"returnMetrics": returnMetrics,
				}},
				"uuid": uuid,
			},
		},
	}

	// ---- Entry 2: Statistics发送请求 ----
	entry2 := map[string]interface{}{
		"name":   "Statistics发送请求",
		"url":    "/v1/stats/querier/FastList",
		"method": "post",
		"headers": map[string]string{
			"X-Trace-Id":   fmt.Sprintf("trace-%x", time.Now().UnixNano()),
			"x-user-id":    "1",
			"x-user-type":  "1",
			"x-org-id":     "1",
			"Accept":       "application/json, text/plain, */*",
			"Content-Type": "application/json",
		},
		"time": nowMs + 10,
		"body": map[string]interface{}{
			"DATABASE": di.db,
			"TABLE":    di.table,
			"QUERIES":  []interface{}{sqlPath},
			"SORT": map[string]interface{}{
				"ORDER_BY":  "Count(行数)",
				"SORTED_BY": "DESC",
			},
			"PAGE_INDEX":  1,
			"PAGE_SIZE":   di.limit,
			"time_start":  req.TimeStart,
			"time_end":    req.TimeEnd,
			"DEBUG":       1,
			"DATA_SOURCE": nil,
		},
	}

	// ---- Entry 3: Statistics收到响应 ----
	queryTime := di.queryEnd.Sub(di.queryStart).Seconds()

	// Build SCHEMAS from ZT result columns.
	schemas := make(map[string]interface{})
	if di.result != nil {
		for i, col := range di.result.Columns {
			s := map[string]interface{}{
				"label_type": "",
				"pre_as":     col,
				"type":       1,
				"unit":       "",
			}
			if i < len(di.result.Schemas) {
				s["value_type"] = di.result.Schemas[i].ValueType
				if di.result.Schemas[i].Type != 0 {
					s["type"] = di.result.Schemas[i].Type
				}
				if di.result.Schemas[i].Unit != "" {
					s["unit"] = di.result.Schemas[i].Unit
				}
			} else {
				s["value_type"] = "String"
			}
			schemas[col] = s
		}
	}

	entry3Data := map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
	}

	if di.err != nil {
		entry3Data["OPT_STATUS"] = "ERROR"
		entry3Data["DESCRIPTION"] = di.err.Error()
	} else {
		// Build DATA rows same as queryFastList.
		dataRows := make([]interface{}, 0)
		if di.result != nil {
			for _, row := range di.result.Values {
				r := make(map[string]interface{})
				for i, col := range di.result.Columns {
					if i >= len(row) {
						continue
					}
					val := row[i]
					if strings.HasPrefix(col, "Enum(") {
						if s, ok := val.(string); ok {
							if cn := flowlog.EnumZHCN(s); cn != "" {
								val = cn
							}
						}
					}
					r[col] = val
				}
				r["_querier_region"] = "本地"
				dataRows = append(dataRows, r)
			}
		}
		entry3Data["DATA"] = dataRows

		// _TSDB_INFO with our SQL.
		entry3Data["_TSDB_INFO"] = map[string]interface{}{
			"QUERY_TIME": queryTime,
			"QUERIES": []interface{}{map[string]interface{}{
				"query_id": map[string]string{"本地": uuid},
				"sql":      di.sql,
				"query_time":     queryTime,
				"query_region_times": map[string]float64{"本地": queryTime},
				"query_regions": map[string]interface{}{
					"本地": map[string]interface{}{
						"query_sqls": []interface{}{map[string]interface{}{
							"IP":           "deeptrace-zt",
							"Sql":          di.sql,
							"QueryTime":    fmt.Sprintf("%fs", queryTime),
							"CallbackTime": fmt.Sprintf("%fs", queryTime),
							"QueryUUID":    uuid,
							"Error":        "",
						}},
					},
				},
			}},
		}

		// _QUERY_IDS.
		entry3Data["_QUERY_IDS"] = []interface{}{map[string]interface{}{
			"query_name": "Get All Records-0",
			"query_id":   map[string]string{"本地": uuid},
		}}

		entry3Data["TYPE"] = "Application_Fast_List"
		entry3Data["SCHEMAS"] = schemas
	}

	entry3 := map[string]interface{}{
		"name":   "Statistics收到响应",
		"url":    "/v1/stats/querier/FastList",
		"status": 200,
		"time":   nowMs + 150,
		"data":   entry3Data,
	}

	if di.err != nil {
		entry3["status"] = 500
	}

	return []interface{}{entry0, entry1, entry2, entry3}
}

