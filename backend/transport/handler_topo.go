package transport

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

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
			instances = topo.AppendTopoInstance(instances, seen, row, "_0", "c", uid0)
			instances = topo.AppendTopoInstance(instances, seen, row, "_1", "s", uid1)
		}
		writeSuccess(w, map[string]interface{}{"instance_data": instances, "peers_data": peers})
	}
}



// queryTopoFlowMetrics queries ClickHouse directly for Topo on flow_metrics tables.
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

