package clickhouse

import (
	"fmt"
	"strings"
	"time"
)

// SelectItem represents one column in a SELECT clause.
type SelectItem struct {
	Expr string
	Key  string
}

// QuerierRequest mirrors the JSON structure of querier API requests.
type QuerierRequest struct {
	Database   string       `json:"DATABASE"`
	Table      string       `json:"TABLE"`
	PageIndex  int          `json:"PAGE_INDEX"`
	PageSize   int          `json:"PAGE_SIZE"`
	Sort       *QuerierSort `json:"SORT"`
	Queries    []QuerierSub `json:"QUERIES"`
	TimeStart  int64        `json:"time_start"`
	TimeEnd    int64        `json:"time_end"`
	Regions    []string     `json:"REGIONS"`
	Total      bool         `json:"TOTAL"`
	DataSource string       `json:"DATA_SOURCE"`
	Interval   int          `json:"interval"`
	Fill       string       `json:"fill"`
}

// QuerierSort holds sort parameters.
type QuerierSort struct {
	OrderBy  string `json:"ORDER_BY"`
	SortedBy string `json:"SORTED_BY"`
}

// QuerierSub represents one sub-query.
type QuerierSub struct {
	QueryID string   `json:"QUERY_ID"`
	Select  string   `json:"SELECT"`
	Where   string   `json:"WHERE"`
	Having  string   `json:"HAVING"`
	Tags    []string `json:"TAGS"`
	CTags   []string `json:"CTAGS"`
	STags   []string `json:"STAGS"`
	Metrics []string `json:"METRICS"`
	GroupBy string   `json:"GROUP_BY"`
}

// ---------------------------------------------------------------------------
// SELECT parser
// ---------------------------------------------------------------------------

// ParseSelectList splits a SELECT expression into items, respecting parentheses.
func ParseSelectList(sel string) []SelectItem {
	var items []SelectItem
	depth := 0
	start := 0
	for i, ch := range sel {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(sel[start:i])
				if part != "" {
					items = append(items, parseOneSelect(part))
				}
				start = i + 1
			}
		}
	}
	if start < len(sel) {
		part := strings.TrimSpace(sel[start:])
		if part != "" {
			items = append(items, parseOneSelect(part))
		}
	}
	return items
}

func parseOneSelect(part string) SelectItem {
	expr, key := part, part
	upper := strings.ToUpper(part)
	asIdx := strings.LastIndex(upper, " AS ")
	if asIdx >= 0 {
		expr = strings.TrimSpace(part[:asIdx])
		key = strings.TrimSpace(part[asIdx+4:])
		key = strings.Trim(key, "`")
	}
	if key == "" {
		key = expr
	}
	return SelectItem{Expr: expr, Key: key}
}

// IsAggExpr reports whether the expression is an aggregate function.
func IsAggExpr(expr string) bool {
	lower := strings.ToLower(expr)
	return strings.Contains(lower, "count(") ||
		strings.Contains(lower, "avg(") ||
		strings.Contains(lower, "sum(") ||
		strings.Contains(lower, "max(") ||
		strings.Contains(lower, "min(") ||
		strings.Contains(lower, "persecond(") ||
		strings.Contains(lower, "uniq(")
}

// NormalizeExpr converts a querier expression to ClickHouse SQL.

// normalizeHavingExpr normalizes aggregate expressions in HAVING clause.
func normalizeHavingExpr(having string) string {
	result := having
	havingExprs := map[string]string{
		"Avg(`server_error_ratio`)": "avg(nullif(server_error, 0) / nullif(request, 0))",
		"Avg(server_error_ratio)":   "avg(nullif(server_error, 0) / nullif(request, 0))",
		"Sum(server_error_ratio)":   "sum(nullif(server_error, 0) / nullif(request, 0))",
		"Avg(`client_error_ratio`)": "avg(nullif(client_error, 0) / nullif(request, 0))",
		"Avg(client_error_ratio)":   "avg(nullif(client_error, 0) / nullif(request, 0))",
		"Avg(`error_ratio`)":        "avg(nullif(error, 0) / nullif(request, 0))",
		"Avg(error_ratio)":          "avg(nullif(error, 0) / nullif(request, 0))",
		"Avg(`rrt`)":                "avg(rrt_sum / nullif(rrt_count, 0))",
		"Avg(rrt)":                  "avg(rrt_sum / nullif(rrt_count, 0))",
	}
	// Case-insensitive replacement
	lowerResult := strings.ToLower(result)
	for expr, chExpr := range havingExprs {
		lowExpr := strings.ToLower(expr)
		if strings.Contains(lowerResult, lowExpr) {
			result = strings.ReplaceAll(result, expr, chExpr)
			lowerResult = strings.ToLower(result)
		}
	}
	return result
}

// replaceDSLFunc replaces DeepFlow DSL function calls in expr with ClickHouse equivalents.
// Handles: PerSecond(...), node_type(...), icon_id(...), Enum(...), newTag(...)
func replaceDSLFunc(expr string) string {
	lower := strings.ToLower(expr)

	// PerSecond(x) → (normalized_x)/60.0
	if idx := strings.Index(lower, "persecond("); idx >= 0 {
		// Find the matching closing paren.
		argStart := idx + len("persecond(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			arg := expr[argStart:argEnd]
			prefix := expr[:idx]
			suffix := expr[argEnd+1:]
			return prefix + "(" + NormalizeExpr(arg) + ")/60.0" + suffix
		}
	}
	// node_type(x) → x (passthrough)
	if idx := strings.Index(lower, "node_type("); idx >= 0 {
		argStart := idx + len("node_type(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			return expr[:idx] + strings.TrimSpace(expr[argStart:argEnd]) + expr[argEnd+1:]
		}
	}
	// icon_id(x) → toInt64(0)
	if idx := strings.Index(lower, "icon_id("); idx >= 0 {
		argStart := idx + len("icon_id(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			return expr[:idx] + "toInt64(0)" + expr[argEnd+1:]
		}
	}
	// Enum(x) → x
	if idx := strings.Index(lower, "enum("); idx >= 0 {
		argStart := idx + len("enum(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			return expr[:idx] + strings.TrimSpace(expr[argStart:argEnd]) + expr[argEnd+1:]
		}
	}
	// newTag(x) → ''
	if idx := strings.Index(lower, "newtag("); idx >= 0 {
		argStart := idx + len("newtag(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			return expr[:idx] + "''" + expr[argEnd+1:]
		}
	}
	return expr
}

// findMatchingParen returns the index of the ')' that matches the '(' at openIdx.
func findMatchingParen(s string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func NormalizeExpr(expr string) string {
	// Strip outer parens so expressions like (x*8) get re-wrapped after normalization.
	outer := strings.TrimSpace(expr)
	if strings.HasPrefix(outer, "(") && strings.HasSuffix(outer, ")") {
		argEnd := findMatchingParen(outer, 0)
		if argEnd == len(outer)-1 {
			// Entire expression is wrapped.
			inner := strings.TrimSpace(outer[1:argEnd])
			normalized := NormalizeExpr(inner)
			return "(" + normalized + ")"
		}
	}

	// Replace known DSL functions in the expression.
	expr = replaceDSLFunc(expr)

	lower := strings.ToLower(expr)
	if lower == "count(`row`)" || lower == "count(row)" {
		return "count(*)"
	}
	if lower == "count(`row`)" || lower == "count(row)" {
		return "count(*)"
	}
	// Metric name mapping: DeepFlow DSL → ClickHouse expressions.
	noBacktick := strings.ReplaceAll(expr, "`", "")
	noBacktickLower := strings.ToLower(noBacktick)
	// Map common DeepFlow metric names to actual ClickHouse column expressions.
	metricMaps := map[string]string{
		"avg(rrt)":                "avg(rrt_sum / nullif(rrt_count, 0))",
		"sum(rrt)":                "sum(rrt_sum / nullif(rrt_count, 0))",
		"max(rrt)":                "max(rrt_sum / nullif(rrt_count, 0))",
		"min(rrt)":                "min(rrt_sum / nullif(rrt_count, 0))",
		"avg(response_delay)":     "avg(response_duration)",
		"sum(response_delay)":     "sum(response_duration)",
		"avg(response_duration)":  "avg(response_duration)",
		"avg(server_error_ratio)": "avg(nullif(server_error, 0) / nullif(request, 0))",
		"sum(server_error_ratio)": "sum(nullif(server_error, 0) / nullif(request, 0))",
		"avg(client_error_ratio)": "avg(nullif(client_error, 0) / nullif(request, 0))",
		"avg(error_ratio)":        "avg(nullif(error, 0) / nullif(request, 0))",
		"avg(request)":            "avg(request)",
		// flow_metrics.network column mappings (DSL names → actual CH columns).
		"avg(retrans_ratio)":  "avg(retrans_tx)",
		"avg(zero_win_ratio)": "avg(zero_win_tx)",
	}
	if mapped, ok := metricMaps[noBacktickLower]; ok {
		return mapped
	}
	return noBacktick
}

// ---------------------------------------------------------------------------
// SQL builder for querier List requests
// ---------------------------------------------------------------------------

// BuildSelectSQL converts a querier List request into a ClickHouse SQL string.

func getFlowLogExpr(sqlExpr, originalExpr string) string {
	if !strings.Contains(strings.ToLower(sqlExpr), "count(") {
		cleanExpr := strings.ToLower(strings.ReplaceAll(originalExpr, "`", ""))
		existsInFlowLog := strings.Contains(cleanExpr, "response_duration") ||
			strings.Contains(cleanExpr, "response_code") ||
			strings.Contains(cleanExpr, "response_status") ||
			strings.Contains(cleanExpr, "request_type") ||
			strings.Contains(cleanExpr, "request_domain") ||
			strings.Contains(cleanExpr, "request_resource") ||
			strings.Contains(cleanExpr, "response_exception") ||
			strings.Contains(cleanExpr, "response_result") ||
			strings.Contains(cleanExpr, "request_length") ||
			strings.Contains(cleanExpr, "captured_request_byte") ||
			strings.Contains(cleanExpr, "syscall_trace_id") ||
			strings.Contains(cleanExpr, "signal_source") ||
			strings.Contains(cleanExpr, "l7_protocol") ||
			strings.Contains(cleanExpr, "endpoint") ||
			strings.Contains(cleanExpr, "x_request_id")
		if !existsInFlowLog {
			return "count(*)"
		}
	}
	return sqlExpr
}

func BuildSelectSQL(req QuerierRequest) (string, error) {
	if len(req.Queries) == 0 {
		return "", fmt.Errorf("no queries in request")
	}
	q := req.Queries[0]
	table := req.Table
	if table == "" {
		table = "l7_flow_log"
	}

	resolvedTable := table
	resolvedDB := req.Database
	if resolvedDB == "flow_metrics" && !strings.Contains(table, ".") {
		if req.DataSource != "" {
			resolvedTable = table + "." + req.DataSource
		} else {
			resolvedTable = table + ".1m"
		}
	}

	// Use _local table for flow_log to bypass broken Distributed table.
	if resolvedDB == "flow_log" && !strings.Contains(resolvedTable, "_local") {
		resolvedTable += "_local"
	}
	if resolvedDB == "flow_log" && !strings.Contains(resolvedTable, "_local") {
		resolvedTable += "_local"
	}
	fullTable := ""
	if resolvedDB != "" {
		fullTable = "`" + resolvedDB + "`.`" + resolvedTable + "`"
	} else {
		fullTable = "`" + resolvedTable + "`"
	}

	items := ParseSelectList(q.Select)
	var selectParts []string
	var groupByParts []string
	var constParts []string

	gbSet := map[string]bool{}
	for _, t := range q.Tags {
		upper := strings.ToUpper(t)
		if idx := strings.LastIndex(upper, " AS "); idx >= 0 {
			key := strings.Trim(strings.TrimSpace(t[idx+4:]), "`")
			gbSet[key] = true
		} else {
			gbSet[strings.Trim(t, "`")] = true
		}
	}
	if q.GroupBy != "" {
		for _, part := range strings.Split(q.GroupBy, ",") {
			part = strings.TrimSpace(strings.ReplaceAll(part, "`", ""))
			if part != "" {
				gbSet[part] = true
			}
		}
	}

	// Column name mapping for DeepFlow DSL → ClickHouse column names.
	// Tables from flow_metrics use app_service (string) while flow_log tables
	// use auto_service_id_0/1 (UInt) for per-side IDs.
	isFlowMetrics := resolvedDB == "flow_metrics"
	isFlowLog := resolvedDB == "flow_log"
	colMap := map[string]string{
		"auto_service":  "app_service",  // application.* tables
		"auto_instance": "app_instance", // application.* tables
		"service_id_0":  "auto_service_id_0",
		"service_id_1":  "auto_service_id_1",
		"instance_id_0": "auto_instance_id_0",
		"instance_id_1": "auto_instance_id_1",
		// Common resource tag mappings.
		"chost": "l3_device_id", "chost_id": "l3_device_id",
		"vpc": "epc_id", "vpc_id": "epc_id",
		"pod_service": "pod_service_id", "pod_service_id": "pod_service_id",
		"pod_group": "pod_group_id", "pod_group_id": "pod_group_id",
		"pod_cluster": "pod_cluster_id", "pod_cluster_id": "pod_cluster_id",
		"pod_ns": "pod_ns_id", "pod_ns_id": "pod_ns_id",
		"region_0": "region_id", "region_1": "region_id",
		"az_0": "az_id", "az_1": "az_id",
		"subnet_0": "subnet_id", "subnet_1": "subnet_id",
		"router_0": "router_id", "router_1": "router_id",
		"lb_0": "lb_id", "lb_1": "lb_id",
		"pod_node_0": "pod_node_id", "pod_node_1": "pod_node_id",
		"service_0": "biz_service_id", "service_1": "biz_service_id",
		"gprocess_0": "gprocess_id_0", "gprocess_1": "gprocess_id_1",
	}
	// For flow_log tables, map _0/_1 suffixed tag names to resolved names or per-side ID columns.
	if isFlowLog {
		colMap["auto_service_0"] = "if(auto_service_type_0 IN (0, 255), if(is_ipv4 = 1, IPv4NumToString(ip4_0), IPv6NumToString(ip6_0)), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_0), toUInt64(auto_service_id_0)), ''))"
		colMap["auto_service_1"] = "if(auto_service_type_1 IN (0, 255), if(is_ipv4 = 1, IPv4NumToString(ip4_1), IPv6NumToString(ip6_1)), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_1), toUInt64(auto_service_id_1)), ''))"
		colMap["auto_instance_0"] = "if(auto_instance_type_0 IN (0, 255), if(is_ipv4 = 1, IPv4NumToString(ip4_0), IPv6NumToString(ip6_0)), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_instance_type_0), toUInt64(auto_instance_id_0)), toString(auto_instance_id_0)))"
		colMap["auto_instance_1"] = "if(auto_instance_type_1 IN (0, 255), if(is_ipv4 = 1, IPv4NumToString(ip4_1), IPv6NumToString(ip6_1)), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_instance_type_1), toUInt64(auto_instance_id_1)), toString(auto_instance_id_1)))"
		colMap["app_service"] = "app_service"
		// computed virtual columns
		colMap["is_internet_0"] = "if(is_ipv4 = 1 AND (startsWith(IPv4NumToString(ip4_0), '10.') OR startsWith(IPv4NumToString(ip4_0), '172.1') OR startsWith(IPv4NumToString(ip4_0), '172.2') OR startsWith(IPv4NumToString(ip4_0), '172.3') OR startsWith(IPv4NumToString(ip4_0), '192.168.') OR startsWith(IPv4NumToString(ip4_0), '127.') OR startsWith(IPv4NumToString(ip4_0), '100.6') OR startsWith(IPv4NumToString(ip4_0), '100.7') OR startsWith(IPv4NumToString(ip4_0), '100.8') OR startsWith(IPv4NumToString(ip4_0), '100.9') OR startsWith(IPv4NumToString(ip4_0), '100.10') OR startsWith(IPv4NumToString(ip4_0), '100.11') OR startsWith(IPv4NumToString(ip4_0), '100.12')), 0, 1)"
colMap["is_internet_1"] = "if(is_ipv4 = 1 AND (startsWith(IPv4NumToString(ip4_1), '10.') OR startsWith(IPv4NumToString(ip4_1), '172.1') OR startsWith(IPv4NumToString(ip4_1), '172.2') OR startsWith(IPv4NumToString(ip4_1), '172.3') OR startsWith(IPv4NumToString(ip4_1), '192.168.') OR startsWith(IPv4NumToString(ip4_1), '127.') OR startsWith(IPv4NumToString(ip4_1), '100.6') OR startsWith(IPv4NumToString(ip4_1), '100.7') OR startsWith(IPv4NumToString(ip4_1), '100.8') OR startsWith(IPv4NumToString(ip4_1), '100.9') OR startsWith(IPv4NumToString(ip4_1), '100.10') OR startsWith(IPv4NumToString(ip4_1), '100.11') OR startsWith(IPv4NumToString(ip4_1), '100.12')), 0, 1)"
colMap["role"] = "0"
		colMap["k8s.label_0"] = "dictGetOrDefault('flow_tag.pod_k8s_labels_map', 'labels', toUInt64(pod_id_0), '')"
		colMap["k8s.label_1"] = "dictGetOrDefault('flow_tag.pod_k8s_labels_map', 'labels', toUInt64(pod_id_1), '')"
		colMap["k8s.annotation_0"] = "dictGetOrDefault('flow_tag.pod_service_k8s_annotations_map', 'annotations', toUInt64(pod_service_id_0), '')"
		colMap["k8s.annotation_1"] = "dictGetOrDefault('flow_tag.pod_service_k8s_annotations_map', 'annotations', toUInt64(pod_service_id_1), '')"
		colMap["k8s.env_0"] = "dictGetOrDefault('flow_tag.pod_k8s_envs_map', 'envs', toUInt64(pod_id_0), '')"
		colMap["k8s.env_1"] = "dictGetOrDefault('flow_tag.pod_k8s_envs_map', 'envs', toUInt64(pod_id_1), '')"
		colMap["cloud.tag_0"] = "dictGetOrDefault('flow_tag.chost_cloud_tags_map', 'cloud_tags', toUInt64(l3_device_id_0), '')"
		colMap["cloud.tag_1"] = "dictGetOrDefault('flow_tag.chost_cloud_tags_map', 'cloud_tags', toUInt64(l3_device_id_1), '')"
		colMap["os.app_0"] = "dictGetOrDefault('flow_tag.os_app_tags_map', 'os_app_tags', toUInt64(gprocess_id_0), '')"
		colMap["os.app_1"] = "dictGetOrDefault('flow_tag.os_app_tags_map', 'os_app_tags', toUInt64(gprocess_id_1), '')"
		colMap["process_0"] = "process_id_0"
		colMap["process_1"] = "process_id_1"
		colMap["x_request_0"] = "x_request_id_0"
		colMap["x_request_1"] = "x_request_id_1"
		// flow_log per-side _id columns override the shared flow_metrics mappings.
		colMap["region_0"] = "region_id_0"
		colMap["region_1"] = "region_id_1"
		colMap["az_0"] = "az_id_0"
		colMap["az_1"] = "az_id_1"
		colMap["chost_0"] = "l3_device_id_0"
		colMap["chost_1"] = "l3_device_id_1"
		colMap["vpc_0"] = "epc_id_0"
		colMap["vpc_1"] = "epc_id_1"
		colMap["subnet_0"] = "subnet_id_0"
		colMap["subnet_1"] = "subnet_id_1"
		colMap["router_0"] = "router_id_0"
		colMap["router_1"] = "router_id_1"
		colMap["lb_0"] = "lb_id_0"
		colMap["lb_1"] = "lb_id_1"
		colMap["pod_node_0"] = "pod_node_id_0"
		colMap["pod_node_1"] = "pod_node_id_1"
		colMap["pod_0"] = "pod_id_0"
		colMap["pod_1"] = "pod_id_1"
		colMap["pod_ns_0"] = "pod_ns_id_0"
		colMap["pod_ns_1"] = "pod_ns_id_1"
		colMap["pod_cluster_0"] = "pod_cluster_id_0"
		colMap["pod_cluster_1"] = "pod_cluster_id_1"
		colMap["pod_service_0"] = "pod_service_id_0"
		colMap["pod_service_1"] = "pod_service_id_1"
		colMap["pod_group_0"] = "pod_group_id_0"
		colMap["pod_group_1"] = "pod_group_id_1"
		colMap["pod_node_0"] = "pod_node_id_0"
		colMap["pod_node_1"] = "pod_node_id_1"
		colMap["tap_port"] = "tap_port"
		colMap["vtap"] = "vtap_id"
		colMap["agent"] = "agent_id"
		colMap["service_0"] = "service_id_0"
		colMap["service_1"] = "service_id_1"
		colMap["gprocess_0"] = "gprocess_id_0"
		colMap["gprocess_1"] = "gprocess_id_1"
		colMap["service_1"] = "dictGetOrDefault('flow_tag.biz_service_map', 'name', toUInt64(biz_service_id_1), '')"
		colMap["service_0"] = "dictGetOrDefault('flow_tag.biz_service_map', 'name', toUInt64(biz_service_id_0), '')"
		colMap["pod_node_1"] = "dictGetOrDefault('flow_tag.pod_node_map', 'name', toUInt64(pod_node_id_1), '')"
		colMap["pod_node_0"] = "dictGetOrDefault('flow_tag.pod_node_map', 'name', toUInt64(pod_node_id_0), '')"
		colMap["pod_group_1"] = "dictGetOrDefault('flow_tag.pod_group_map', 'name', toUInt64(pod_group_id_1), '')"
		colMap["pod_group_0"] = "dictGetOrDefault('flow_tag.pod_group_map', 'name', toUInt64(pod_group_id_0), '')"
		colMap["pod_service_1"] = "dictGetOrDefault('flow_tag.pod_service_map', 'name', toUInt64(pod_service_id_1), '')"
		colMap["pod_service_0"] = "dictGetOrDefault('flow_tag.pod_service_map', 'name', toUInt64(pod_service_id_0), '')"
		colMap["pod_cluster_1"] = "dictGetOrDefault('flow_tag.pod_cluster_map', 'name', toUInt64(pod_cluster_id_1), '')"
		colMap["pod_cluster_0"] = "dictGetOrDefault('flow_tag.pod_cluster_map', 'name', toUInt64(pod_cluster_id_0), '')"
		colMap["pod_ns_1"] = "dictGetOrDefault('flow_tag.pod_ns_map', 'name', toUInt64(pod_ns_id_1), '')"
		colMap["pod_ns_0"] = "dictGetOrDefault('flow_tag.pod_ns_map', 'name', toUInt64(pod_ns_id_0), '')"
		colMap["vpc_1"] = "dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(epc_id_1), '')"
		colMap["vpc_0"] = "dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(epc_id_0), '')"
		colMap["chost_1"] = "dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id_1), '')"
		colMap["chost_0"] = "dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id_0), '')"
		colMap["az_1"] = "dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id_1), '')"
		colMap["az_0"] = "dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id_0), '')"
		colMap["region_1"] = "dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id_1), '')"
		colMap["region_0"] = "dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id_0), '')"
	}
	if isFlowMetrics {
		colMap["auto_service_0"] = "if(auto_service_type_0 IN (0, 255), if(is_ipv4 = 1, IPv4NumToString(ip4_0), IPv6NumToString(ip6_0)), ifNull(dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_0), toUInt64(auto_service_id_0)), ''), app_service))"
		colMap["auto_service_1"] = "if(auto_service_type_1 IN (0, 255), if(is_ipv4 = 1, IPv4NumToString(ip4_1), IPv6NumToString(ip6_1)), ifNull(dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_1), toUInt64(auto_service_id_1)), ''), app_service))"
	}

	for _, item := range items {
		lower := strings.ToLower(item.Expr)
		expr := NormalizeExpr(item.Expr)
		isAgg := IsAggExpr(item.Expr)
		_, isTag := gbSet[item.Key]
		isFunc := strings.HasPrefix(lower, "node_type(") ||
			strings.HasPrefix(lower, "icon_id(") ||
			strings.HasPrefix(lower, "enum(") ||
			strings.HasPrefix(lower, "newtag(")

		switch {
		case strings.HasPrefix(lower, "newtag("):
			tagVal := item.Expr[len("newTag(") : len(item.Expr)-1]
			tagVal = strings.Trim(tagVal, "'\"")
			constParts = append(constParts, fmt.Sprintf("'%s' AS `%s`", tagVal, item.Key))

		case isTag || (!isAgg && !isFunc):
			col := strings.Trim(item.Expr, "`")
			// Check for node_type, icon_id, enum function calls disguised as tags.
			if strings.HasPrefix(lower, "node_type(") {
				inner := strings.TrimSpace(item.Expr[len("node_type(") : len(item.Expr)-1])
				inner = strings.Trim(inner, "\"'")
				selectParts = append(selectParts, fmt.Sprintf("'%s' AS `%s`", inner, item.Key))
			} else if strings.HasPrefix(lower, "icon_id(") {
				selectParts = append(selectParts, fmt.Sprintf("toInt64(-13) AS `%s`", item.Key))
			} else if strings.HasPrefix(lower, "enum(") {
				inner := strings.TrimSpace(item.Expr[len("enum(") : len(item.Expr)-1])
				inner = strings.Trim(inner, "\"'")
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", inner, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", inner))
			} else if _, err := fmt.Sscanf(col, "%f", new(float64)); err == nil {
				// Numeric literal (e.g., -42)
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", col, item.Key))
			} else if mapped, ok := colMap[col]; ok {
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", mapped, item.Key))
				groupByParts = append(groupByParts, mapped)
			} else if col == "is_internet_0" || col == "is_internet_1" {
				selectParts = append(selectParts, fmt.Sprintf("0 AS `%s`", col))
			} else {
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", col))
			}
		case isAgg:
			sqlExpr := expr
			if isFlowLog {
				sqlExpr = getFlowLogExpr(sqlExpr, item.Expr)
			}
			selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", sqlExpr, item.Key))

		case strings.HasPrefix(lower, "node_type("):
			inner := strings.TrimSpace(item.Expr[len("node_type(") : len(item.Expr)-1])
			selectParts = append(selectParts, fmt.Sprintf("toString(`%s`) AS `%s`", inner, item.Key))
			groupByParts = append(groupByParts, fmt.Sprintf("`%s`", inner))

		case strings.HasPrefix(lower, "icon_id("):
			selectParts = append(selectParts, fmt.Sprintf("toInt64(-13) AS `%s`", item.Key))

		case strings.HasPrefix(lower, "enum("):
			inner := strings.TrimSpace(item.Expr[len("enum(") : len(item.Expr)-1])
			selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", inner, item.Key))
			groupByParts = append(groupByParts, fmt.Sprintf("`%s`", inner))

		default:
			col := strings.Trim(item.Expr, "`")
			if mapped, ok := colMap[col]; ok {
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", mapped, item.Key))
				groupByParts = append(groupByParts, mapped)
			} else {
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", col))
			}
		}
	}

	// Auto-add required columns that the cloud backend always returns.
	// These are expected by the frontend even when not in SELECT/TAGS.
	if resolvedDB == "flow_metrics" {
		// auto_service_type_0/1 — needed for node_type/icon_id resolution
		for _, suffix := range []string{"_0", "_1"} {
			colName := "auto_service_type" + suffix
			already := false
			for _, sp := range selectParts {
				if strings.Contains(sp, "`"+colName+"`") {
					already = true
					break
				}
			}
			if !already && gbSet["auto_service"+suffix] {
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", colName, colName))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", colName))
			}
		}
		// resource_l7_protocol_0/1 — protocol label for topology
		for _, suffix := range []string{"_0", "_1"} {
			colName := "resource_l7_protocol" + suffix
			already := false
			for _, sp := range selectParts {
				if strings.Contains(sp, "`"+colName+"`") {
					already = true
					break
				}
			}
			if !already && gbSet["auto_service_0"] {
				selectParts = append(selectParts, fmt.Sprintf("`l7_protocol` AS `%s`", colName))
				groupByParts = append(groupByParts, "`l7_protocol`")
			}
		}
	}

	var sql strings.Builder
	sql.WriteString("SELECT ")
	allSelects := append(constParts, selectParts...)
	sql.WriteString(strings.Join(allSelects, ", "))
	sql.WriteString(fmt.Sprintf(" FROM %s", fullTable))

	var wheres []string
	if req.TimeStart > 0 {
		ts := time.Unix(req.TimeStart, 0).In(time.UTC).Format("2006-01-02 15:04:05")
		wheres = append(wheres, fmt.Sprintf("time >= '%s'", ts))
	}
	if req.TimeEnd > 0 {
		ts := time.Unix(req.TimeEnd, 0).In(time.UTC).Format("2006-01-02 15:04:05")
		wheres = append(wheres, fmt.Sprintf("time <= '%s'", ts))
	}
	if q.Where != "" {
		cleanWhere := cleanWhereClause(q.Where)
		// Map API column names to physical CH column names.
		cleanWhere = strings.ReplaceAll(cleanWhere, "`ip_0`", "`ip4_0`")
		cleanWhere = strings.ReplaceAll(cleanWhere, "`ip_1`", "`ip4_1`")
		cleanWhere = strings.ReplaceAll(cleanWhere, "ip_0", "ip4_0")
		cleanWhere = strings.ReplaceAll(cleanWhere, "ip_1", "ip4_1")
		// Strip is_internet/role conditions (ZT virtual cols, not in CH)
		for _, vcol := range []string{"is_internet_0", "is_internet_1", "role"} {
			for _, pat := range []string{"`" + vcol + "`", vcol} {
				for {
					idx := strings.Index(cleanWhere, pat)
					if idx < 0 {
						break
					}
					// Find the end of this condition
					scan := idx
					for scan < len(cleanWhere) {
						if cleanWhere[scan] == ')' {
							scan++
							break
						}
						if scan > idx && scan+4 < len(cleanWhere) && (cleanWhere[scan:scan+5] == " AND " || cleanWhere[scan:scan+4] == " OR ") {
							break
						}
						scan++
					}
					// Backtrack to the start of this condition (AND/OR/EOL)
					start := idx
					preAnd := strings.LastIndex(cleanWhere[:start], "AND ")
					preOr := strings.LastIndex(cleanWhere[:start], "OR ")
					if preOr > preAnd {
						preAnd = preOr
					}
					if start > 0 && cleanWhere[start-1] == '(' {
						preAnd = strings.LastIndex(cleanWhere[:start-1], "(")
					}
					if preAnd >= 0 {
						start = preAnd
					}
					cleanWhere = cleanWhere[:start] + cleanWhere[scan:]
				}
			}
		}
		cleanWhere = strings.TrimSpace(cleanWhere)
		cleanWhere = strings.TrimPrefix(cleanWhere, "AND ")
		cleanWhere = strings.TrimPrefix(cleanWhere, "OR ")
		if cleanWhere != "" {
			wheres = append(wheres, cleanWhere)
		}
	}
	if len(wheres) > 0 {
		sql.WriteString(" WHERE ")
		sql.WriteString(strings.Join(wheres, " AND "))
	}

	if len(groupByParts) > 0 {
		sql.WriteString(" GROUP BY ")
		sql.WriteString(strings.Join(groupByParts, ", "))
	}

	// HAVING clause — normalize aggregate expressions to CH equivalents.
	if q.Having != "" {
		cleanHaving := cleanWhereClause(q.Having)
		cleanHaving = strings.ReplaceAll(cleanHaving, "`ip_0`", "`ip4_0`")
		cleanHaving = strings.ReplaceAll(cleanHaving, "`ip_1`", "`ip4_1`")
		cleanHaving = strings.ReplaceAll(cleanHaving, "ip_0", "ip4_0")
		cleanHaving = strings.ReplaceAll(cleanHaving, "ip_1", "ip4_1")
		// Normalize aggregate function references in HAVING using NormalizeExpr.
		// This maps Avg(server_error_ratio) → avg(nullif(server_error, 0) / nullif(request, 0)) etc.
		normalized := normalizeHavingExpr(cleanHaving)
		sql.WriteString(" HAVING ")
		sql.WriteString(normalized)
	}

	if req.Sort != nil && req.Sort.OrderBy != "" {
		dir := "ASC"
		if strings.ToUpper(req.Sort.SortedBy) == "DESC" {
			dir = "DESC"
		}
		sql.WriteString(fmt.Sprintf(" ORDER BY `%s` %s", strings.ReplaceAll(req.Sort.OrderBy, "`", ""), dir))
	}

	if req.PageSize > 0 {
		sql.WriteString(fmt.Sprintf(" LIMIT %d", req.PageSize))
		if req.PageIndex > 1 {
			sql.WriteString(fmt.Sprintf(" OFFSET %d", (req.PageIndex-1)*req.PageSize))
		}
	} else {
		sql.WriteString(" LIMIT 500")
	}

	return sql.String(), nil
}

// ---------------------------------------------------------------------------
// Histogram SQL builder
// ---------------------------------------------------------------------------

// HistogramRequest carries parameters for a histogram query.
type HistogramRequest struct {
	Database  string
	Table     string
	TimeStart int64
	TimeEnd   int64
}

// BuildHistogramSQL builds a time-bucketed count query for histogram/flame-graph views.
// Uses 1-minute buckets for short ranges, 1-hour buckets for ranges > 2 hours.
func BuildHistogramSQL(req HistogramRequest) (string, error) {
	db := req.Database
	if db == "" {
		db = "flow_log"
	}
	table := req.Table
	if table == "" {
		table = "l7_flow_log"
	}
	resolvedTable := table
	if db == "flow_metrics" && !strings.Contains(table, ".") {
		resolvedTable = table + ".1m"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)

	interval := "toStartOfMinute(time)"
	if req.TimeEnd-req.TimeStart > 7200 {
		interval = "toStartOfHour(time)"
	}

	var wheres []string
	if req.TimeStart > 0 {
		ts := time.Unix(req.TimeStart, 0).In(time.UTC).Format("2006-01-02 15:04:05")
		wheres = append(wheres, fmt.Sprintf("time >= '%s'", ts))
	}
	if req.TimeEnd > 0 {
		ts := time.Unix(req.TimeEnd, 0).In(time.UTC).Format("2006-01-02 15:04:05")
		wheres = append(wheres, fmt.Sprintf("time <= '%s'", ts))
	}
	whereClause := ""
	if len(wheres) > 0 {
		whereClause = " WHERE " + strings.Join(wheres, " AND ")
	}

	sql := fmt.Sprintf("SELECT toUnixTimestamp(%s) AS time_bucket, count(*) AS count FROM %s%s GROUP BY time_bucket ORDER BY time_bucket LIMIT 1000",
		interval, fullTable, whereClause)
	return sql, nil
}

// ---------------------------------------------------------------------------
// ShowAttributes SQL builder
// ---------------------------------------------------------------------------

// BuildShowAttributesSQL builds a query to fetch span attribute names/values.
func BuildShowAttributesSQL(database, table string) string {
	db := database
	if db == "" {
		db = "flow_log"
	}
	tbl := table
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, tbl)
	return fmt.Sprintf("SELECT attribute_names, attribute_values, _id FROM %s LIMIT 100", fullTable)
}

// cleanWhereClause strips exist() calls from the WHERE clause.
func cleanWhereClause(where string) string {
	for strings.Contains(where, "exist(") {
		start := strings.Index(where, "exist(")
		end := start + 5
		for end < len(where) && where[end] != ')' {
			end++
		}
		if end < len(where) {
			end++
		}
		before := strings.TrimSpace(where[:start])
		after := strings.TrimSpace(where[end:])
		if strings.HasSuffix(before, "AND") {
			before = strings.TrimSpace(before[:len(before)-3])
		}
		if strings.HasSuffix(before, "OR") {
			before = strings.TrimSpace(before[:len(before)-2])
		}
		if strings.HasPrefix(after, "AND") {
			after = strings.TrimSpace(after[3:])
		}
		if strings.HasPrefix(after, "OR") {
			after = strings.TrimSpace(after[2:])
		}
		where = strings.TrimSpace(before + " " + after)
	}
	return where
}
