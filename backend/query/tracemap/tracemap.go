package tracemap

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
)

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
	name            string
	parentIDs       map[tmKey]bool
	childIDs        map[tmKey]bool
	total           float64
	responseTotal   float64
	durationSum     float64
	successCount    float64
	serverErrCount  float64
	signalSource    float64
	obsPoint        string
	ip              string
	serverEndpoints []string // endpoints this service serves (as server, endpoints_1)
	clientEndpoints []string // endpoints this service calls (as client, endpoints_0)
	gprocessIDs     map[string]interface{}
	epStats         map[string]endpointStat
}

// ---------------------------------------------------------------------------
// QueryTraceMap — returns TraceMap node data from ClickHouse
// ---------------------------------------------------------------------------

func QueryTraceMap(ch *clickhouse.CHService, ctx context.Context, timeStart, timeEnd int64, queryCondition string) (*clickhouse.QueryTraceMapResult, error) {
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
	if rows2, err2 := ch.Query(qCtx, traceCountSQL); err2 == nil {
		if td, e2 := clickhouse.ScanRows(rows2); e2 == nil && len(td) > 0 {
			totalTraceCount = int64(clickhouse.GetF64(td[0], "total_traces"))
			calcTraceCount = int64(clickhouse.GetF64(td[0], "calc_traces"))
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
			dictGet('flow_tag.biz_service_map', 'name', toUInt64(any(auto_service_id_0))) AS auto_service_name_0,
			dictGet('flow_tag.biz_service_map', 'name', toUInt64(any(auto_service_id_1))) AS auto_service_name_1,
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

	rows, err := ch.Query(qCtx, sqlStr)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	allData, err := clickhouse.ScanRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if len(allData) == 0 {
		return &clickhouse.QueryTraceMapResult{}, nil
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

	epRows, epErr := ch.Query(qCtx, endpointSQL)
	var endpointData []map[string]interface{}
	if epErr == nil {
		endpointData, epErr = clickhouse.ScanRows(epRows)
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
			clickhouse.GetF64(epRow, "auto_service_id_0"), clickhouse.GetF64(epRow, "auto_service_type_0"),
			clickhouse.GetF64(epRow, "auto_service_id_1"), clickhouse.GetF64(epRow, "auto_service_type_1"))
		epName := clickhouse.GetStr(epRow, "request_resource")
		if epName == "" {
			continue
		}
		epByPair[key] = append(epByPair[key], endpointInfo{
			name:              epName,
			total:             clickhouse.GetF64(epRow, "ep_total"),
			bizCode:           clickhouse.GetStr(epRow, "biz_code"),
			responseCode:      clickhouse.GetF64(epRow, "response_code"),
			responseException: clickhouse.GetStr(epRow, "response_exception"),
			responseStatus:    clickhouse.GetF64(epRow, "response_status"),
		})
	}

	// -----------------------------------------------------------------------
	// Step 1: Extract unique services and aggregate metrics per service.
	// -----------------------------------------------------------------------

	svcMap := map[tmKey]*tmAgg{}

	for _, row := range allData {
		sk0 := tmKey{clickhouse.GetF64(row, "auto_service_id_0"), clickhouse.GetF64(row, "auto_service_type_0")}
		sk1 := tmKey{clickhouse.GetF64(row, "auto_service_id_1"), clickhouse.GetF64(row, "auto_service_type_1")}

		if sk0 == sk1 {
			continue // self-loop: same service, no valid edge
		}
		total := clickhouse.GetF64(row, "total")
		rTotal := clickhouse.GetF64(row, "response_total")
		durSum := clickhouse.GetF64(row, "response_duration_sum")
		succ := clickhouse.GetF64(row, "response_success_count")
		errCnt := clickhouse.GetF64(row, "response_status_server_error_count")

		// Endpoints from this row (for the client service, these are endpoints_1 in parent info)
		endpoints := strList(clickhouse.GetArr(row, "endpoints_arr"))

		svcName0 := clickhouse.GetStr(row, "auto_service_name_0")
		if svcName0 == "" {
			svcName0 = clickhouse.GetStr(row, "ip4_0")
		}
		agg0 := getOrCreate(svcMap, sk0, svcName0)
		// Metrics: only tracked on server side (incoming edges). Client side tracks
		// childIDs for BFS leveling and endpoint/gprocess collection only.
		agg0.childIDs[sk1] = true
		agg0.signalSource = clickhouse.GetF64(row, "signal_source")
		agg0.obsPoint = clickhouse.GetStr(row, "observation_point")
		if agg0.ip == "" {
			agg0.ip = clickhouse.GetStr(row, "ip4_0")
		}
		// Collect gprocess IDs for client side (skip zero/empty IDs)
		for _, gpid := range strList(clickhouse.GetArr(row, "gprocess_ids_0_arr")) {
			if gpid != "" && gpid != "0" {
				agg0.gprocessIDs[gpid] = struct{}{}
			}
		}

		svcName1 := clickhouse.GetStr(row, "auto_service_name_1")
		if svcName1 == "" {
			svcName1 = clickhouse.GetStr(row, "ip4_1")
		}
		agg1 := getOrCreate(svcMap, sk1, svcName1)
		agg1.total += total
		agg1.responseTotal += rTotal
		agg1.durationSum += durSum
		agg1.successCount += succ
		agg1.serverErrCount += errCnt
		agg1.parentIDs[sk0] = true
		agg1.signalSource = clickhouse.GetF64(row, "signal_source")
		agg1.obsPoint = clickhouse.GetStr(row, "observation_point")
		if agg1.ip == "" {
			agg1.ip = clickhouse.GetStr(row, "ip4_1")
		}
		// Collect gprocess IDs for server side (skip zero/empty IDs)
		for _, gpid := range strList(clickhouse.GetArr(row, "gprocess_ids_1_arr")) {
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
			clickhouse.GetF64(row, "auto_service_id_0"), clickhouse.GetF64(row, "auto_service_type_0"),
			clickhouse.GetF64(row, "auto_service_id_1"), clickhouse.GetF64(row, "auto_service_type_1"))
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
		nodeType := clickhouse.NodeTypeFor(int(sk.typ))
		iconID := clickhouse.IconFor(int(sk.typ))
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
			ti := float64(0)
			if s, ok := agg.epStats[uniqueServerEPs[i]]; ok {
				ti = s.total
			}
			tj := float64(0)
			if s, ok := agg.epStats[uniqueServerEPs[j]]; ok {
				tj = s.total
			}
			return ti > tj
		})
		if len(uniqueServerEPs) > 5 {
			uniqueServerEPs = uniqueServerEPs[:5]
		}

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
				"biz_response_code":  bizCode,
				"response_exception": respExc,
				"response_code":      respCode,
				"total":              epTotal,
				"response_status":    respStat,
			})
		}
		// Keep nil as nil for JSON null (matching cloud behavior).
		// Limit endpoints to top 5 by total count (matching cloud behavior).
		sort.Slice(uniqueClientEPs, func(i, j int) bool {
			ti := float64(0)
			if s, ok := agg.epStats[uniqueClientEPs[i]]; ok {
				ti = s.total
			}
			tj := float64(0)
			if s, ok := agg.epStats[uniqueClientEPs[j]]; ok {
				tj = s.total
			}
			return ti > tj
		})
		if len(uniqueClientEPs) > 5 {
			uniqueClientEPs = uniqueClientEPs[:5]
		}

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
				"biz_response_code":  bizCode,
				"response_exception": respExc,
				"response_code":      respCode,
				"total":              epTotal,
				"response_status":    respStat,
			})
		}
		// Keep nil as nil for JSON null (matching cloud behavior).
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
			"level":              levelOf[sk],
			"signal_source":      agg.signalSource,
			"response_code":      float64(0),
			"response_status":    float64(0),
			"response_exception": "",
			"biz_response_code":  "",
			"auto_service_type":  sk.typ,
			"auto_service_id":    sk.id,
			"icon_id":            iconID,
			"ip":                 agg.ip,
			"uid":                uid,
			"node_type":          nodeType,
			"app_service":        agg.name,
			"service_uid":        serviceUID,
			"auto_service":       agg.name,
			// (auto_service kept as-is; IP fallback is done in cloud but requires IP per edge)
			"_querier_region":   "本地",
			"observation_point": agg.obsPoint,
			"parent_node_infos": []interface{}{},

			// Endpoints at node level
			"endpoints_0":      clientEPsList,
			"endpoints_1":      serverEPsList,
			"endpoint_stats_0": clientEndpointStats,
			"endpoint_stats_1": serverEndpointStats,

			// Trace IDs as dict (cloud format)
			"trace_ids":          traceIDsMap,
			"abnormal_trace_ids": map[string]interface{}{},
			"gprocess_ids":       agg.gprocessIDs,

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
		// Null-conversion: empty maps → delete key (cloud omits the key entirely).
		if m, ok := node["gprocess_ids"].(map[string]interface{}); ok && len(m) == 0 {
			delete(node, "gprocess_ids")
		}
		if m, ok := node["trace_ids"].(map[string]interface{}); ok && len(m) == 0 {
			delete(node, "trace_ids")
		}
		if m, ok := node["abnormal_trace_ids"].(map[string]interface{}); ok && len(m) == 0 {
			delete(node, "abnormal_trace_ids")
		}
		// When no response data (total=0), set ratio/duration fields to nil (cloud behavior).
		if rTotal == 0 {
			node["avg_response_duration"] = nil
			node["avg_success_ratio"] = nil
			node["avg_response_ratio"] = nil
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
		sk0 := tmKey{clickhouse.GetF64(row, "auto_service_id_0"), clickhouse.GetF64(row, "auto_service_type_0")}
		sk1 := tmKey{clickhouse.GetF64(row, "auto_service_id_1"), clickhouse.GetF64(row, "auto_service_type_1")}
		if sk0 == sk1 {
			continue
		}
		idx1, ok1 := nodeIdx[sk1]
		idx0, ok0 := nodeIdx[sk0]
		if !ok1 || !ok0 {
			continue
		}
		par := nodes[idx1]["parent_node_infos"].([]interface{})

		if sk0 == sk1 {
			continue // self-loop: same service, no valid edge
		}
		total := clickhouse.GetF64(row, "total")
		rTotal := clickhouse.GetF64(row, "response_total")
		durSum := clickhouse.GetF64(row, "response_duration_sum")
		succ := clickhouse.GetF64(row, "response_success_count")
		errCnt := clickhouse.GetF64(row, "response_status_server_error_count")

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
			clickhouse.GetF64(row, "auto_service_id_0"), clickhouse.GetF64(row, "auto_service_type_0"),
			clickhouse.GetF64(row, "auto_service_id_1"), clickhouse.GetF64(row, "auto_service_type_1"))
		epInfos := epByPair[pairKey]
		// Limit to top 5 by total for span_info (matching cloud behavior).
		if len(epInfos) > 5 {
			sort.Slice(epInfos, func(i, j int) bool { return epInfos[i].total > epInfos[j].total })
			epInfos = epInfos[:5]
		}
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
		// Keep nil as nil for JSON null (matching cloud behavior).

		// Trace IDs for this edge (from groupArray)
		traceIDStrs := strList(clickhouse.GetArr(row, "trace_ids_arr"))
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

		// Add trace IDs to both client and server nodes (node level uses dict).
		for _, idx := range []int{idx0, idx1} {
			for _, tid := range traceIDsList {
				if existing, ok := nodes[idx]["trace_ids"].(map[string]interface{}); ok {
					existing[tid.(string)] = map[string]interface{}{}
				}
			}
		}
		// Add abnormal trace IDs to both client and server nodes.
		for _, idx := range []int{idx0, idx1} {
			for _, tid := range abnormalIDsList {
				if existing, ok := nodes[idx]["abnormal_trace_ids"].(map[string]interface{}); ok {
					existing[tid.(string)] = float64(1)
				}
			}
		}

		// Build observation_point display (use sample from data or empty)
		obsPt := clickhouse.GetStr(row, "observation_point")

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
					"signal_source":               clickhouse.GetF64(row, "signal_source"),
					"auto_service_type_0":         clickhouse.GetF64(row, "auto_service_type_0"),
					"auto_service_type_1":         clickhouse.GetF64(row, "auto_service_type_1"),
					"auto_service_id_0":           clickhouse.GetF64(row, "auto_service_id_0"),
					"auto_service_id_1":           clickhouse.GetF64(row, "auto_service_id_1"),
					"client_icon_id":              clickhouse.IconFor(int(clickhouse.GetF64(row, "auto_service_type_0"))),
					"server_icon_id":              clickhouse.IconFor(int(clickhouse.GetF64(row, "auto_service_type_1"))),
					"observation_point":           obsPt,
					"ip_0":                        clickhouse.GetStr(row, "ip4_0"),
					"ip_1":                        clickhouse.GetStr(row, "ip4_1"),
					"app_service_0":               clickhouse.GetStr(row, "auto_service_name_0"),
					"app_service_1":               clickhouse.GetStr(row, "auto_service_name_1"),
					"auto_service_0":              clickhouse.GetStr(row, "auto_service_name_0"),
					"auto_service_1":              clickhouse.GetStr(row, "auto_service_name_1"),
					"client_node_type":            clickhouse.NodeTypeFor(int(clickhouse.GetF64(row, "auto_service_type_0"))),
					"server_node_type":            clickhouse.NodeTypeFor(int(clickhouse.GetF64(row, "auto_service_type_1"))),
					"_querier_region":             "本地",
					"endpoints":                   endpointsList,
					"endpoint_stats":              endpointStats,
					"trace_ids":                   traceIDsList,
					"abnormal_trace_ids":          abnormalIDsList,
					"inferred_application_type_0": nil,
					"inferred_application_type_1": nil,
				},
			},
		}
		nodes[idx1]["parent_node_infos"] = append(par, parentInfo)
	}

	return &clickhouse.QueryTraceMapResult{
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
		name:        name,
		parentIDs:   map[tmKey]bool{},
		childIDs:    map[tmKey]bool{},
		gprocessIDs: map[string]interface{}{},
		epStats:     map[string]endpointStat{},
	}
	m[key] = a
	return a
}

// convertHistory transforms history query data for Top result rows.
func ConvertHistory(hist []map[string]interface{}, metrics []clickhouse.MetricExpr) []map[string]interface{} {
	var result []map[string]interface{}
	for _, row := range hist {
		entry := map[string]interface{}{}
		if toi, ok := row["toi"]; ok {
			entry["toi"] = toi
		}
		for _, m := range metrics {
			if v, ok := row[m.Key]; ok {
				entry[m.Key] = v
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
func FillNullHistory(hist []map[string]interface{}, interval, timeStart, timeEnd int64, fill string, metrics []clickhouse.MetricExpr) []map[string]interface{} {
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
					entry[m.Key] = float64(0)
				} else if fill == "none" {
					// Skip — don't create entry for missing data
					continue
				} else {
					// "null" or empty — use nil
					entry[m.Key] = nil
				}
			}
			result = append(result, entry)
		}
	}
	return result
}
