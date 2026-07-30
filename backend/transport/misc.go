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

	"deeptrace-backend/client"
	"deeptrace-backend/engine"
	"deeptrace-backend/query"
	"deeptrace-backend/query/flowlog"
)

func RegisterMisc(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/df-web/v1/icons", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		writeSuccess(w, []interface{}{})
	})
	mux.HandleFunc("/api/df-web/v1/config/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "outerlinks") { writeSuccess(w, []interface{}{}); return }
		writeSuccess(w, map[string]interface{}{
			"VERSION": "v7.1", "COMPANY": "DeepTrace",
			"SUPPORT_EMAIL": "admin@deeptrace.local", "SITE_TITLE": "DeepTrace",
			"DEPLOY_MODE": "k8s", "BILLING_METHOD": "voucher",
			"SFLOW_MENU_ENABLED": "false", "NTP_SERVERS": "0.cn.pool.ntp.org",
		})
	})
	mux.HandleFunc("/api/df-web/v1/indicator_template", func(w http.ResponseWriter, r *http.Request) {
		tableName := r.URL.Query().Get("table_name")
		dbName := r.URL.Query().Get("db_name")
		if tableName == "" {
			tableName = "application"
		}
		if dbName == "" {
			dbName = "flow_metrics"
		}
		writeSuccess(w, []interface{}{
			map[string]interface{}{
				"ID":              1,
				"LCUUID":          "00000000-0000-0000-0000-000000000001",
				"TEMPLATE_NAME":   "常用模板",
				"TABLE_NAME":      tableName,
				"DB_NAME":         dbName,
				"DEF_TEMP":        0,
				"USER_ID":         1,
				"CREATED_AT":      "2025-01-01 00:00:00",
				"UPDATED_AT":      "2025-01-01 00:00:00",
				"TEAM_INFO":       map[string]interface{}{"current_role": 4, "name": "Default", "team_id": 1},
				"TEAM_ID":         1,
				"OWNER_USER_INFO": map[string]interface{}{"ID": 1, "USERNAME": "admin"},
				"ACCESS_ACTIONS":  []string{"read"},
			},
		})
	})
	mux.HandleFunc("/api/df-web/v1/logo_info", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{"LOGO_URL": "", "FAVICON_URL": "", "TITLE": "DeepTrace"})
	})
	mux.HandleFunc("/api/alarm/", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		writeSuccess(w, []interface{}{})
	})
	mux.HandleFunc("/api/warrant/", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{
			"LICENSE_DATA": true, "CHECK_HOST": true, "CHECK_IP": true,
			"LICENSE_FUNCTION": []string{
				"application_observation", "network_observation",
				"infrastructure_observation", "network_tracing",
				"system_tracing", "application_tracing",
				"call_log", "flow_log", "profile",
			},
		})
	})
	mux.HandleFunc("/api/df-web/v1/search-histories", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		writeSuccess(w, []interface{}{})
	})
	mux.HandleFunc("/api/df-web/v1/fast_filter_black_lists", handleFastFilterBlackLists)
	mux.HandleFunc("/api/fuser/v1/user/", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		writeSuccess(w, map[string]interface{}{"params": []interface{}{}})
	})

	mux.HandleFunc("/api/df-web-composer/api/querier/fast_list/", handleFastList(deps))
	mux.HandleFunc("/api/df-web-composer/api/querier/fast_list", handleFastList(deps))
	mux.HandleFunc("/api/df-web-composer/api/service_topo/entry_path_overview", handleServiceOverview())
	mux.HandleFunc("/api/df-web-composer/api/service_topo/", handleServiceTopo())
	mux.HandleFunc("/api/df-web-composer/api/l7_flowlog_analysis/duration_detail_with_max/", handleDurationDetail(deps))
	mux.HandleFunc("/api/df-web-composer/", handleComposerFallback(deps))
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

var virtualColumnMap = map[string]string{
	"auto_service":      "auto_service_id",
	"auto_instance":     "auto_instance_id",
	"auto_service_type": "auto_service_type",
	"chost":             "l3_device_id",
	"host":              "l3_device_id",
	"vpc":               "epc_id",
	"pod_service":       "pod_service_id",
	"pod_group":         "pod_group_id",
	"pod_cluster":       "pod_cluster_id",
	"pod_ns":            "pod_ns_id",
	"pod_node":          "pod_node_id",
	"subnet":            "subnet_id",
	"router":            "router_id",
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
			if physicalCol, mapped := virtualColumnMap[col]; mapped {
				if _, isNum := val.(float64); isNum {
					col = physicalCol
					// For flow_log, resource columns use _0/_1 suffix.
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

// Column info for a tag or metric in the QuerierJs intermediate response.
type querierColumn struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	IsResource  bool   `json:"isResource"`
	Type        int    `json:"type,omitempty"`
	Unit        string `json:"unit,omitempty"`
}

func handleFastList(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		log.Printf("🎼 fast_list %s body=%d", r.URL.Path[:min(len(r.URL.Path), 60)], len(body))
		debug := r.URL.Query().Get("debug") == "1"
		data, di := queryFastList(deps, r, body, debug)
		if data == nil && di == nil {
			writeSuccess(w, []interface{}{})
			return
		}
		resp := map[string]interface{}{"OPT_STATUS": "SUCCESS", "DATA": data}
		if debug && di != nil {
			resp["_debug"] = buildFastListDebug(deps, r, body, di)
		}
		writeJSON(w, resp)
	}
}

// fastListDebugInfo collects all internal state during a fast_list query for debug output.
type fastListDebugInfo struct {
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

func queryFastList(deps *Dependencies, r *http.Request, body []byte, debug bool) ([]interface{}, *fastListDebugInfo) {
	var req fastListRequest
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
	selStart := strings.Index(r.URL.Path, "fast_list/")
	if selStart < 0 {
		return nil, nil
	}
	selStr := r.URL.Path[selStart+len("fast_list/"):]
	if idx := strings.IndexByte(selStr, '?'); idx >= 0 {
		selStr = selStr[:idx]
	}
	if selStr == "" {
		return nil, nil
	}

	var extras []string
	if req.Where != nil {
		for _, rs := range req.Where.ResourceSets {
			extras = append(extras, flattenFastListConditions(rs.Condition, db)...)
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
	di := &fastListDebugInfo{
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

	zt := deps.Querier.Zerotrace
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
		if chData := chQueryFastList(deps, db, resolvedTbl, selStr, extras, req.TimeStart, req.TimeEnd, limit, req.Offset); chData != nil {
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
func normalizeFastListSelect(sel string) string {
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

func chQueryFastList(deps *Dependencies, db, tbl, selStr string, extras []string,
	timeStart, timeEnd int64, limit, offset int) []interface{} {
	if deps.CH == nil || !deps.CH.Enabled() {
		return nil
	}
	// Build a ClickHouse-compatible SQL.
	chSel := normalizeFastListSelect(selStr)
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

	rows, err := chHTTPQuery(sql)
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
		eRes, eErr := chHTTPQuery(fmt.Sprintf("SELECT toString(value), name_zh FROM flow_tag.int_enum_map WHERE tag_name='%s'", col))
		m := map[string]string{}
		if eErr == nil {
			for _, er := range eRes {
				k := fmt.Sprintf("%v", er["toString(value)"])
				v := getSVStr(er, "name_zh")
				m[k] = v
			}
		}
		if len(m) == 0 {
			if fb, ok := builtinEnumFallback[col]; ok {
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
//	[3] Statistics收到响应 — Statistics query result with _TSDB_INFO
func buildFastListDebug(deps *Dependencies, r *http.Request, body []byte, di *fastListDebugInfo) []interface{} {
	nowMs := di.queryStart.UnixMilli()

	// Parse the raw request for reuse.
	var req fastListRequest
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
		returnTags = append(returnTags, querierColumn{
			Name:        t,
			DisplayName: t,
			IsResource:  false,
		})
	}
	returnMetrics := make([]interface{}, 0)
	returnMetrics = append(returnMetrics, querierColumn{
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

func handleServiceOverview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{
			"overviewTrend": []interface{}{}, "overviewList": []interface{}{},
		})
	}
}

func handleServiceTopo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "alert_event") {
			writeSuccess(w, map[string]interface{}{
				"alertLevelCount": map[string]int{}, "alertTrend": []interface{}{},
				"alertActiveLevelTrend": []interface{}{}, "alertActiveLevelIntervals": []interface{}{},
			})
			return
		}
		writeSuccess(w, map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}})
	}
}

// durationDetailRequest is the request body for l7_flowlog_analysis/duration_detail_with_max.
type durationDetailRequest struct {
	TimeStart int64     `json:"time_start"`
	TimeEnd   int64     `json:"time_end"`
	Region    string    `json:"region"`
	Offset    int       `json:"offset"`
	Limit     int       `json:"limit"`
	GroupBy   []string  `json:"group_by"`
	Where     *struct {
		Paths        []map[string]string `json:"paths"`
		ResourceSets []struct {
			ID        string        `json:"id"`
			Condition []interface{} `json:"condition"`
			GroupBy   []string      `json:"groupBy"`
		} `json:"resourceSets"`
	} `json:"where"`
}

func handleDurationDetail(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req durationDetailRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		if req.Limit <= 0 { req.Limit = 20 }
		if req.TimeStart == 0 || req.TimeEnd == 0 {
			writeSuccess(w, map[string]interface{}{"result": []interface{}{}})
			return
		}
		log.Printf("📊 duration_detail: time=%d-%d limit=%d", req.TimeStart, req.TimeEnd, req.Limit)

		var tagConditions, metricConditions []string
		var allGroupBys []string
		seenGroupBy := map[string]bool{}

		if req.Where != nil {
			for _, rs := range req.Where.ResourceSets {
				conds := flattenFastListConditions(rs.Condition, "flow_log")
				for _, c := range conds {
					if strings.Contains(c, "response_duration") || strings.Contains(c, "request") {
						metricConditions = append(metricConditions, c)
					} else {
						// Strip ZT-only virtual columns not present in CH (e.g., role).
						skip := false
						for _, vcol := range []string{"role", "is_internet"} {
							if strings.Contains(c, "`"+vcol+"`") { skip = true; break }
						}
						if !skip {
							tagConditions = append(tagConditions, c)
						}
					}
				}
				for _, gb := range rs.GroupBy {
					if !seenGroupBy[gb] { allGroupBys = append(allGroupBys, gb); seenGroupBy[gb] = true }
				}
			}
		}
		for _, gb := range req.GroupBy {
			if !seenGroupBy[gb] { allGroupBys = append(allGroupBys, gb); seenGroupBy[gb] = true }
		}

		// Strip ZT-only virtual columns not present in CH.
		ztVirtual := map[string]bool{"is_internet": true, "role": true}
		var filteredGB []string
		for _, gb := range allGroupBys {
			if !ztVirtual[gb] { filteredGB = append(filteredGB, gb) }
		}
		allGroupBys = filteredGB

		tagColMap := map[string]string{
			"auto_service": "app_service", "auto_instance": "app_instance",
			"observation_point": "observation_point",
		}
		var tagCols, groupParts []string
		for _, gb := range allGroupBys {
			mapped := gb
			if m, ok := tagColMap[gb]; ok { mapped = m }
			tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mapped, gb))
			groupParts = append(groupParts, fmt.Sprintf("`%s`", mapped))
		}
		for _, side := range []string{"_0", "_1"} {
			for _, tag := range []string{"auto_service_id", "auto_service_type"} {
				col := tag + side
				if !seenGroupBy[col] {
					tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", col, col))
					groupParts = append(groupParts, fmt.Sprintf("`%s`", col))
				}
			}
			col := "auto_service" + side
			if !seenGroupBy[col] {
				tagCols = append(tagCols, "`app_service` AS `"+col+"`")
				groupParts = append(groupParts, "`app_service`")
			}
		}

		metricSelects := []string{
			"count(*) AS `Sum(请求)`",
			"avg(response_duration) AS `Avg(响应时延)`",
			"countIf(response_status != 2) / count(*) * 100 AS `Avg(响应比例)`",
			"countIf(response_status = 0) / count(*) * 100 AS `Avg(正常比例)`",
		}

		var wheres []string
		if req.TimeStart > 0 { wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart)) }
		if req.TimeEnd > 0 { wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd)) }
		wheres = append(wheres, tagConditions...)
		wheres = append(wheres, metricConditions...)
		whereStr := ""
		if len(wheres) > 0 { whereStr = " WHERE " + strings.Join(wheres, " AND ") }

		fullTable := "`flow_log`.`l7_flow_log`"
		allSel := append(tagCols, metricSelects...)
		sql := fmt.Sprintf("SELECT %s FROM %s%s GROUP BY %s ORDER BY `Sum(请求)` DESC LIMIT %d",
			strings.Join(allSel, ", "), fullTable, whereStr, strings.Join(groupParts, ", "), req.Limit)
		if req.Offset > 0 { sql += fmt.Sprintf(" OFFSET %d", req.Offset) }
		log.Printf("📊 duration_detail SQL: %s", sql)

		rows, err := chHTTPQuery(sql)
		if err != nil {
			log.Printf("⚠️ duration_detail CH error: %v", err)
			writeSuccess(w, map[string]interface{}{"result": []interface{}{}})
			return
		}

		result := make([]map[string]interface{}, 0, len(rows))
		for _, row := range rows {
			r := make(map[string]interface{})
			r["query_id"] = "R1-R2"
			for k, v := range row { r[k] = v }
			for _, prefix := range []string{"client_", "server_"} {
				typeCol := "auto_service_type_0"
				iconKey, nodeKey := prefix+"icon_id", prefix+"node_type"
				if prefix == "server_" { typeCol = "auto_service_type_1" }
				if _, exists := r[nodeKey]; !exists {
					if tv, ok := r[typeCol]; ok {
						if t, ok2 := toInt(tv); ok2 {
							r[nodeKey] = engine.NodeTypeFor(t)
							r[iconKey] = engine.IconFor(t)
						}
					}
				}
			}
			r["_querier_region"] = "本地"
			if _, exists := r["Enum(observation_point)"]; !exists {
				if op, ok := r["observation_point"]; ok {
					if fb, found := builtinEnumFallback["observation_point"]; found {
						if display, ok2 := fb[fmt.Sprintf("%v", op)]; ok2 {
							r["Enum(observation_point)"] = display
						} else { r["Enum(observation_point)"] = op }
					} else { r["Enum(observation_point)"] = op }
				}
			}
			result = append(result, r)
		}

		var maxRequest, maxDuration map[string]interface{}
		var maxReqVal, maxDurVal float64
		for _, r := range result {
			if v, ok := toFloat(r["Sum(请求)"]); ok && (maxRequest == nil || v > maxReqVal) {
				maxRequest, maxReqVal = r, v
			}
			if v, ok := toFloat(r["Avg(响应时延)"]); ok && (maxDuration == nil || v > maxDurVal) {
				maxDuration, maxDurVal = r, v
			}
		}

		resp := map[string]interface{}{"result": result, "maxRequest": maxRequest, "maxResponseDuration": maxDuration}
		writeSuccess(w, resp)
	}
}


func handleComposerFallback(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		log.Printf("🎼 COMPOSER fallback %s %s", r.Method, r.URL.Path)
		writeSuccess(w, []interface{}{})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// toFloat safely converts a value to float64.
func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// toInt safely converts a value to int.
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}
