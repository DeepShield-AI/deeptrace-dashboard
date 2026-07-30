package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/query"
)

// RegisterQuerier registers all core querier List/Top/Profile endpoints.
func RegisterQuerier(mux *http.ServeMux, srv *query.QuerierService) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/List", handleList(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/Top", handleTop(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/Profile", handleProfile(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/Topo", handleTopo(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MergedMultiList", handleList(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MultiPromList", handleMultiPromList(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MultiTop", handleTop(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MultiHistogram", handleList(srv))

	// Resource list endpoints.
	for _, path := range []string{"ContainerList", "HostList", "ServiceList", "ResourceUsageList"} {
		mux.HandleFunc("/api/statistics/v1/stats/querier/"+path, handleList(srv))
	}

	// Alarm event history.
	mux.HandleFunc("/api/statistics/v1/stats/querier/AlarmEventHistory", handleList(srv))

	// Universal/generic endpoints.
	mux.HandleFunc("/api/statistics/v1/stats/querier/UniversalTop", handleTop(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/UniversalHistory", handleUniversalHistory(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/UniversalPromHistory", handleUniversalHistory(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MultiUniversalPromHistory", handleUniversalHistory(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/UnsupportedTags", handleUnsupportedTags)
	mux.HandleFunc("/api/statistics/v1/stats/querier/L", handleList(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/Npb", handleList(srv))
}

// --------------------------------------------------------------------------
// List
// --------------------------------------------------------------------------

func handleList(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		req.NormalizeQuery()
		result, err := srv.QueryList(r.Context(), &req)
		if err != nil {
			log.Printf("⚠️  QueryList error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}


// --------------------------------------------------------------------------
// MultiPromList
// --------------------------------------------------------------------------

func handleMultiPromList(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req struct {
			TimeStart int64    `json:"time_start"`
			TimeEnd   int64    `json:"time_end"`
			PromQLs   []string `json:"PROMQLS"`
			Metrics   []string `json:"METRICS"`
			TOP       int      `json:"TOP"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			log.Printf("⚠️  MultiPromList unmarshal error: %v", err)
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS": "SUCCESS", "TYPE": "Multi_Prom_List", "DATA": []interface{}{},
			})
			return
		}
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS": "SUCCESS",
			"TYPE":       "Multi_Prom_List",
			"DATA":       []interface{}{},
		})
	}
}

// --------------------------------------------------------------------------
// Top
// --------------------------------------------------------------------------

func handleTop(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			log.Printf("⚠️  Top unmarshal error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		req.NormalizeQuery()
		result, err := srv.QueryTop(r.Context(), &req)
		if err != nil {
			log.Printf("⚠️  QueryTop error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}

// --------------------------------------------------------------------------
// Profile
// --------------------------------------------------------------------------

func handleProfile(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		req.NormalizeQuery()
		result, err := srv.QueryTopForProfile(r.Context(), &req)
		if err != nil {
			log.Printf("⚠️  Profile error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}


func handleTopo(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			log.Printf("⚠️  Topo unmarshal error: %v", err)
			writeSuccess(w, map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}})
			return
		}
		// flow_metrics topo: query ClickHouse directly (ZT doesn't support aggregation funcs).
		if req.Database == "flow_metrics" {
			data := queryTopoFlowMetrics(srv.CH, req)
			writeSuccess(w, data)
			return
		}
		// flow_log topo: inject auto_service_type columns for real type values.
		if len(req.Queries) > 0 {
			q := &req.Queries[0]
			if !strings.Contains(q.Select, "auto_service_type_0") {
				q.Select += ", auto_service_type_0, auto_service_type_1"
			}
			if !strings.Contains(q.GroupBy, "auto_service_type_0") {
				if q.GroupBy != "" {
					q.GroupBy += ", auto_service_type_0, auto_service_type_1"
				}
			}
			seen := map[string]bool{}
			for _, t := range q.Tags { seen[t] = true }
			if !seen["auto_service_type_0"] {
				q.Tags = append(q.Tags, "auto_service_type_0")
			}
			if !seen["auto_service_type_1"] {
				q.Tags = append(q.Tags, "auto_service_type_1")
			}
		}
		// flow_log topo: reuse Top query chain.
		result, err := srv.QueryTop(r.Context(), &req)
		if err != nil {
			log.Printf("⚠️  Topo error: %v", err)
			writeSuccess(w, map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}})
			return
		}
		if result == nil || len(result.Data) == 0 {
			writeSuccess(w, map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}})
			return
		}
		peers := result.Data
		seen := map[string]bool{}
		var instances []map[string]interface{}
		for _, row := range peers {
			// Add uid_0/uid_1 to peers matching cloud format.
			// Override ZT placeholder node_type/icon_id with our mapping so uid_0/uid_1 match instances.
			for _, suf := range []string{"_0", "_1"} {
				ast := int(getIntVal(row, "auto_service_type"+suf))
				if suf == "_0" {
					row["client_node_type"] = topoNodeTypeFor(ast)
					row["client_icon_id"] = topoIconFor(ast)
				} else {
					row["server_node_type"] = topoNodeTypeFor(ast)
					row["server_icon_id"] = topoIconFor(ast)
				}
			}
			uid0 := buildTopoUID(row, "_0", "client_icon_id", "client_node_type")
			uid1 := buildTopoUID(row, "_1", "server_icon_id", "server_node_type")
			row["uid_0"] = uid0
			row["uid_1"] = uid1
			instances = appendTopoInstance(instances, seen, row, "_0", "c", uid0)
			instances = appendTopoInstance(instances, seen, row, "_1", "s", uid1)
		}
		writeSuccess(w, map[string]interface{}{"instance_data": instances, "peers_data": peers})
	}
}



// queryTopoFlowMetrics queries ClickHouse directly for Topo on flow_metrics tables.
func queryTopoFlowMetrics(ch *clickhouse.CHService, req query.QuerierListRequest) map[string]interface{} {
	if ch == nil || !ch.Enabled() {
		return map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}}
	}
	tbl := req.Table
	if tbl == "" {
		tbl = "application_map"
	}
	resolvedTbl := tbl
	if !strings.Contains(tbl, ".") {
		if req.DataSource != "" {
			resolvedTbl = tbl + "." + req.DataSource
		} else {
			resolvedTbl = tbl + ".1m"
		}
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", req.Database, resolvedTbl)
	ts := req.TimeStart
	te := req.TimeEnd
	if ts == 0 && te == 0 {
		return map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}}
	}

	// Resolve service names from biz_service_map.
	svcName0 := "dictGet('flow_tag.biz_service_map', 'name', toUInt64(auto_service_id_0)) AS auto_service_0"
	svcName1 := "dictGet('flow_tag.biz_service_map', 'name', toUInt64(auto_service_id_1)) AS auto_service_1"

	// Node type and icon from auto_service_type (complete mapping).
	// types: 0=internet_ip, 1=chost, 11=pod_service, 15=lb, 103=pod_cluster, 104=biz_service, 120=gprocess, 130/133=pod_group, 255=ip
	nodeType0 := "multiIf(auto_service_type_0=0,'internet_ip',auto_service_type_0=1,'chost',auto_service_type_0=11,'pod_service',auto_service_type_0=15,'lb',auto_service_type_0=103,'pod_cluster',auto_service_type_0=104,'biz_service',auto_service_type_0=120,'gprocess',auto_service_type_0=130,'pod_group',auto_service_type_0=133,'pod_group',auto_service_type_0=255,'ip','other') AS client_node_type"
	nodeType1 := "multiIf(auto_service_type_1=0,'internet_ip',auto_service_type_1=1,'chost',auto_service_type_1=11,'pod_service',auto_service_type_1=15,'lb',auto_service_type_1=103,'pod_cluster',auto_service_type_1=104,'biz_service',auto_service_type_1=120,'gprocess',auto_service_type_1=130,'pod_group',auto_service_type_1=133,'pod_group',auto_service_type_1=255,'ip','other') AS server_node_type"
	iconID0 := "multiIf(auto_service_type_0=0,-1,auto_service_type_0=1,-23,auto_service_type_0=11,-16,auto_service_type_0=15,-12,auto_service_type_0=103,-13,auto_service_type_0=104,-45,auto_service_type_0=120,-43,auto_service_type_0=130,-18,auto_service_type_0=133,-18,auto_service_type_0=255,-10,-42) AS client_icon_id"
	iconID1 := "multiIf(auto_service_type_1=0,-1,auto_service_type_1=1,-23,auto_service_type_1=11,-16,auto_service_type_1=15,-12,auto_service_type_1=103,-13,auto_service_type_1=104,-45,auto_service_type_1=120,-43,auto_service_type_1=130,-18,auto_service_type_1=133,-18,auto_service_type_1=255,-10,-42) AS server_icon_id"

	// Observation point enum translation.
	obsCN := "dictGet('flow_tag.string_enum_map', 'name_zh', ('observation_point', observation_point)) AS `Enum(observation_point)`"

	// Detect table type: application_map (L7) or network_map (L4).
	isAppMap := strings.Contains(tbl, "application")

	// Build metric SQL depending on table type.
	var metricSQL string
	if isAppMap {
		// application_map has L7 metrics and l7_protocol:
		// 使用 any(l7_protocol) 避免 GROUP BY 协议导致的行膨胀 (P4)
		proto0 := "nullIf(dictGet('flow_tag.int_enum_map', 'name_en', ('l7_protocol', toInt16(any(l7_protocol)))), '') AS resource_l7_protocol_0"
		proto1 := "nullIf(dictGet('flow_tag.int_enum_map', 'name_en', ('l7_protocol', toInt16(any(l7_protocol)))), '') AS resource_l7_protocol_1"
		metricSQL = fmt.Sprintf(`
			%s, %s,
			sum(request) AS sum_request,
			sum(rrt_sum) / greatest(sum(rrt_count), 1) AS avg_rrt,
			sum(server_error) / greatest(sum(request), 1) AS server_error_ratio`,
			proto0, proto1)
	} else {
		// network_map has L4 metrics; no l7_protocol column (P3)
		metricSQL = `
			NULL AS resource_l7_protocol_0,
			NULL AS resource_l7_protocol_1,
			sum(byte) * 8 AS sum_byte_rate,
			ifNull(sum(rtt_sum) / greatest(sum(rtt_count), 1), 0) AS avg_rtt,
			ifNull(sum(tcp_establish_fail) / greatest(sum(syn_count) + sum(synack_count), 1), 0) AS establish_fail_ratio,
			ifNull(sum(retrans) / greatest(sum(byte), 1), 0) AS retrans_ratio`
	}

	// Determine ORDER BY column (different metric for each table).
	orderBy := "sum_request"
	if !isAppMap {
		orderBy = "sum_byte_rate"
	}

	sql := fmt.Sprintf(`
		SELECT
			%s, %s, %s, %s,
			auto_service_id_0, auto_service_type_0,
			auto_service_id_1, auto_service_type_1,
			%s,
			observation_point,
			any(ip4_0) AS ip4_0, any(ip4_1) AS ip4_1,
			%s, %s,
			%s
		FROM %s
		WHERE time >= %d AND time <= %d
		GROUP BY
			auto_service_id_0, auto_service_type_0,
			auto_service_id_1, auto_service_type_1,
			observation_point
		ORDER BY %s DESC
		LIMIT 50`,
		svcName0, svcName1, nodeType0, nodeType1,
		obsCN,
		iconID0, iconID1,
		metricSQL,
		fullTable, ts, te,
		orderBy)

	log.Printf("🔍 CH Topo (%s): %s", tbl, sql[:min(len(sql), 300)])

	qCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := ch.Query(qCtx, sql)
	if err != nil {
		log.Printf("⚠️  CH Topo (%s) error: %v", tbl, err)
		return map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}}
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil || len(data) == 0 {
		return map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}}
	}

	var peers []map[string]interface{}
	seen := map[string]bool{}
	var instances []map[string]interface{}
	var rateKey, errKey, latencyKey string
	for _, row := range data {
		uid0 := buildTopoUID(row, "_0", "client_icon_id", "client_node_type")
		uid1 := buildTopoUID(row, "_1", "server_icon_id", "server_node_type")

		// Fallback: if auto_service is empty and id is 0, use IP4 as name (orphan case P2)
		svc0 := getStrVal(row, "auto_service_0")
		if svc0 == "" {
			if v, ok := row["ip4_0"]; ok && v != nil {
				svc0 = fmt.Sprintf("%v", v)
			}
		}
		svc1 := getStrVal(row, "auto_service_1")
		if svc1 == "" {
			if v, ok := row["ip4_1"]; ok && v != nil {
				svc1 = fmt.Sprintf("%v", v)
			}
		}

		// Determine metric field names based on table type.
		if isAppMap {
			rateKey = "请求速率"
			latencyKey = "响应时延"
			errKey = "服务端异常比例"
		} else {
			rateKey = "流量速率"
			latencyKey = "TCP 建连时延"
			errKey = "TCP 重传比例"
		}

		// Pick metric values based on table type.
		var rate, errRatio, latency interface{}
		if isAppMap {
			rate = row["sum_request"]
			errRatio = row["server_error_ratio"]
			latency = row["avg_rrt"]
		} else {
			rate = row["sum_byte_rate"]
			errRatio = row["retrans_ratio"]
			latency = row["avg_rtt"]
		}

		peer := map[string]interface{}{
			"query_id":                "R1-R1",
			"client_node_type":        row["client_node_type"],
			"client_icon_id":          row["client_icon_id"],
			"auto_service_id_0":       row["auto_service_id_0"],
			"auto_service_0":          svc0,
			"auto_service_type_0":     row["auto_service_type_0"],
			"is_internet_0":           0,
			"server_node_type":        row["server_node_type"],
			"server_icon_id":          row["server_icon_id"],
			"auto_service_id_1":       row["auto_service_id_1"],
			"auto_service_1":          svc1,
			"auto_service_type_1":     row["auto_service_type_1"],
			"is_internet_1":           0,
			"observation_point":       row["observation_point"],
			"Enum(observation_point)": row["Enum(observation_point)"],
			"uid_0":                   uid0,
			"uid_1":                   uid1,
			"resource_l7_protocol_0":  row["resource_l7_protocol_0"],
			"resource_l7_protocol_1":  row["resource_l7_protocol_1"],
			"_querier_region":         "本地",
		}
		// Set metrics
		peer[rateKey] = rate
		if !isAppMap {
			// network_map specific metrics
			peer["TCP 重传比例"] = errRatio  // TCP retransmission ratio
			peer["TCP 建连失败比例"] = row["establish_fail_ratio"]  // TCP establish fail ratio
			peer[latencyKey] = latency
		} else {
			peer["服务端异常比例"] = errRatio  // server error ratio
			peer[latencyKey] = latency
		}

		peers = append(peers, peer)
		instances = appendTopoInstanceFm(instances, seen, row, "_0", "c", uid0, svc0, rate, errRatio, latency, rateKey, errKey, latencyKey)
		instances = appendTopoInstanceFm(instances, seen, row, "_1", "s", uid1, svc1, rate, errRatio, latency, rateKey, errKey, latencyKey)
	}

	// P2: Add orphan instances for IP-based services without a regular instance.
	instances = appendOrphanInstances(instances, peers, isAppMap)
	return map[string]interface{}{"instance_data": instances, "peers_data": peers}
}

// appendTopoInstanceFm adds a unique service instance from flow_metrics topo rows.
func appendTopoInstanceFm(instances []map[string]interface{}, seen map[string]bool,
	row map[string]interface{}, suffix, role string, uid string,
	svcName string, rate, errRatio, latency interface{},
	rateKey, errKey, latencyKey string) []map[string]interface{} {
	svcID := fmt.Sprintf("%v", row["auto_service_id"+suffix])
	svcType := fmt.Sprintf("%v", row["auto_service_type"+suffix])
	obs := fmt.Sprintf("%v", row["observation_point"])
	key := svcID + "|" + svcType + "|" + obs + "|" + svcName
	if seen[key] {
		return instances
	}
	seen[key] = true

	// Determine column keys based on suffix.
	var nodeTypeKey, iconIDKey string
	if suffix == "_0" {
		nodeTypeKey = "client_node_type"
		iconIDKey = "client_icon_id"
	} else {
		nodeTypeKey = "server_node_type"
		iconIDKey = "server_icon_id"
	}

	inst := map[string]interface{}{
		"rs_set_id":           "R1",
		"observation_point":   obs,
		"Enum(observation_point)": row["Enum(observation_point)"],
		"role":                role,
		"_querier_region":     "本地",
		"uid":                 uid,
		"node_type":           row[nodeTypeKey],
		"icon_id":             row[iconIDKey],
		"auto_service_id":     row["auto_service_id"+suffix],
		"auto_service":        svcName,
		"auto_service_type":   row["auto_service_type"+suffix],
		"is_internet":         0,
		"resource_l7_protocol": row["resource_l7_protocol_0"],
	}
	// Set metrics from passed-in values (keys vary by table type).
	if rate != nil {
		inst[rateKey] = rate
	}
	if errRatio != nil {
		inst[errKey] = errRatio
	}
	if latency != nil {
		inst[latencyKey] = latency
	}
	return append(instances, inst)
}

func handleUniversalHistory(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		result, err := srv.QueryTop(r.Context(), &req)
		if err != nil {
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}

func handleUnsupportedTags(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, []interface{}{})
}

// appendTopoInstance extracts a unique service instance from _0 (client) or _1 (server) side
// of a Topo peer row and appends it to instances if not already seen.
func appendTopoInstance(instances []map[string]interface{}, seen map[string]bool,
	row map[string]interface{}, suffix, role string, uid string) []map[string]interface{} {

	// Build a unique key from (auto_service_id_suffix, observation_point, is_internet_suffix)
	svcID := getStrVal(row, "auto_service_id"+suffix)
	svcName := getStrVal(row, "auto_service"+suffix)
	obs := getStrVal(row, "observation_point")
	isNet := getStrVal(row, "is_internet"+suffix)
	key := svcID + "|" + obs + "|" + isNet + "|" + svcName
	if seen[key] {
		return instances
	}
	seen[key] = true


	inst := map[string]interface{}{
		"rs_set_id":        "R1",
		"observation_point": obs,
		"Enum(observation_point)": row["Enum(observation_point)"],
		"role":              role,
		"uid":                 uid,
		"_querier_region":   "本地",
		"auto_service_id":   row["auto_service_id"+suffix],
		"auto_service":      row["auto_service"+suffix],
		"auto_service_type": row["auto_service_type"+suffix],
		"is_internet":       row["is_internet"+suffix],
		"node_type":         topoNodeTypeFor(int(getIntVal(row, "auto_service_type"+suffix))),
		"icon_id":           topoIconFor(int(getIntVal(row, "auto_service_type"+suffix))),
		"resource_l7_protocol": row["resource_l7_protocol" + suffix],
	}

	// Copy metric fields from the row.
	if v, ok := row["请求速率"]; ok {
		inst["请求速率"] = v
	}
	if v, ok := row["服务端异常比例"]; ok {
		inst["服务端异常比例"] = v
	}

	return append(instances, inst)
}

// appendOrphanInstances creates orphan instances for IP-based services without a regular instance.
// These have no observation_point/role and carry add_description, matching cloud behavior.
func appendOrphanInstances(instances []map[string]interface{}, peers []map[string]interface{}, isAppMap bool) []map[string]interface{} {
	seenUIDs := map[string]bool{}
	for _, inst := range instances {
		if uid, ok := inst["uid"].(string); ok {
			seenUIDs[uid] = true
		}
	}
	// Metric key names depend on table type.
	var rateKey, errRatioKey, latencyKey string
	if isAppMap {
		rateKey = "请求速率"
		errRatioKey = "服务端异常比例"
		latencyKey = "响应时延"
	} else {
		rateKey = "流量速率"
		errRatioKey = "TCP 重传比例"
		latencyKey = "TCP 建连时延"
	}
	for _, p := range peers {
		for _, suffix := range []string{"_0", "_1"} {
			svcIDStr := fmt.Sprintf("%v", p["auto_service_id"+suffix])
			if svcIDStr != "0" {
				continue
			}
			svcName := getStrVal(p, "auto_service"+suffix)
			if svcName == "" {
				continue
			}
			var iconKey, nodeKey string
			if suffix == "_0" {
				iconKey = "client_icon_id"
				nodeKey = "client_node_type"
			} else {
				iconKey = "server_icon_id"
				nodeKey = "server_node_type"
			}
			uid := buildTopoUIDFromMap(p, suffix, iconKey, nodeKey, svcName)
			if seenUIDs[uid] {
				continue
			}
			seenUIDs[uid] = true
			protoKey := "resource_l7_protocol" + suffix
			proto := p[protoKey]
			inst := map[string]interface{}{
				"rs_set_id":           "R1",
				"uid":                 uid,
				"add_description":   "双端不在单端的补点",
				"_querier_region":   "本地",
				"node_type":           p[nodeKey],
				"icon_id":             p[iconKey],
				"auto_service_id":     p["auto_service_id"+suffix],
				"auto_service":        svcName,
				"auto_service_type":   p["auto_service_type"+suffix],
				"is_internet":         0,
				"resource_l7_protocol": proto,
			}
			if v, ok := p[rateKey]; ok {
				inst[rateKey] = v
			}
			if v, ok := p[errRatioKey]; ok {
				inst[errRatioKey] = v
			}
			if v, ok := p[latencyKey]; ok {
				inst[latencyKey] = v
			}
			instances = append(instances, inst)
		}
	}
	return instances
}

func buildTopoUIDFromMap(m map[string]interface{}, suffix, iconKey, nodeKey, svcName string) string {
	svcID := fmt.Sprintf("%v", m["auto_service_id"+suffix])
	svcType := fmt.Sprintf("%v", m["auto_service_type"+suffix])
	iconID := fmt.Sprintf("%v", m[iconKey])
	nodeType := fmt.Sprintf("%v", m[nodeKey])
	return fmt.Sprintf("auto_service=%s,auto_service_id=%s,auto_service_type=%s,icon_id=%s,is_internet=0,node_type=%s,rs_set_id=R1",
		svcName, svcID, svcType, iconID, nodeType)
}

// buildTopoUID builds a UID string matching cloud format:
// auto_service=<name>,auto_service_id=<id>,auto_service_type=<type>,icon_id=<icon>,is_internet=<n>,node_type=<nt>,rs_set_id=R1
func buildTopoUID(row map[string]interface{}, suffix, iconKey, nodeKey string) string {
	svcName := ""
	if v, ok := row["auto_service"+suffix]; ok && v != nil {
		svcName = fmt.Sprintf("%v", v)
	}
	svcID := ""
	if v, ok := row["auto_service_id"+suffix]; ok && v != nil {
		svcID = fmt.Sprintf("%v", v)
	}
	svcType := ""
	if v, ok := row["auto_service_type"+suffix]; ok && v != nil {
		svcType = fmt.Sprintf("%v", v)
	}
	iconID := ""
	if v, ok := row[iconKey]; ok && v != nil {
		iconID = fmt.Sprintf("%v", v)
	}
	nodeType := ""
	if v, ok := row[nodeKey]; ok && v != nil {
		nodeType = fmt.Sprintf("%v", v)
	}
	return fmt.Sprintf("auto_service=%s,auto_service_id=%s,auto_service_type=%s,icon_id=%s,is_internet=0,node_type=%s,rs_set_id=R1",
		svcName, svcID, svcType, iconID, nodeType)
}

// getStrVal safely extracts a string value from a map.
// topoNodeTypeFor maps auto_service_type to node_type string (matches cloud behavior).
func topoNodeTypeFor(t int) string {
	switch t {
	case 0:
		return "internet_ip"
	case 1:
		return "chost"
	case 11:
		return "pod_service"
	case 15:
		return "lb"
	case 103:
		return "pod_cluster"
	case 104:
		return "biz_service"
	case 120:
		return "gprocess"
	case 130, 133:
		return "pod_group"
	case 255:
		return "ip"
	default:
		return "other"
	}
}

// topoIconFor maps auto_service_type to icon_id (matches cloud behavior).
func topoIconFor(t int) int {
	switch t {
	case 0:
		return -1
	case 1:
		return -23
	case 11:
		return -16
	case 15:
		return -12
	case 103:
		return -13
	case 104:
		return -45
	case 120:
		return -43
	case 130, 133:
		return -18
	case 255:
		return -10
	default:
		return -42
	}
}

// getIntVal safely extracts an int value from a map (returns 0 if missing/nil).
func getIntVal(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int64:
			return int(n)
		case uint8:
			return int(n)
		case uint64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

// getStrVal safely extracts a string value from a map.
func getStrVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok2 := v.(string); ok2 {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}
