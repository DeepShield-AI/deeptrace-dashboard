package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/query"
)

// MockDataSource serves static mock data from example.json for List/Top/TraceMap.
// It is placed at the highest priority in the chain so ALL non-FlowLog queries get mock data.
type MockDataSource struct {
	mu   sync.RWMutex
	data []map[string]interface{}
}

// NewMockDataSource creates a MockDataSource that reads from the given JSON file.
// The file should be a DeepFlow response with a DATA array.
func NewMockDataSource(filePath string) *MockDataSource {
	ds := &MockDataSource{}
	ds.load(filePath)
	return ds
}

func (d *MockDataSource) load(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("⚠️ MockDataSource: cannot read %s: %v", path, err)
		return
	}
	// Try {"DATA": [...]} wrapper first.
	var wrapped struct {
		Data []map[string]interface{} `json:"DATA"`
	}
	if json.Unmarshal(data, &wrapped) == nil && len(wrapped.Data) > 0 {
		d.mu.Lock()
		d.data = wrapped.Data
		d.mu.Unlock()
		log.Printf("📦 MockDataSource: loaded %d rows from %s (DATA wrapper)", len(wrapped.Data), path)
		return
	}
	// Fallback: plain array [...] like traces.json.
	var arr []map[string]interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		log.Printf("⚠️ MockDataSource: cannot parse %s: %v", path, err)
		return
	}
	d.mu.Lock()
	d.data = arr
	d.mu.Unlock()
	log.Printf("📦 MockDataSource: loaded %d rows from %s (plain array)", len(arr), path)
}

func (d *MockDataSource) Name() string  { return "Mock" }
func (d *MockDataSource) Enabled() bool { return true }

// getData returns a copy of the loaded mock data.
func (d *MockDataSource) getData() []map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.data) == 0 {
		return nil
	}
	// Deep copy to avoid mutation races.
	out := make([]map[string]interface{}, len(d.data))
	for i, row := range d.data {
		cp := make(map[string]interface{}, len(row)+1)
		for k, v := range row {
			cp[k] = v
		}
		cp["_querier_region"] = "本地"
		out[i] = cp
	}
	return out
}

// QueryList returns all mock data rows as a List result.
// This handles all LIST-type queries (app_link_trace, etc.)
func (d *MockDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	data := d.getData()
	if len(data) == 0 {
		return nil, nil
	}

	// Build SCHEMAS from first row.
	schemas := clickhouse.BuildSchemas(data[0])

	return &query.Result{
		Data:   data,
		Count:  len(data),
		Type:   "Application_Detail_List",
		Fields: schemas,
	}, nil
}

// QueryTop aggregates mock data and returns TopN-style results.
// Builds service pairs from parent_span_id hierarchy in traces.json.
func (d *MockDataSource) QueryTop(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	data := d.getData()
	if len(data) == 0 {
		return nil, nil
	}

	// Build span index for parent lookups.
	byID := map[string]map[string]interface{}{}
	for _, row := range data {
		if id, ok := row["span_id"].(string); ok && id != "" {
			byID[id] = row
		}
	}

	// Build pairs as (parent_service, service) from parent_span_id.
	type pair struct{ s0, s1 string }
	type group struct {
		rows []map[string]interface{}
		rep  map[string]interface{} // representative span
		peer map[string]interface{} // parent span
	}
	groups := map[pair]*group{}
	hasServicePair := false

	for _, row := range data {
		svc := getStr(row, "auto_service")
		if svc == "" {
			continue
		}
		pid := getStr(row, "parent_span_id")
		if pid != "" {
			if parent := byID[pid]; parent != nil {
				pSvc := getStr(parent, "auto_service")
				if pSvc != "" {
					hasServicePair = true
					k := pair{pSvc, svc}
					if groups[k] == nil {
						groups[k] = &group{rep: row, peer: parent}
					}
					groups[k].rows = append(groups[k].rows, row)
				}
			}
		}
	}

	var result []map[string]interface{}

	if hasServicePair {
		for k, g := range groups {
			totalReq := len(g.rows)
			var sumDur float64
			errorCount := 0
			for _, row := range g.rows {
				if d, ok := row["response_duration"].(float64); ok {
					sumDur += d
				}
				if s, ok := row["response_status"].(float64); ok && s != 0 {
					errorCount++
				}
			}
			avgDur := 0.0
			errorRatio := 0.0
			if totalReq > 0 {
				avgDur = sumDur / float64(totalReq)
				errorRatio = float64(errorCount) / float64(totalReq)
			}

			r := map[string]interface{}{
				"_querier_region":   "本地",
				"query_id":         "R1-R1",
				"auto_service_0":   k.s0,
				"auto_service_1":   k.s1,
				"auto_service_id_0": getF64(g.peer, "auto_service_id"),
				"auto_service_id_1": getF64(g.rep, "auto_service_id"),
				"请求总量":           float64(totalReq),
				"响应时延":           avgDur,
				"错误率":            errorRatio,
			}
			// Add _0 / _1 aliases the frontend expects for client/server.
			r["_0"] = k.s0
			r["_1"] = k.s1
			// Add icon_id and node_type fields.
			r["client_icon_id"] = getF64(g.peer, "icon_id")
			if r["client_icon_id"] == nil || r["client_icon_id"].(float64) == 0 {
				r["client_icon_id"] = float64(-17)
			}
			r["server_icon_id"] = getF64(g.rep, "icon_id")
			if r["server_icon_id"] == nil || r["server_icon_id"].(float64) == 0 {
				r["server_icon_id"] = float64(-13)
			}
			r["client_node_type"] = getStr(g.peer, "node_type")
			if r["client_node_type"] == "" {
				r["client_node_type"] = "_"
			}
			r["server_node_type"] = getStr(g.rep, "node_type")
			if r["server_node_type"] == "" {
				r["server_node_type"] = "_"
			}

			result = append(result, r)
		}
	} else {
		// No parent-child relationships: just return all rows.
		for _, row := range data {
			row["query_id"] = "R1-R1"
			result = append(result, row)
		}
	}

	if len(result) == 0 {
		return &query.Result{Data: []map[string]interface{}{}}, nil
	}

	schemas := clickhouse.BuildSchemas(result[0])
	return &query.Result{
		Data:   result,
		Count:  len(result),
		Fields: schemas,
		Type:   "Application_Detail_Top",
	}, nil
}

// QueryTraceMap builds TraceMap node data from mock data rows.
func (d *MockDataSource) QueryTraceMap(ctx context.Context, req *query.QuerierListRequest) (*query.TraceMapResult, error) {
	data := d.getData()
	if len(data) == 0 {
		return nil, nil
	}

	// Build a span index by _id (if available) or index position.
	type spanInfo struct {
		index     int
		service   string
		serviceID float64
		parentID  string
		row       map[string]interface{}
	}
	spans := make([]spanInfo, 0, len(data))
	byID := map[string]int{}

	getS := func(m map[string]interface{}, k string) string {
		if v, ok := m[k].(string); ok {
			return v
		}
		return ""
	}
	getF := func(m map[string]interface{}, k string) float64 {
		if v, ok := m[k].(float64); ok {
			return v
		}
		return 0
	}

	for i, row := range data {
		id := getS(row, "_id")
		svc := getS(row, "auto_service_1")
		if svc == "" {
			svc = getS(row, "auto_instance_1")
		}
		svcID := getF(row, "auto_service_id_1")
		spans = append(spans, spanInfo{
			index:     i,
			service:   svc,
			serviceID: svcID,
			parentID:  "",
			row:       row,
		})
		if id != "" {
			byID[id] = i
		}
	}

	var nodes []map[string]interface{}
	for i, s := range spans {
		dur := getF(s.row, "response_duration")
		obsPoint := getS(s.row, "observation_point")
		if obsPoint == "" {
			obsPoint = "c-app"
		}

		node := map[string]interface{}{
			"level":             1,
			"signal_source":     float64(3),
			"response_code":     s.row["response_code"],
			"response_status":   s.row["response_status"],
			"response_exception": "",
			"biz_response_code": "",
			"auto_service_type": float64(11),
			"auto_service_id":   s.serviceID,
			"icon_id":           float64(-16),
			"ip":                "",
			"uid":               fmt.Sprintf("self_index=%d,auto_service_id=%v,app_service=%s", i, s.serviceID, s.service),
			"node_type":         "_",
			"app_service":       s.service,
			"service_uid":       fmt.Sprintf("auto_service_id=%v,app_service=%s", s.serviceID, s.service),
			"auto_service":      s.service,
			"observation_point": obsPoint,
			"parent_node_infos": []interface{}{
				map[string]interface{}{
					"pseudo_link":                        0,
					"parent_index":                       0,
					"total":                              1,
					"response_total":                     1,
					"response_duration_sum":              dur,
					"response_status_server_error_count": 0,
					"response_success_count":             1,
					"uniq_parent_span_infos": []interface{}{
						map[string]interface{}{
							"signal_source":       float64(3),
							"auto_service_type_0": float64(11),
							"auto_service_type_1": float64(11),
							"auto_service_id_0":   getF(s.row, "auto_service_id_0"),
							"auto_service_id_1":   s.serviceID,
							"client_icon_id":      float64(-16),
							"server_icon_id":      float64(-16),
							"observation_point":   obsPoint,
							"ip_0":                "",
							"ip_1":                "",
							"app_service_0":       getS(s.row, "auto_service_0"),
							"app_service_1":       s.service,
							"auto_service_0":      getS(s.row, "auto_service_0"),
							"auto_service_1":      s.service,
							"client_node_type":    "_",
							"server_node_type":    "_",
							"endpoints":           []interface{}{getS(s.row, "request_resource")},
						},
					},
				},
			},
		}
		nodes = append(nodes, node)
	}

	return &query.TraceMapResult{
		NodeData: nodes,
		ProgressInfo: map[string]interface{}{
			"total_traces_count":      1,
			"calculated_traces_count": 1,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getStr safely extracts a string from a map.
func getStr(m map[string]interface{}, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// getF64 safely extracts a float64 from a map.
func getF64(m map[string]interface{}, k string) float64 {
	if m == nil {
		return 0
	}
	if v, ok := m[k].(float64); ok {
		return v
	}
	return 0
}

// Ensure interface compliance.
var _ query.QuerierListSource = (*MockDataSource)(nil)
var _ query.QuerierTopSource = (*MockDataSource)(nil)
var _ query.TraceMapSource = (*MockDataSource)(nil)
