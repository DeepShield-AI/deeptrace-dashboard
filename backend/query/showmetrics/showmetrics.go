package showmetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/logging"
)

// QueryMetricsMerged returns the merged ShowMetrics list for a request.
// Merges deepflow-server (ZT) metrics with ClickHouse column metrics (dedup by name),
// falling back to a hardcoded minimal list when both sources are unavailable.
// language ("zh" / "en") picks the primary display fields, matching the cloud
// contract where X-Language: zh swaps display_name/description/unit to the
// Chinese variants.
func QueryMetricsMerged(zt *client.ZerotraceService, bodyStr, language string) []interface{} {
	db, tbl := "flow_log", "l7_flow_log"
	var req struct {
		Database string `json:"DATABASE"`
		Table    string `json:"TABLE"`
	}
	if json.Unmarshal([]byte(bodyStr), &req) == nil {
		if req.Database != "" {
			db = req.Database
		}
		if req.Table != "" {
			tbl = req.Table
		}
	}

	// Collect metrics from multiple sources and merge for best coverage.
	var mergedMetrics []interface{}
	seen := map[string]bool{}

	// 1. Try deepflow-server (ZT) — returns virtual tags and metrics.
	if zt != nil && zt.Available() {
		for _, mm := range queryMetricsZT(zt, bodyStr, language) {
			key := fmt.Sprintf("%s.%s.%s", db, tbl, mm["name"])
			if !seen[key] {
				seen[key] = true
				mergedMetrics = append(mergedMetrics, mm)
			}
		}
	}

	// 2. Try ClickHouse HTTP for column metadata (fills gaps ZT misses).
	if chMetrics := QueryShowMetricsCH(db, tbl); chMetrics != nil {
		for _, m := range chMetrics {
			if mm, ok := m.(map[string]interface{}); ok {
				key := fmt.Sprintf("%s.%s.%s", db, tbl, mm["name"])
				if !seen[key] {
					seen[key] = true
					mergedMetrics = append(mergedMetrics, mm)
				}
			}
		}
	}

	if len(mergedMetrics) > 0 {
		return mergedMetrics
	}

	// 3. Fallback: hardcoded minimal list.
	return FallbackShowMetrics(tbl)
}

// queryMetricsZT queries deepflow-server for metric metadata.
// Returns the converted row maps (nil if unavailable).
func queryMetricsZT(zt *client.ZerotraceService, bodyStr, language string) []map[string]interface{} {
	var req struct {
		Database string `json:"DATABASE"`
		Table    string `json:"TABLE"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil
	}

	db := req.Database
	if db == "" {
		db = "flow_log"
	}
	tbl := req.Table
	if tbl == "" {
		tbl = "l7_flow_log"
	}

	// No backticks around the table name: the deepflow-server parser returns
	// an incomplete list (missing all virtual/aggregate metrics such as
	// request/response/rrt) and a backtick-quoted table field when the table
	// is backtick-quoted. Bare table names return the full 83-entry list.
	sql := fmt.Sprintf("SHOW metrics FROM %s", tbl)

	rows, err := zt.QueryRaw(db, sql)
	if err != nil {
		logging.Errorf("ZT ShowMetrics error: %v (db=%s tbl=%s)", err, db, tbl)
		return nil
	}
	if len(rows.Values) == 0 {
		return []map[string]interface{}{}
	}

	// Convert columns+values to []map[string]interface{}.
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
		// Language switch: the cloud (X-Language: zh) reports the primary
		// display_name/description/unit fields in the request language,
		// while deepflow-server returns English primaries + zh/en variants.
		if language == "zh" {
			if zh, ok := r["display_name_zh"].(string); ok && zh != "" {
				r["display_name"] = zh
			}
			if zh, ok := r["description_zh"].(string); ok && zh != "" {
				r["description"] = zh
			}
			if zh, ok := r["unit_zh"].(string); ok && zh != "" {
				r["unit"] = zh
			}
		} else if en, ok := r["display_name_en"].(string); ok && en != "" {
			r["display_name"] = en
		}
		data = append(data, r)
	}

	return data
}

var chHost = GetCHHTTPHost()

func GetCHHTTPHost() string {
	host := os.Getenv("CLICKHOUSE_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("CLICKHOUSE_HTTP_PORT")
	if port == "" {
		port = "8123"
	}
	return "http://" + host + ":" + port
}

// chHTTPQuery runs a ClickHouse SQL query via HTTP and returns the parsed data.
func HTTPQuery(query string) ([]map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reqURL := chHost + "/?query=" + url.QueryEscape(query+" FORMAT JSON")
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse: %s / body: %s", err, string(body[:min(len(body), 200)]))
	}
	return result.Data, nil
}

// ---------------------------------------------------------------------------
// ShowMetrics: build metric entries from ClickHouse system.columns
// ---------------------------------------------------------------------------

// queryShowMetricsCH builds the ShowMetrics response by querying ClickHouse.
// For flow_metrics database, appends ".1m" suffix if no results with bare table name.
// Returns nil if ClickHouse is unavailable.
func QueryShowMetricsCH(database, table string) []interface{} {
	// Try with table name first; for flow_metrics, try with .1m suffix.
	tableNames := []string{table}
	if database == "flow_metrics" && !strings.Contains(table, ".") {
		tableNames = []string{table + ".1m", table}
	}
	var rows []map[string]interface{}
	var lastErr error
	for _, tn := range tableNames {
		query := fmt.Sprintf("SELECT name, type, comment FROM system.columns WHERE database='%s' AND table='%s' ORDER BY position", database, tn)
		rows, lastErr = HTTPQuery(query)
		if lastErr == nil && len(rows) > 0 {
			break
		}
	}
	if lastErr != nil {
		logging.Errorf("ShowMetrics CH query failed: %v", lastErr)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	type chCol struct {
		Name    string
		Type    string
		Comment string
	}
	cols := make([]chCol, 0, len(rows))
	for _, row := range rows {
		cols = append(cols, chCol{
			Name:    clickhouse.Get[string](row, "name"),
			Type:    clickhouse.Get[string](row, "type"),
			Comment: clickhouse.Get[string](row, "comment"),
		})
	}

	var metrics []interface{}
	seen := map[string]bool{}

	// Helper to add a metric entry.
	addMetric := func(name, displayName, unit string, isAgg bool, typ int, category, description string) {
		key := database + "." + table + "." + name
		if seen[key] {
			return
		}
		seen[key] = true

		entry := map[string]interface{}{
			"name":            name,
			"is_agg":          isAgg,
			"display_name":    displayName,
			"display_name_zh": displayName,
			"display_name_en": displayName,
			"unit":            unit,
			"unit_zh":         unit,
			"unit_en":         unit,
			"type":            typ,
			"category":        category,
			"operators":       []string{">=", "<=", ">", "<", "="},
			"permissions":     []bool{true, true, true},
			"table":           table,
			"description":     description,
			"description_zh":  description,
			"description_en":  description,
		}
		metrics = append(metrics, entry)
	}

	// Derived display name from column name.
	displayFromName := func(name string) string {
		s := strings.ReplaceAll(name, "_", " ")
		if len(s) > 0 {
			s = strings.ToUpper(s[:1]) + s[1:]
		}
		return s
	}

	// classifyColumn determines (metricType, category, isAgg, unit) from column name and type.
	classifyColumn := func(name, colType string) (int, string, bool, string) {
		lowName := strings.ToLower(name)
		lowType := strings.ToLower(colType)

		isTag := false
		switch {
		case strings.HasPrefix(lowName, "is_"):
			isTag = true
		case strings.HasSuffix(lowName, "_type") || strings.HasSuffix(lowName, "_type_0") || strings.HasSuffix(lowName, "_type_1"):
			isTag = true
		case strings.HasSuffix(lowName, "_0") || strings.HasSuffix(lowName, "_1"):
			// All _0/_1 suffix columns are resource tags (client/server side).
			// Skip internal raw columns like ip4_0, ip6_0.
			if !strings.HasPrefix(lowName, "ip4_") && !strings.HasPrefix(lowName, "ip6_") {
				isTag = true
			}
		case strings.HasSuffix(lowName, "_id_0") || strings.HasSuffix(lowName, "_id_1"):
			isTag = true
		case strings.HasSuffix(lowName, "_source"):
			isTag = true
		case strings.HasPrefix(lowName, "capture_"):
			isTag = true
		case lowName == "protocol" || lowName == "l7_protocol" || lowName == "l7_protocol_str":
			isTag = true
		case lowName == "flow_id":
			isTag = true
		case lowName == "type" || lowName == "version" || lowName == "span_kind":
			isTag = true
		case lowName == "response_status" || lowName == "response_code" || lowName == "request_id" || lowName == "response_result":
			isTag = true
		case strings.HasPrefix(lowName, "syscall_trace_id"):
			isTag = true
		case strings.HasSuffix(lowName, "_port"):
			isTag = true
		case strings.HasSuffix(lowName, "_seq"):
			isTag = true
		case strings.HasPrefix(lowName, "tag_source"):
			isTag = true
		case lowName == "biz_code" || lowName == "biz_scenario" || lowName == "biz_type" || lowName == "biz_protocol" || lowName == "biz_response_code":
			isTag = true
		case strings.HasPrefix(lowName, "x_request_id"):
			isTag = true
		case strings.Contains(lowName, "observation_point"):
			isTag = true
		case strings.Contains(lowName, "tap_"):
			isTag = true
		case lowName == "nat_source" || lowName == "tunnel_type":
			isTag = true
		case strings.HasPrefix(lowName, "attribute_"):
			isTag = true
		case strings.HasPrefix(lowName, "metrics_"):
			isTag = true
		case lowName == "team_id" || lowName == "agent_id":
			isTag = true
		case lowName == "end_time" || lowName == "start_time" || lowName == "time":
			isTag = true
		case strings.Contains(lowName, "role"):
			isTag = true
		case strings.HasPrefix(lowName, "k8s.") || strings.HasPrefix(lowName, "cloud.") || strings.HasPrefix(lowName, "os."):
			isTag = true
		}

		isNumeric := strings.Contains(lowType, "uint") || strings.Contains(lowType, "int") || strings.Contains(lowType, "float")
		isString := strings.Contains(lowType, "string") || strings.Contains(lowType, "char") || strings.Contains(lowType, "enum")
		isDateTime := strings.Contains(lowType, "datetime") || strings.Contains(lowType, "timestamp")
		isIP := strings.Contains(lowType, "ip") && !isNumeric
		isArray := strings.Contains(lowType, "array")

		switch {
		case isArray:
			return 7, "Native Metric", false, ""
		case isTag || isString || isDateTime || isIP:
			return 6, "Tag", false, ""
		case isNumeric:
			switch {
			case strings.Contains(lowName, "rrt") || strings.Contains(lowName, "rtt"):
				return 3, "Delay", false, "us"
			case strings.Contains(lowName, "duration") || strings.Contains(lowName, "delay"):
				return 3, "Delay", false, "us"
			case strings.Contains(lowName, "error") || strings.Contains(lowName, "exception"):
				return 1, "Error", true, ""
			case lowName == "direction_score":
				return 9, "Throughput", false, ""
			default:
				return 1, "Throughput", true, ""
			}
		default:
			return 6, "Tag", false, ""
		}
	}

	// Process each column.
	for _, c := range cols {
		name := c.Name

		// Skip internal columns.
		if name == "_id" || name == "team_id" || name == "_tid" || name == "time" || name == "is_key_service" || name == "agent_id" {
			continue
		}
		// Skip raw device/epc columns (internal CH implementation).
		if strings.HasPrefix(name, "l3_device") || strings.HasPrefix(name, "l3_epc") || strings.HasPrefix(name, "tag_source") ||
			strings.HasPrefix(name, "capture_network_type_id") || name == "host" || strings.HasPrefix(name, "host_") {
			continue
		}
		// Skip role and is_internet — added as virtual tags with proper display names.
		if name == "role" || name == "is_internet" {
			continue
		}

		// Skip internal aggregation columns.
		if strings.HasSuffix(name, "_sum") || strings.HasSuffix(name, "_count") || strings.HasSuffix(name, "_max") {
			continue
		}

		// Skip IP raw columns (ip4_0, ip4_1, ip6_0, ip6_1 handled separately).
		if strings.HasPrefix(name, "ip4_") || strings.HasPrefix(name, "ip6_") {
			continue
		}

		// Skip bare physical ID columns (az_id/region_id/auto_service_id/...).
		// The cloud lists only virtual tags here — these would show up as
		// metrics the cloud doesn't have. (_id_0/_id_1 are handled above.)
		if strings.HasSuffix(name, "_id") {
			continue
		}

		// Skip bare ip4/ip6 columns (flow_metrics tables carry unsuffixed
		// copies; the cloud does not list them as metrics).
		if name == "ip4" || name == "ip6" {
			continue
		}

		// direction_score/log_count/session_length are flow_log-only in the
		// cloud contract, but flow_metrics tables carry a direction_score
		// physical column — exclude it outside flow_log.
		if database != "flow_log" && !strings.HasPrefix(table, "flow_log") && name == "direction_score" {
			continue
		}

		// For naming convention: if column ends with _id_0 or _id_1, generate tag variant.
		// E.g., auto_instance_id_0 → auto_instance_0 (客户端 实例).
		if strings.HasSuffix(name, "_id_0") || strings.HasSuffix(name, "_id_1") {
			tagName := strings.Replace(name, "_id_", "_", 1)
			side := "客户端"
			if strings.HasSuffix(name, "_id_1") {
				side = "服务端"
			}
			base := strings.TrimSuffix(tagName, "_0")
			base = strings.TrimSuffix(base, "_1")
			tagDisplay := side + " " + displayFromName(base)
			addMetric(tagName, tagDisplay, "", false, 6, "Tag", "")
			continue
		}

		mt, category, isAgg, unit := classifyColumn(name, c.Type)
		display := c.Comment
		if display == "" {
			display = displayFromName(name)
		}

		addMetric(name, display, unit, isAgg, mt, category, c.Comment)
	}

	// Add computed/core metrics that are not actual columns.
	// flowLogOnly: the cloud only reports these for flow_log tables.
	coreMetrics := []struct {
		Name       string
		Display    string
		Category   string
		MetricType int
		Desc       string
		FlowLog    bool
	}{
		{"request", "请求", "Throughput", 1, "请求总数", false},
		{"response", "响应", "Throughput", 1, "响应总数", false},
		{"error", "异常", "Error", 1, "客户端异常 + 服务端异常", false},
		{"client_error", "客户端异常", "Error", 1, "客户端异常数", false},
		{"server_error", "服务端异常", "Error", 1, "服务端异常数", false},
		{"timeout", "超时", "Error", 1, "超时数", false},
		{"error_ratio", "异常比例", "Error", 4, "异常 / 响应", false},
		{"client_error_ratio", "客户端异常比例", "Error", 4, "客户端异常 / 响应", false},
		{"server_error_ratio", "服务端异常比例", "Error", 4, "服务端异常 / 响应", false},
		{"timeout_ratio", "超时比例", "Error", 4, "超时 / 请求", false},
		{"response_ratio", "响应比例", "Throughput", 4, "响应 / 请求", false},
		{"success_ratio", "正常比例", "Throughput", 4, "1 - 异常 / 响应", false},
		{"row", "行数", "Other", 8, "数据行数", false},
		{"direction_score", "方向得分", "Throughput", 9, "", true},
		{"log_count", "日志总量", "Throughput", 1, "日志总量", true},
		{"session_length", "会话长度", "Throughput", 1, "请求长度 + 响应长度", true},
	}
	isFlowLogTable := database == "flow_log" || strings.HasPrefix(table, "flow_log")
	for _, m := range coreMetrics {
		if m.FlowLog && !isFlowLogTable {
			continue
		}
		addMetric(m.Name, m.Display, "", true, m.MetricType, m.Category, m.Desc)
	}

	logging.Debugf("ShowMetrics: queryShowMetricsCH called for db=%s tbl=%s", database, table)
	// Add virtual metrics (computed by ZT, not directly in CH).
	virtualMetrics := []struct {
		Name    string
		Display string
		Desc    string
		MType   int
	}{
		{"rrt", "平均时延", "采集周期内所有应用时延的平均值", 3},
		{"rrt_max", "最大时延", "采集周期内所有应用时延的最大值", 3},
	}
	for _, vm := range virtualMetrics {
		logging.Debugf("ShowMetrics: adding virtual metric %s", vm.Name)
		addMetric(vm.Name, vm.Display, "us", true, vm.MType, "Delay", vm.Desc)
	}

	// Add virtual/computed tags that aren't physical columns but are used by Topo/Top queries.
	// (is_internet is intentionally absent — the cloud ShowMetrics does not list it.)
	virtualTags := []struct {
		Name    string
		Display string
		Desc    string
	}{
		{"role", "角色", "客户端/服务端角色"},
	}
	for _, vt := range virtualTags {
		addMetric(vt.Name, vt.Display, "", false, 6, "Tag", vt.Desc)
	}

	if len(metrics) == 0 {
		return nil
	}
	return metrics
}

// fallbackShowMetrics returns a minimal hardcoded metric list when CH is down.
func FallbackShowMetrics(table string) []interface{} {
	base := []map[string]interface{}{
		{"name": "request", "is_agg": true, "display_name": "请求", "display_name_zh": "请求", "display_name_en": "Request", "unit": "个", "unit_zh": "个", "unit_en": "", "type": 1, "category": "Throughput", "operators": []string{">=", "<=", ">", "<", "="}, "permissions": []bool{true, true, true}, "table": table, "description": "", "description_zh": "", "description_en": ""},
		{"name": "response", "is_agg": true, "display_name": "响应", "display_name_zh": "响应", "display_name_en": "Response", "unit": "个", "unit_zh": "个", "unit_en": "", "type": 1, "category": "Throughput", "operators": []string{">=", "<=", ">", "<", "="}, "permissions": []bool{true, true, true}, "table": table, "description": "", "description_zh": "", "description_en": ""},
		{"name": "response_duration", "is_agg": false, "display_name": "响应时延", "display_name_zh": "响应时延", "display_name_en": "Response Delay", "unit": "us", "unit_zh": "us", "unit_en": "us", "type": 3, "category": "Delay", "operators": []string{">=", "<=", ">", "<", "="}, "permissions": []bool{true, true, true}, "table": table, "description": "", "description_zh": "", "description_en": ""},
		{"name": "error", "is_agg": true, "display_name": "异常", "display_name_zh": "异常", "display_name_en": "Error", "unit": "个", "unit_zh": "个", "unit_en": "", "type": 1, "category": "Error", "operators": []string{">=", "<=", ">", "<", "="}, "permissions": []bool{true, true, true}, "table": table, "description": "", "description_zh": "", "description_en": ""},
		{"name": "error_ratio", "is_agg": true, "display_name": "异常比例", "display_name_zh": "异常比例", "display_name_en": "Error Ratio", "unit": "%", "unit_zh": "%", "unit_en": "%", "type": 4, "category": "Error", "operators": []string{">=", "<=", ">", "<", "="}, "permissions": []bool{true, true, true}, "table": table, "description": "", "description_zh": "", "description_en": ""},
	}
	result := make([]interface{}, len(base))
	for i, b := range base {
		result[i] = b
	}
	return result
}
