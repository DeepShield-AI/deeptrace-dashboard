package query

// QuerierListQuery represents a single query inside a querier List request.
type QuerierListQuery struct {
	QueryID string   `json:"QUERY_ID"`
	Select  string   `json:"SELECT"`
	Where   string   `json:"WHERE"`
	Tags    []string `json:"TAGS"`
	Metrics []string `json:"METRICS"`
	GroupBy string   `json:"GROUP_BY"`
}

// QuerierListRequest is the full body of a querier List/Top/FlowLog request.
type QuerierListRequest struct {
	Database  string             `json:"DATABASE"`
	Table     string             `json:"TABLE"`
	PageIndex int                `json:"PAGE_INDEX"`
	PageSize  int                `json:"PAGE_SIZE"`
	Queries   []QuerierListQuery `json:"QUERIES"`
	TimeStart int64              `json:"time_start"`
	TimeEnd   int64              `json:"time_end"`
}

// ---------------------------------------------------------------------------
// Response types (consolidated)
// ---------------------------------------------------------------------------

// Result is the standard response for List/Top/Histogram/FlowLog queries.
type Result struct {
	Data   []map[string]interface{}
	Count  int
	Type   string
	Fields map[string]interface{} // SCHEMAS (may be nil)
}

// Envelope wraps the result in the standard DeepFlow response format.
func (r *Result) Envelope() map[string]interface{} {
	m := map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DATA":        r.Data,
		"DESCRIPTION": "",
	}
	if r.Count > 0 || len(r.Data) == 0 {
		m["COUNT"] = r.Count
	}
	if r.Type != "" {
		m["TYPE"] = r.Type
	}
	if r.Fields != nil {
		m["SCHEMAS"] = r.Fields
	}
	return m
}

// TraceMapResult holds trace map node and progress data.
type TraceMapResult struct {
	NodeData     []map[string]interface{}
	ProgressInfo map[string]interface{}
}

// Envelope wraps the trace map result in the standard response format.
func (r *TraceMapResult) Envelope() map[string]interface{} {
	pi := r.ProgressInfo
	if pi == nil {
		pi = map[string]interface{}{
			"total_traces_count":      0,
			"calculated_traces_count": 0,
		}
	}
	return map[string]interface{}{
		"OPT_STATUS": "SUCCESS",
		"TYPE":       "TraceMap",
		"DATA": map[string]interface{}{
			"node_data":     r.NodeData,
			"progress_info": pi,
		},
		"DESCRIPTION": "",
		"debug":       map[string]interface{}{"querier_debug": nil},
	}
}
