package top

import (
	"context"

	"deeptrace-backend/query/tracemap"
	"fmt"
	"log"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
)


func QueryTop(ch *clickhouse.CHService, ctx context.Context, req *clickhouse.QuerierRequest) (*clickhouse.QueryTopResult, error) {
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
	// Use _local table for flow_log to bypass broken Distributed table.
	if db == "flow_log" && !strings.Contains(resolvedTable, "_local") {
		resolvedTable += "_local"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)

	if len(req.Queries) == 0 {
		return nil, fmt.Errorf("no queries")
	}
	q := req.Queries[0]
	items := clickhouse.ParseSelectList(q.Select)

	constKeys := map[string]bool{}
	var metricExprs []clickhouse.MetricExpr
	isFlowLog := db == "flow_log"
	isFlowMetrics := db == "flow_metrics"

	for _, item := range items {
		lower := strings.ToLower(item.Expr)

		if strings.HasPrefix(lower, "percentile(") {
			inner := item.Expr[len("Percentile(") : len(item.Expr)-1]
			commaIdx := strings.LastIndex(inner, ",")
			if commaIdx > 0 {
				field := strings.TrimSpace(inner[:commaIdx])
				pct := strings.TrimSpace(inner[commaIdx+1:])
				metricExprs = append(metricExprs, clickhouse.MetricExpr{
					item.Key, fmt.Sprintf("quantile(%s)(`%s`)", pct, strings.ReplaceAll(field, "`", "")),
				})
			}
			continue
		}

		if strings.HasPrefix(lower, "newtag(") ||
			strings.HasPrefix(lower, "icon_id(") ||
			strings.HasPrefix(lower, "node_type(") ||
			strings.HasPrefix(lower, "enum(") {
			constKeys[item.Key] = true
			continue
		}

		if _, err := fmt.Sscanf(item.Expr, "%f", new(float64)); err == nil {
			constKeys[item.Key] = true
			continue
		}

		if clickhouse.IsAggExpr(item.Expr) {
			sqlExpr := clickhouse.NormalizeExpr(item.Expr)

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
			metricExprs = append(metricExprs, clickhouse.MetricExpr{item.Key, sqlExpr})
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
		cleanWhere := clickhouse.CleanWhereClause(q.Where)
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
		metricSelects = append(metricSelects, fmt.Sprintf("%s AS `%s`", m.SQL, m.Key))
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

	rows, err := ch.Query(qCtx, querySQL)
	if err != nil {
		log.Printf("CH query error: %v", err)
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
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
		// Common resource tag _0/_1 mappings (flow_metrics: shared column for both sides).
	}

	// flow_log-specific column mappings for DeepFlow field names.
	flowLogColMap := map[string]string{}
	if isFlowLog {
		flowLogColMap = map[string]string{
			"event_type":    "l7_protocol",
			"auto_instance": "if(empty(app_instance), toString(auto_instance_id_0), app_instance)",
			"event_desc":    "request_resource",
			// auto_service_0/1: resolve name via dictGet(device_map) matching cloud behavior.
			"auto_service_0": "if(auto_service_type_0 IN (0, 255), if(any(is_ipv4) = 1, IPv4NumToString(any(ip4_0)), IPv6NumToString(any(ip6_0))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_0), toUInt64(any(auto_service_id_0))), ''))",
			"auto_service_1": "if(auto_service_type_1 IN (0, 255), if(any(is_ipv4) = 1, IPv4NumToString(any(ip4_1)), IPv6NumToString(any(ip6_1))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_1), toUInt64(any(auto_service_id_1))), ''))",
			// Virtual ZT columns: map to real ClickHouse columns for flow_log.
			"client_node_type": "auto_service_type_0",
			"server_node_type": "auto_service_type_1",
			// flow_log: per-side _id_0/_id_1 columns (override flow_metrics shared columns).
			"region_0": "any(dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id_0), ''))", "region_1": "any(dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id_1), ''))",
			"az_0": "any(dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id_0), ''))", "az_1": "any(dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id_1), ''))",
			"chost_0": "any(dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id_0), ''))", "chost_1": "any(dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id_1), ''))",
			"chost_id_0": "l3_device_id_0", "chost_id_1": "l3_device_id_1",
			"vpc_0": "any(dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(epc_id_0), ''))", "vpc_1": "any(dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(epc_id_1), ''))",
			"vpc_id_0": "epc_id_0", "vpc_id_1": "epc_id_1",
			"subnet_0": "subnet_id_0", "subnet_1": "subnet_id_1",
			"router_0": "router_id_0", "router_1": "router_id_1",
			"l2_vpc_0": "epc_id_0", "l2_vpc_1": "epc_id_1",
			"lb_0": "lb_id_0", "lb_1": "lb_id_1",
			"lb_listener_0": "lb_listener_id_0", "lb_listener_1": "lb_listener_id_1",
			"pod_node_0": "any(dictGetOrDefault('flow_tag.pod_node_map', 'name', toUInt64(pod_node_id_0), ''))", "pod_node_1": "any(dictGetOrDefault('flow_tag.pod_node_map', 'name', toUInt64(pod_node_id_1), ''))",
			"pod_ingress_0": "pod_ingress_id_0", "pod_ingress_1": "pod_ingress_id_1",
			"pod_ns_0": "any(dictGetOrDefault('flow_tag.pod_ns_map', 'name', toUInt64(pod_ns_id_0), ''))", "pod_ns_1": "any(dictGetOrDefault('flow_tag.pod_ns_map', 'name', toUInt64(pod_ns_id_1), ''))",
			"pod_cluster_0": "any(dictGetOrDefault('flow_tag.pod_cluster_map', 'name', toUInt64(pod_cluster_id_0), ''))", "pod_cluster_1": "any(dictGetOrDefault('flow_tag.pod_cluster_map', 'name', toUInt64(pod_cluster_id_1), ''))",
			"pod_service_0": "any(dictGetOrDefault('flow_tag.pod_service_map', 'name', toUInt64(pod_service_id_0), ''))", "pod_service_1": "any(dictGetOrDefault('flow_tag.pod_service_map', 'name', toUInt64(pod_service_id_1), ''))",
			"pod_group_0": "any(dictGetOrDefault('flow_tag.pod_group_map', 'name', toUInt64(pod_group_id_0), ''))", "pod_group_1": "any(dictGetOrDefault('flow_tag.pod_group_map', 'name', toUInt64(pod_group_id_1), ''))",
			"pod_0": "pod_id_0", "pod_1": "pod_id_1",
			"service_0": "any(dictGetOrDefault('flow_tag.biz_service_map', 'name', toUInt64(biz_service_id_0), ''))", "service_1": "any(dictGetOrDefault('flow_tag.biz_service_map', 'name', toUInt64(biz_service_id_1), ''))",
			"gprocess_0": "gprocess_id_0", "gprocess_1": "gprocess_id_1",
			"tap_port": "tap_port", "vtap": "vtap_id", "agent": "agent_id",
			// Computed virtual columns.
			"is_internet_0": "any(if(is_ipv4=1 AND (startsWith(IPv4NumToString(ip4_0),'10.') OR startsWith(IPv4NumToString(ip4_0),'172.1') OR startsWith(IPv4NumToString(ip4_0),'172.2') OR startsWith(IPv4NumToString(ip4_0),'172.3') OR startsWith(IPv4NumToString(ip4_0),'192.168.') OR startsWith(IPv4NumToString(ip4_0),'127.') OR startsWith(IPv4NumToString(ip4_0),'100.6') OR startsWith(IPv4NumToString(ip4_0),'100.7') OR startsWith(IPv4NumToString(ip4_0),'100.8') OR startsWith(IPv4NumToString(ip4_0),'100.9') OR startsWith(IPv4NumToString(ip4_0),'100.10') OR startsWith(IPv4NumToString(ip4_0),'100.11') OR startsWith(IPv4NumToString(ip4_0),'100.12')),0,1))",
			"is_internet_1": "any(if(is_ipv4=1 AND (startsWith(IPv4NumToString(ip4_1),'10.') OR startsWith(IPv4NumToString(ip4_1),'172.1') OR startsWith(IPv4NumToString(ip4_1),'172.2') OR startsWith(IPv4NumToString(ip4_1),'172.3') OR startsWith(IPv4NumToString(ip4_1),'192.168.') OR startsWith(IPv4NumToString(ip4_1),'127.') OR startsWith(IPv4NumToString(ip4_1),'100.6') OR startsWith(IPv4NumToString(ip4_1),'100.7') OR startsWith(IPv4NumToString(ip4_1),'100.8') OR startsWith(IPv4NumToString(ip4_1),'100.9') OR startsWith(IPv4NumToString(ip4_1),'100.10') OR startsWith(IPv4NumToString(ip4_1),'100.11') OR startsWith(IPv4NumToString(ip4_1),'100.12')),0,1))",
			"role": "0",
			"process_0": "process_id_0", "process_1": "process_id_1",
			"x_request_0": "x_request_id_0", "x_request_1": "x_request_id_1",
			"k8s.label_0": "any(dictGetOrDefault('flow_tag.pod_k8s_labels_map', 'labels', toUInt64(pod_id_0), ''))",
			"k8s.label_1": "any(dictGetOrDefault('flow_tag.pod_k8s_labels_map', 'labels', toUInt64(pod_id_1), ''))",
			"cloud.tag_0": "any(dictGetOrDefault('flow_tag.chost_cloud_tags_map', 'cloud_tags', toUInt64(l3_device_id_0), ''))",
			"cloud.tag_1": "any(dictGetOrDefault('flow_tag.chost_cloud_tags_map', 'cloud_tags', toUInt64(l3_device_id_1), ''))",
			"os.app_0": "any(dictGetOrDefault('flow_tag.os_app_tags_map', 'os_app_tags', toUInt64(gprocess_id_0), ''))",
			"os.app_1": "any(dictGetOrDefault('flow_tag.os_app_tags_map', 'os_app_tags', toUInt64(gprocess_id_1), ''))",
		}
	}

	// flow_metrics-specific: _0/_1 both map to same shared column.
	flowMetricsColMap := map[string]string{}
	if isFlowMetrics {
		flowMetricsColMap = map[string]string{
			"auto_service_0":  "app_service",
			"auto_service_1":  "app_service",
			"auto_instance_0": "app_instance",
			"auto_instance_1": "app_instance",
			"chost_0":         "l3_device_id", "chost_1": "l3_device_id",
			"region_0": "region_id", "region_1": "region_id",
			"az_0": "az_id", "az_1": "az_id",
			"subnet_0": "subnet_id", "subnet_1": "subnet_id",
			"vpc_0": "l3_epc_id", "vpc_1": "l3_epc_id",
			"pod_ns_0": "pod_ns_id", "pod_ns_1": "pod_ns_id",
			"pod_cluster_0": "pod_cluster_id", "pod_cluster_1": "pod_cluster_id",
			"pod_service_0": "pod_service_id", "pod_service_1": "pod_service_id",
			"pod_group_0": "pod_group_id", "pod_group_1": "pod_group_id",
			"pod_node_0": "pod_node_id", "pod_node_1": "pod_node_id",
			"service_0": "biz_service_id", "service_1": "biz_service_id",
		}
	}

	// flow_log columns that don't exist in raw ClickHouse: skip from tags/group.
	flowLogSkipCols := map[string]bool{}
	if isFlowLog {
		flowLogSkipCols = map[string]bool{
			"_0": true, "_1": true,
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
		} else if m3, ok3 := flowMetricsColMap[colName]; ok3 {
			mappedCol = m3
		} else if m3, ok3 := flowMetricsColMap[colName]; ok3 {
			mappedCol = m3
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
		// Skip constants and function aliases (newTag('x'), node_type('x'), -42, etc.)
		if constKeys[gb] {
			continue
		}
		mappedCol := gb
		if m, ok := topColMap[gb]; ok {
			mappedCol = m
		} else if m2, ok2 := flowLogColMap[gb]; ok2 {
			mappedCol = m2
		} else if m3, ok3 := flowMetricsColMap[gb]; ok3 {
			mappedCol = m3
		} else if m3, ok3 := flowMetricsColMap[gb]; ok3 {
			mappedCol = m3
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

	if len(tagCols) > 0 && len(groupCols) > 0 {
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
		gRows, gErr := ch.Query(qCtx, groupSQL)
		if gErr == nil {
			defer gRows.Close()
			if gData, gErr2 := clickhouse.ScanRows(gRows); gErr2 == nil {
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
		return &clickhouse.QueryTopResult{Data: []map[string]interface{}{}}, nil
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
			if v, ok := row[m.Key]; ok {
				resultRow[m.Key] = v
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
		histRows, hErr := ch.Query(qCtx, hSQL)
		if hErr == nil {
			if histData, hErr2 := clickhouse.ScanRows(histRows); hErr2 == nil {
				resultRow["HISTORY"] = tracemap.FillNullHistory(tracemap.ConvertHistory(histData, metricExprs), int64(req.Interval), req.TimeStart, req.TimeEnd, req.Fill, metricExprs)
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
		for _, item := range clickhouse.ParseSelectList(req.Queries[0].Select) {
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
	return &clickhouse.QueryTopResult{
		Data:   resultRows,
		Fields: schemas,
	}, nil
}
