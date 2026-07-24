package aggregator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"deeptrace-backend/query"
)

// Aggregator provides trace data aggregation from local JSON data files.
// It is the final fallback in the handler data chain.
type Aggregator struct {
	dataDir string
}

// NewAggregator creates an aggregator that reads data from dir.
func NewAggregator(dataDir string) *Aggregator {
	return &Aggregator{dataDir: dataDir}
}

// ---------------------------------------------------------------------------
// Public helpers used by handlers
// ---------------------------------------------------------------------------

// ReadDataFile reads a raw file from the data directory.
func (a *Aggregator) ReadDataFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(a.dataDir, name))
}

// ReadDataFileJSON reads and unmarshals a JSON file from the data directory.
func (a *Aggregator) ReadDataFileJSON(name string) (interface{}, error) {
	data, err := a.ReadDataFile(name)
	if err != nil {
		return nil, err
	}
	var v interface{}
	err = json.Unmarshal(data, &v)
	return v, err
}

// LoadSpans reads traces.json and returns a list of span maps.
func (a *Aggregator) LoadSpans() []map[string]interface{} {
	data, err := a.ReadDataFileJSON("traces.json")
	if err != nil {
		return nil
	}
	list, ok := data.([]interface{})
	if !ok {
		return nil
	}
	spans := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if m, ok2 := item.(map[string]interface{}); ok2 {
			spans = append(spans, m)
		}
	}
	return spans
}

// ---------------------------------------------------------------------------
// Querier List — aggregation from traces.json
// ---------------------------------------------------------------------------

// AggregateList builds querier List rows from traces.json based on the request definition.
func (a *Aggregator) AggregateList(req query.QuerierListRequest) []map[string]interface{} {
	spans := a.LoadSpans()
	if len(spans) == 0 {
		return nil
	}
	q := query.QuerierListQuery{}
	if len(req.Queries) > 0 {
		q = req.Queries[0]
	}
	selects := parseSelectAliases(q.Select)
	isPair := strings.Contains(req.Table, "_map") || strings.Contains(q.Select, "_0")

	// Build span index for parent lookup.
	byID := map[string]map[string]interface{}{}
	for _, s := range spans {
		if id, ok2 := s["span_id"].(string); ok2 {
			byID[id] = s
		}
	}

	type group struct {
		spans []map[string]interface{}
		rep   map[string]interface{} // representative span
		peer  map[string]interface{} // parent span (for pair queries)
	}
	groups := map[string]*group{}

	hasGroupTags := len(q.Tags) > 0 || strings.Contains(q.Select, "auto_service")

	switch {
	case isPair:
		for _, s := range spans {
			pid, _ := s["parent_span_id"].(string)
			parent := byID[pid]
			if parent == nil {
				continue
			}
			gkey := fmt.Sprintf("%v→%v", parent["auto_service"], s["auto_service"])
			if groups[gkey] == nil {
				groups[gkey] = &group{rep: s, peer: parent}
			}
			groups[gkey].spans = append(groups[gkey].spans, s)
		}
	case hasGroupTags:
		for _, s := range spans {
			gkey := fmt.Sprintf("%v", s["auto_service"])
			if groups[gkey] == nil {
				groups[gkey] = &group{rep: s}
			}
			groups[gkey].spans = append(groups[gkey].spans, s)
		}
	default:
		g := &group{rep: spans[0], spans: spans}
		groups["_all"] = g
	}

	var rows []map[string]interface{}
	for _, g := range groups {
		row := map[string]interface{}{"_querier_region": "本地"}
		for _, se := range selects {
			expr, key := se[0], se[1]
			lower := strings.ToLower(expr)
			switch {
			case strings.HasPrefix(lower, "newtag("):
				row["query_id"] = q.QueryID
			case strings.Contains(lower, "avg(") || strings.Contains(lower, "count(") ||
				strings.Contains(lower, "sum(") || strings.Contains(lower, "persecond(") ||
				strings.Contains(lower, "max(") || strings.Contains(lower, "min("):
				row[key] = computeMetric(expr, g.spans)
			default:
				row[key] = resolveTag(expr, g.rep, g.peer)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// AggregateCount returns the total number of spans (for ungrouped count queries).
func (a *Aggregator) AggregateCount() int {
	return len(a.LoadSpans())
}

// ---------------------------------------------------------------------------
// TraceMap — node generation from traces.json
// ---------------------------------------------------------------------------

// BuildTraceMapNodes generates TraceMap node_data from traces.json spans.
func (a *Aggregator) BuildTraceMapNodes() []map[string]interface{} {
	spans := a.LoadSpans()
	if len(spans) == 0 {
		return nil
	}
	byID := map[string]map[string]interface{}{}
	for _, s := range spans {
		if id, ok2 := s["span_id"].(string); ok2 {
			byID[id] = s
		}
	}

	// Compute level for each span (root = 1).
	var levelOf func(m map[string]interface{}) int
	levelOf = func(s map[string]interface{}) int {
		pid, _ := s["parent_span_id"].(string)
		if pid == "" || byID[pid] == nil {
			return 1
		}
		return levelOf(byID[pid]) + 1
	}

	// Node index per span.
	indexOf := map[string]int{}
	for i, s := range spans {
		if id, ok2 := s["span_id"].(string); ok2 {
			indexOf[id] = i
		}
	}

	getF := func(m map[string]interface{}, k string) float64 {
		if v, ok2 := m[k].(float64); ok2 {
			return v
		}
		return 0
	}
	getS := func(m map[string]interface{}, k string) string {
		if v, ok2 := m[k].(string); ok2 {
			return v
		}
		return ""
	}

	var nodes []map[string]interface{}
	for i, s := range spans {
		svc := getS(s, "auto_service")
		svcID := getF(s, "auto_service_id")
		uid := fmt.Sprintf("self_index=%d,auto_service_id=%v,app_service=%s", i, svcID, svc)
		serviceUID := fmt.Sprintf("auto_service_id=%v,app_service=%s", svcID, svc)

		sigSrc := getF(s, "signal_source")
		if sigSrc == 0 && getS(s, "signal_source") == "" {
			sigSrc = 3
		}
		node := map[string]interface{}{
			"level":              levelOf(s),
			"signal_source":      sigSrc,
			"response_code":      s["response_code"],
			"response_status":    s["response_status"],
			"response_exception": "",
			"biz_response_code":  "",
			"auto_service_type":  11,
			"auto_service_id":    svcID,
			"icon_id":            s["icon_id"],
			"ip":                 "10.0.0." + fmt.Sprint(i+1),
			"uid":                uid,
			"node_type":          getS(s, "node_type"),
			"app_service":        svc,
			"service_uid":        serviceUID,
			"auto_service":       svc,
			"observation_point":  getS(s, "tap_side"),
			"parent_node_infos":  []interface{}{},
		}
		// Link to parent.
		pid, _ := s["parent_span_id"].(string)
		if parent := byID[pid]; parent != nil {
			pIdx := indexOf[pid]
			dur := getF(s, "response_duration")
			node["parent_node_infos"] = []interface{}{
				map[string]interface{}{
					"pseudo_link":                        0,
					"parent_index":                       pIdx,
					"total":                              1,
					"response_total":                     1,
					"response_duration_sum":              dur,
					"response_status_server_error_count": 0,
					"response_success_count":             1,
					"uniq_parent_span_infos": []interface{}{
						map[string]interface{}{
							"signal_source":       sigSrc,
							"auto_service_type_0": 11,
							"auto_service_type_1": 11,
							"auto_service_id_0":   getF(parent, "auto_service_id"),
							"auto_service_id_1":   svcID,
							"client_icon_id":      parent["icon_id"],
							"server_icon_id":      s["icon_id"],
							"observation_point":   getS(s, "tap_side"),
							"ip_0":                "",
							"ip_1":                "",
							"app_service_0":       getS(parent, "auto_service"),
							"app_service_1":       svc,
							"auto_service_0":      getS(parent, "auto_service"),
							"auto_service_1":      svc,
							"client_node_type":    getS(parent, "node_type"),
							"server_node_type":    getS(s, "node_type"),
							"endpoints":           []interface{}{getS(s, "resource")},
						},
					},
				},
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// ExtractQueryID gets QUERIES[0].QUERY_ID from a querier request body string.
func ExtractQueryID(bodyStr string) string {
	var req struct {
		Queries []struct {
			QueryID string `json:"QUERY_ID"`
		} `json:"QUERIES"`
	}
	if json.Unmarshal([]byte(bodyStr), &req) == nil && len(req.Queries) > 0 {
		return req.Queries[0].QueryID
	}
	return ""
}

// RewriteQueryID replaces all query_id values in cached response DATA with the requested one.
func RewriteQueryID(cached []byte, newQueryID string) []byte {
	if newQueryID == "" {
		return cached
	}
	var resp map[string]interface{}
	if json.Unmarshal(cached, &resp) != nil {
		return cached
	}
	if data, ok2 := resp["DATA"].([]interface{}); ok2 {
		for _, item := range data {
			if row, ok3 := item.(map[string]interface{}); ok3 {
				if _, has := row["query_id"]; has {
					row["query_id"] = newQueryID
				}
			}
		}
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return cached
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal helpers — copied from the original main.go aggregation logic
// ---------------------------------------------------------------------------

// selectAlias is (expr, key)
type selectAlias [2]string

func parseSelectAliases(sel string) []selectAlias {
	var out []selectAlias
	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		expr, key := part, part
		if idx := strings.LastIndex(strings.ToUpper(part), " AS "); idx >= 0 {
			expr = strings.TrimSpace(part[:idx])
			key = strings.Trim(strings.TrimSpace(part[idx+4:]), "`")
		}
		if key == "" {
			key = expr
		}
		out = append(out, selectAlias{expr, key})
	}
	return out
}

// computeMetric evaluates a metric expression against a group of spans.
func computeMetric(expr string, group []map[string]interface{}) interface{} {
	lower := strings.ToLower(expr)
	n := len(group)
	switch {
	case strings.Contains(lower, "count("):
		return n
	case strings.Contains(lower, "request"):
		return float64(n) / 60.0
	case strings.Contains(lower, "error_ratio") || strings.Contains(lower, "exception"):
		errors := 0
		for _, s := range group {
			if v, ok2 := s["response_status"].(float64); ok2 && v != 0 {
				errors++
			}
		}
		if n == 0 {
			return 0.0
		}
		return float64(errors) / float64(n)
	case strings.Contains(lower, "rrt") || strings.Contains(lower, "duration") || strings.Contains(lower, "时延"):
		sum := 0.0
		for _, s := range group {
			if v, ok2 := s["response_duration"].(float64); ok2 {
				sum += v
			}
		}
		if n == 0 {
			return 0.0
		}
		return sum / float64(n)
	default:
		return 0
	}
}

// resolveTag maps a tag expression to a value from a representative span (with optional edge peer).
func resolveTag(expr string, span, peer map[string]interface{}) interface{} {
	key := strings.Trim(expr, "`")
	// Strip function wrappers: node_type(auto_service) → node_type.
	if idx := strings.Index(key, "("); idx > 0 && strings.HasSuffix(key, ")") {
		fn := key[:idx]
		switch fn {
		case "node_type", "icon_id":
			key = fn
		case "Enum":
			inner := key[idx+1 : len(key)-1]
			if v, ok2 := span["Enum("+inner+")"]; ok2 {
				return v
			}
			if inner == "role" {
				return "客户端"
			}
			return ""
		}
	}
	// Edge fields: _0 = client (parent), _1 = server (current).
	if strings.HasSuffix(key, "_0") && peer != nil {
		base := strings.TrimSuffix(key, "_0")
		if v, ok2 := peer[base]; ok2 {
			return v
		}
		return ""
	}
	if strings.HasSuffix(key, "_1") {
		base := strings.TrimSuffix(key, "_1")
		if v, ok2 := span[base]; ok2 {
			return v
		}
		return ""
	}
	if key == "role" {
		return 0
	}
	if v, ok2 := span[key]; ok2 {
		return v
	}
	return ""
}

// ---------------------------------------------------------------------------
// Convenience helper (used from main.go / server.go)
// ---------------------------------------------------------------------------

// OKResponse is a helper to create the standard DeepFlow wrapper.
func OKResponse(data interface{}) map[string]interface{} {
	return map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DATA":        data,
		"DESCRIPTION": "",
	}
}

// Ensure unused import doesn't trigger.
