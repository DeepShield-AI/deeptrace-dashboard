package transport

import (
	"context"
	"deeptrace-backend/query"
	"encoding/json"
	"fmt"
	"os"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RegisterShowMetrics registers the ShowMetrics endpoint.
func RegisterShowMetrics(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/ShowMetrics", handleShowMetrics(deps))
}

func handleShowMetrics(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Parse request.
		var req struct {
			Database string `json:"DATABASE"`
			Table    string `json:"TABLE"`
		}
		db, tbl := "flow_log", "l7_flow_log"
		if json.Unmarshal([]byte(bodyStr), &req) == nil {
			if req.Database != "" {
				db = req.Database
			}
			if req.Table != "" {
				tbl = req.Table
			}
		}

	// ShowMetrics writes helper: include TYPE field.
	writeShowMetrics := func(w http.ResponseWriter, data interface{}) {
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS":  "SUCCESS",
			"DESCRIPTION": "",
			"DATA":        data,
			"TYPE":        "DBDescription",
			"SCHEMAS":     map[string]interface{}{},
		})
	}

		// Collect metrics from multiple sources and merge for best coverage.
		var mergedMetrics []interface{}
		seen := map[string]bool{}

		// 1. Try deepflow-server (ZT) — returns virtual tags and metrics.
		if deps.Querier != nil && deps.Querier.Zerotrace != nil {
			result, err := query.QueryShowMetrics(deps.Querier.Zerotrace, bodyStr)
			if err == nil && result != nil {
				for _, m := range result.Data {
				key := fmt.Sprintf("%s.%s.%s", db, tbl, m["name"])
				if !seen[key] {
					seen[key] = true
					mergedMetrics = append(mergedMetrics, m)
				}
			}
		}
		}

		// 2. Try ClickHouse HTTP for column metadata (fills gaps ZT misses).
		if chMetrics := queryShowMetricsCH(db, tbl); chMetrics != nil {
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
			writeShowMetrics(w, mergedMetrics)
			return
		}

		// 3. Fallback: hardcoded minimal list.
		writeShowMetrics(w, fallbackShowMetrics(tbl))
	}
}

// ---------------------------------------------------------------------------
// ClickHouse HTTP query (port 8123)
// ---------------------------------------------------------------------------

var chHost = getCHHTTPHost()

func getCHHTTPHost() string {
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
func chHTTPQuery(query string) ([]map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reqURL := chHost + "/?query=" + url.QueryEscape(query + " FORMAT JSON")
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
func queryShowMetricsCH(database, table string) []interface{} {
	// Try with table name first; for flow_metrics, try with .1m suffix.
	tableNames := []string{table}
	if database == "flow_metrics" && !strings.Contains(table, ".") {
		tableNames = []string{table + ".1m", table}
	}
	var rows []map[string]interface{}
	var lastErr error
	for _, tn := range tableNames {
		query := fmt.Sprintf("SELECT name, type, comment FROM system.columns WHERE database='%s' AND table='%s' ORDER BY position", database, tn)
		rows, lastErr = chHTTPQuery(query)
		if lastErr == nil && len(rows) > 0 {
			break
		}
	}
	if lastErr != nil {
		log.Printf("⚠️  ShowMetrics CH query failed: %v", lastErr)
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
			Name:    getStr(row, "name"),
			Type:    getStr(row, "type"),
			Comment: getStr(row, "comment"),
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
			"name":             name,
			"is_agg":           isAgg,
			"display_name":     displayName,
			"display_name_zh":  displayName,
			"display_name_en":  displayName,
			"unit":             unit,
			"unit_zh":          unit,
			"unit_en":          unit,
			"type":             typ,
			"category":         category,
			"operators":        []string{">=", "<=", ">", "<", "="},
			"permissions":      []bool{true, true, true},
			"table":            table,
			"description":      description,
			"description_zh":   description,
			"description_en":   description,
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
	coreMetrics := []struct {
		Name       string
		Display    string
		Category   string
		MetricType int
		Desc       string
	}{
		{"request", "请求", "Throughput", 1, "请求总数"},
		{"response", "响应", "Throughput", 1, "响应总数"},
		{"error", "异常", "Error", 1, "客户端异常 + 服务端异常"},
		{"client_error", "客户端异常", "Error", 1, "客户端异常数"},
		{"server_error", "服务端异常", "Error", 1, "服务端异常数"},
		{"timeout", "超时", "Error", 1, "超时数"},
		{"error_ratio", "异常比例", "Error", 4, "异常 / 响应"},
		{"client_error_ratio", "客户端异常比例", "Error", 4, "客户端异常 / 响应"},
		{"server_error_ratio", "服务端异常比例", "Error", 4, "服务端异常 / 响应"},
		{"timeout_ratio", "超时比例", "Error", 4, "超时 / 请求"},
		{"response_ratio", "响应比例", "Throughput", 4, "响应 / 请求"},
		{"success_ratio", "正常比例", "Throughput", 4, "1 - 异常 / 响应"},
		{"row", "行数", "Other", 8, "数据行数"},
		{"direction_score", "方向得分", "Throughput", 9, ""},
		{"log_count", "日志总量", "Throughput", 1, "日志总量"},
		{"session_length", "会话长度", "Throughput", 1, "请求长度 + 响应长度"},
	}
	for _, m := range coreMetrics {
		addMetric(m.Name, m.Display, "", true, m.MetricType, m.Category, m.Desc)
	}

	log.Printf("ShowMetrics: queryShowMetricsCH called for db=%s tbl=%s", database, table)
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
		log.Printf("ShowMetrics: adding virtual metric %s", vm.Name)
		addMetric(vm.Name, vm.Display, "us", true, vm.MType, "Delay", vm.Desc)
	}

	// Add virtual/computed tags that aren't physical columns but are used by Topo/Top queries.
	virtualTags := []struct {
		Name    string
		Display string
		Desc    string
	}{
		{"role", "角色", "客户端/服务端角色"},
		{"is_internet", "网络类型", "内网/公网网络类型"},
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
func fallbackShowMetrics(table string) []interface{} {
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

// getStr safely extracts a string from a map.
func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}
