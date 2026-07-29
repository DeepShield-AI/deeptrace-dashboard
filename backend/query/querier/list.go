package querier

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"deeptrace-backend/client"
	"deeptrace-backend/clickhouse"
	"deeptrace-backend/engine"
	"deeptrace-backend/query"
	"deeptrace-backend/query/flowlog"
)

func QueryList(zt *client.ZerotraceService, bodyStr string) (*query.Result, error) {
	if zt == nil || !zt.Available() { return nil, nil }
	var req query.QuerierListRequest
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil { return nil, nil }
	req.NormalizeQuery()
	if len(req.Queries) == 0 { return nil, nil }
	db := req.Database
	tbl := req.Table
	if db == "" { db = "flow_log" }
	if tbl == "" { tbl = "l7_flow_log" }
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
	if req.Sort != nil { sf, sd = req.Sort.OrderBy, req.Sort.SortedBy }
	sql, _ := buildSQL(req.Queries[0].Select, resolvedTbl, req.Queries[0].Where,
		ts, te, "", "", 0,
		req.PageSize, req.PageIndex, req.Interval, sf, sd, false)
	if sql == "" { return nil, nil }
	log.Printf("🔍 querier.List: db=%s sql=%s", db, sql)

	qid := ""
	if len(req.Queries) > 0 { qid = req.Queries[0].QueryID }
	rows, err := zt.QueryRaw(db, sql)
	if err != nil { log.Printf("🔍 querier.List: ZT unavailable (%v), falling back to CH", err); return nil, nil }
	if len(rows.Values) == 0 {
		return &query.Result{Data: []map[string]interface{}{}, Type: "Application_Detail_List"}, nil
	}
	data := flowlog.BuildData(rows, "")
	schemas := flowlog.BuildSchemas(rows, qid)
	fillListExtraFields(req.Queries[0].Select, data)
	translateEnumResults(data)
	os := rows.OptStatus
	if isPartial { os = "PARTIAL_RESULT" }
	if os == "" { os = "SUCCESS" }
	desc := ""
	if os == "PARTIAL_RESULT" { desc = "最大可查询时间为 1440 分钟" }
	return &query.Result{Data: data, Type: "Application_Detail_List", Fields: schemas, OptStatus: os, Description: desc}, nil
}

func QueryTop(zt *client.ZerotraceService, bodyStr string) (*query.Result, error) {
	if zt == nil || !zt.Available() { return nil, nil }
	var req query.QuerierListRequest
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil { return nil, nil }
	req.NormalizeQuery()
	if len(req.Queries) == 0 { return nil, nil }
	db := req.Database
	tbl := req.Table
	if db == "" { db = "flow_log" }
	if tbl == "" { tbl = "l7_flow_log" }
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
	if req.Sort != nil { sf, sd = req.Sort.OrderBy, req.Sort.SortedBy }
	sql, hasHist := buildSQL(req.Queries[0].Select, resolvedTbl, req.Queries[0].Where,
		ts, te, req.Queries[0].GroupBy, req.Queries[0].Select,
		int(req.Top), req.PageSize, req.PageIndex, req.Interval, sf, sd, true)
	if sql == "" { return nil, nil }
	log.Printf("🔍 querier.Top: db=%s sql=%s", db, sql)

	qid := ""
	if len(req.Queries) > 0 { qid = req.Queries[0].QueryID }
	rows, err := zt.QueryRaw(db, sql)
	if err != nil {
		log.Printf("🔍 querier.Top: ZT unavailable (%v), falling back to CH", err)
		return &query.Result{Data: []map[string]interface{}{}}, nil
	}
	if len(rows.Values) == 0 {
		return &query.Result{Data: []map[string]interface{}{}}, nil
	}
	data := flowlog.BuildData(rows, "")
	schemas := flowlog.BuildSchemas(rows, qid)
	fillTopExtraFields(req.Queries[0].Select, data)
	translateEnumResults(data)
	if hasHist {
		data = consolidateHistory(data, req.Queries[0].Select, req.Queries[0].Metrics,
			ts, te, int64(req.Interval), req.Fill)
	}
	fillTopMetaFields(data, req.Queries[0].Select, req.Queries[0].Tags)
	fillTopSchemas(schemas, req.Queries[0].Select, hasHist, req.Interval)
	os := rows.OptStatus
	if os == "" { os = "SUCCESS" }
	if isPartial { os = "PARTIAL_RESULT" }
	desc := ""
	if os == "PARTIAL_RESULT" { desc = "最大可查询时间为 1440 分钟" }
	return &query.Result{Data: data, Type: "", OptStatus: os, Description: desc}, nil
}

// consolidateHistory groups time-bucketed ZT rows into HISTORY arrays,
// filling missing time buckets with null when fill param is set.
func consolidateHistory(data []map[string]interface{}, sel string, metrics []string,
	timeStart, timeEnd, interval int64, fill string) []map[string]interface{} {
	if len(data) == 0 { return data }
	mk := parseMetricKeys(sel, metrics)
	hasTime := false
	for k := range data[0] { if k == "time" { hasTime = true; break } }
	if !hasTime { return data }
	type gk string
	gs := make(map[gk][]map[string]interface{})
	for _, row := range data {
		var p []string
		for k := range row {
			if k == "time" || k == "_querier_region" { continue }
			if mk[k] { continue }
			p = append(p, fmt.Sprintf("%v=%v", k, row[k]))
		}
		sort.Strings(p)
		key := gk(strings.Join(p, "|"))
		gs[key] = append(gs[key], row)
	}
	if len(gs) == len(data) { return data }
	var r []map[string]interface{}
	for _, g := range gs {
		if len(g) == 0 { continue }
		b := make(map[string]interface{})
		for k, v := range g[0] { if k != "time" { b[k] = v } }
		sort.Slice(g, func(i, j int) bool { ti, _ := toFloat64(g[i]["time"]); tj, _ := toFloat64(g[j]["time"]); return ti > tj })
		var h []map[string]interface{}
		seenTOI := map[int64]bool{}
		for _, row := range g {
			tv := row["time"]
			if s, ok := tv.(string); ok { if t, err := parseTime(s); err == nil { tv = t.Unix() } }
			if toi, ok := toFloat64(tv); ok && interval > 0 {
				tv = float64(int64(toi) / interval * interval)
			}
			toiKey := int64(toFloat64OrZero(tv))
			if seenTOI[toiKey] { continue }
			seenTOI[toiKey] = true
			pt := map[string]interface{}{"toi": tv}
			for mk2 := range mk { if v, ok := row[mk2]; ok { pt[mk2] = v } }
			h = append(h, pt)
		}
			// Fill missing time buckets with null
			if fill != "" && interval > 0 && timeEnd > timeStart {
				h = fillHistory(h, mk, timeStart, timeEnd, interval)
			}
		b["HISTORY"] = h
		r = append(r, b)
	}
	return r
}

func parseMetricKeys(sel string, metrics []string) map[string]bool {
	keys := map[string]bool{}
	for _, m := range metrics {
		idx := strings.LastIndex(m, " AS ")
		if idx >= 0 { a := strings.TrimSpace(m[idx+4:]); keys[strings.Trim(a, "`")] = true } else { keys[strings.Trim(m, "`")] = true }
	}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		if strings.HasPrefix(lower, "newtag(") ||
			strings.HasPrefix(lower, "enum(") ||
			strings.HasPrefix(lower, "node_type(") || strings.HasPrefix(lower, "icon_id(") { continue }
		if strings.Contains(item.Expr, "(") { keys[item.Key] = true }
	}
	return keys
}

// fallbackTopQueryCH tries a direct CH query when ZT fails on Top queries.

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) { case float64: return n, true; case int64: return float64(n), true; case json.Number: if f, err := n.Float64(); err == nil { return f, true } }
	return 0, false
}

func toFloat64OrZero(v interface{}) float64 {
	f, ok := toFloat64(v)
	if !ok { return 0 }
	return f
}

func fillTopMetaFields(data []map[string]interface{}, sel string, tags []string) {
	excl := computeExcludedColumns(sel)
	for _, row := range data {
		if _, ok := row["UID"]; ok { continue }
		var keys []string
		for k := range row { if k == "HISTORY" || k == "_querier_region" || k == "UID" || excl[k] { continue }; keys = append(keys, k) }
		sort.Strings(keys)
		var ups []string
		for _, k := range keys { if v := row[k]; v != nil { ups = append(ups, fmt.Sprintf("%s=%v", k, v)) } }
		row["UID"] = strings.Join(ups, ",")
		if len(ups) > 0 { row["UID_NAME"] = ups[len(ups)-1] } else { row["UID_NAME"] = "" }
		if _, ok := row["Enum(response_status)"]; ok { row["FULL_NAME"] = fmt.Sprintf("响应状态=%v，响应状态=%v", row["Enum(response_status)"], row["response_status"]) } else { row["FULL_NAME"] = "" }
		row["NAME"] = ""
		tm := map[string]interface{}{}
		for _, k := range keys { if v, ok := row[k]; ok && v != nil { tm[k] = v } }
		if b, err := json.Marshal(tm); err == nil { row["TAGS"] = string(b) } else { row["TAGS"] = "{}" }
	}
}

func computeExcludedColumns(sel string) map[string]bool {
	excl := map[string]bool{}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		if strings.HasPrefix(lower, "newtag(") || strings.HasPrefix(lower, "enum(") ||
			strings.HasPrefix(lower, "node_type(") || strings.HasPrefix(lower, "icon_id(") { continue }
		if strings.Contains(item.Expr, "(") { excl[item.Key] = true }
	}
	return excl
}

func fillTopSchemas(schemas map[string]interface{}, sel string, hasTimeGroupBy bool, interval int) {
	if schemas == nil { return }
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		if _, exists := schemas[item.Key]; exists { continue }
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
		if e != nil { schemas[item.Key] = e }
	}
	if hasTimeGroupBy {
		if _, exists := schemas["toi"]; !exists {
			iv := interval
			if iv <= 0 { iv = 0 }
			schemas["toi"] = map[string]interface{}{"label_type": "", "pre_as": fmt.Sprintf("time(time, %d, 1, null)", iv), "type": 0, "unit": "", "value_type": "UInt32"}
		}
	}
}

func propagateUnits(schemas map[string]interface{}, rows *client.QueryResult) {
	if schemas == nil || rows == nil { return }
	for i, col := range rows.Columns {
		if i >= len(rows.Schemas) { continue }
		m, ok := schemas[col].(map[string]interface{})
		if !ok { continue }
		if u := rows.Schemas[i].Unit; u != "" { m["unit"] = u }
		if t := rows.Schemas[i].Type; t > 0 { m["type"] = t }
	}
}

func parseTime(s string) (time.Time, error) {
	formats := []string{"2006-01-02T15:04:05-07:00", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02T15:04:05", time.RFC3339}
	for _, f := range formats { if t, err := time.Parse(f, s); err == nil { return t, nil } }
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

func fillTopExtraFields(sel string, data []map[string]interface{}) {
	if sel == "" || len(data) == 0 { return }
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		var val interface{}; fill := false
		switch {
		case strings.HasPrefix(lower, "newtag("):
			v := strings.TrimSpace(item.Expr[len("newTag("):len(item.Expr)-1]); val = strings.Trim(v, "'\""); fill = true
		case strings.HasPrefix(lower, "node_type("):
			val = "_"; fill = true
		case strings.HasPrefix(lower, "icon_id("):
			val = engine.IconIDDefault(item.Key); fill = true
		case strings.HasPrefix(lower, "enum("):
			val = nil; fill = true
			if len(data) > 0 { if v, ok := data[0][strings.TrimSpace(item.Expr[len("Enum("):len(item.Expr)-1])]; ok { val = v } }
		default:
			var n float64; if _, err := fmt.Sscanf(item.Expr, "%f", &n); err == nil { val = n; fill = true }
		}
		if fill { for _, row := range data { if _, exists := row[item.Key]; !exists { row[item.Key] = val } } }
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
				if cn := flowlog.EnumZHCN(s); cn != "" {
					row[k] = cn
				}
			}
		}
	}
}

func fillListExtraFields(sel string, data []map[string]interface{}) {
	if sel == "" || len(data) == 0 { return }
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		var val interface{}; fill := false
		switch {
		case strings.HasPrefix(lower, "newtag("):
			v := strings.TrimSpace(item.Expr[len("newTag("):len(item.Expr)-1]); val = strings.Trim(v, "'\""); fill = true
		case strings.HasPrefix(lower, "node_type("):
			val = "_"; fill = true
		case strings.HasPrefix(lower, "icon_id("):
			val = engine.IconIDDefault(item.Key); fill = true
		case strings.HasPrefix(lower, "enum("):
			val = nil; fill = true
			if len(data) > 0 { if v, ok := data[0][strings.TrimSpace(item.Expr[len("Enum("):len(item.Expr)-1])]; ok { val = v } }
		}
		if fill { for _, row := range data { if _, exists := row[item.Key]; !exists { row[item.Key] = val } } }
	}
}

func buildWhere(timeStart, timeEnd int64, whereFilter string) string {
	var c []string
	if timeStart > 0 { c = append(c, fmt.Sprintf("time >= %d", timeStart)) }
	if timeEnd > 0 { c = append(c, fmt.Sprintf("time <= %d", timeEnd)) }
	if whereFilter != "" { c = append(c, whereFilter) }
	if len(c) == 0 { return "" }
	return " WHERE " + strings.Join(c, " AND ")
}

func buildOrder(req *query.QuerierListRequest) string {
	if req.Sort == nil || req.Sort.OrderBy == "" { return "" }
	d := "ASC"
	if strings.ToUpper(req.Sort.SortedBy) == "DESC" { d = "DESC" }
	return fmt.Sprintf(" ORDER BY `%s` %s", req.Sort.OrderBy, d)
}

func isNum(expr string) bool {
	var n float64; _, err := fmt.Sscanf(expr, "%f", &n)
	return err == nil
}

func fillHistory(h []map[string]interface{}, mk map[string]bool, timeStart, timeEnd, interval int64) []map[string]interface{} {
	if interval <= 0 { interval = 1 }
	existing := map[int64]bool{}
	for _, pt := range h {
		if toi, ok := toFloat64(pt["toi"]); ok {
			existing[int64(toi)] = true
		}
	}
	// Fill from timeEnd - 30*interval down to timeStart
	fillEnd := timeEnd - timeEnd%interval
	fillStart := timeStart - timeStart%interval
	maxPts := int64(30) * interval
	if fillEnd-fillStart > maxPts {
		fillStart = fillEnd - maxPts
	}
	for t := fillEnd; t >= fillStart; t -= interval {
		if existing[t] { continue }
		pt := map[string]interface{}{"toi": t}
		for mk2 := range mk { pt[mk2] = nil }
		h = append(h, pt)
	}
	sort.Slice(h, func(i, j int) bool {
		ti, _ := toFloat64(h[i]["toi"]); tj, _ := toFloat64(h[j]["toi"])
		return ti > tj
	})
	return h
}
