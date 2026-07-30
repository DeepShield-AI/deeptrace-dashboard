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
	"deeptrace-backend/query/topo"
)


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
			data := topo.QueryTopoFlowMetrics(srv.CH, req)
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
				ast := int(topo.GetIntVal(row, "auto_service_type"+suf))
				if suf == "_0" {
					row["client_node_type"] = topo.TopoNodeTypeFor(ast)
					row["client_icon_id"] = topo.TopoIconFor(ast)
				} else {
					row["server_node_type"] = topo.TopoNodeTypeFor(ast)
					row["server_icon_id"] = topo.TopoIconFor(ast)
				}
			}
			uid0 := topo.BuildTopoUID(row, "_0", "client_icon_id", "client_node_type")
			uid1 := topo.BuildTopoUID(row, "_1", "server_icon_id", "server_node_type")
			row["uid_0"] = uid0
			row["uid_1"] = uid1
			instances = appendTopoInstance(instances, seen, row, "_0", "c", uid0)
			instances = appendTopoInstance(instances, seen, row, "_1", "s", uid1)
		}
		writeSuccess(w, map[string]interface{}{"instance_data": instances, "peers_data": peers})
	}
}



// queryTopoFlowMetrics queries ClickHouse directly for Topo on flow_metrics tables.

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
	svcID := topo.GetStrVal(row, "auto_service_id"+suffix)
	svcName := topo.GetStrVal(row, "auto_service"+suffix)
	obs := topo.GetStrVal(row, "observation_point")
	isNet := topo.GetStrVal(row, "is_internet"+suffix)
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
		"node_type":         topo.TopoNodeTypeFor(int(topo.GetIntVal(row, "auto_service_type"+suffix))),
		"icon_id":           topo.TopoIconFor(int(topo.GetIntVal(row, "auto_service_type"+suffix))),
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
			svcName := topo.GetStrVal(p, "auto_service"+suffix)
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
			uid := topo.BuildTopoUIDFromMap(p, suffix, iconKey, nodeKey, svcName)
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

