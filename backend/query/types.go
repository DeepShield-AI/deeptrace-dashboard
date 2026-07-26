package query

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexInt accepts either a JSON number or a JSON string that represents a number.
// DeepFlow API sometimes sends TOP as 4 (number) or "5" (string).
type FlexInt int

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" { return nil }
	var n int
	if err := json.Unmarshal(b, &n); err == nil { *f = FlexInt(n); return nil }
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if n, err := strconv.Atoi(s); err == nil { *f = FlexInt(n); return nil }
	}
	return fmt.Errorf("cannot unmarshal %s into FlexInt", string(b))
}

// QuerierListQuery represents a single query inside a querier List request.
type QuerierListQuery struct {
	QueryID string   `json:"QUERY_ID"`
	Roles   []string `json:"ROLES"`
	Select  string   `json:"SELECT"`
	Where   string   `json:"WHERE"`
	Tags    []string `json:"TAGS"`
	CTags   []string `json:"CTAGS"`
	STags   []string `json:"STAGS"`
	Metrics []string `json:"METRICS"`
	GroupBy string   `json:"GROUP_BY"`
}

// ListSort represents the ORDER BY clause.
type ListSort struct {
	OrderBy  string `json:"ORDER_BY"`
	SortedBy string `json:"SORTED_BY"`
}

// QuerierListRequest is the full body of a querier List/Top/FlowLog request.
type QuerierListRequest struct {
	Database   string             `json:"DATABASE"`
	Table      string             `json:"TABLE"`
	PageIndex  int                `json:"PAGE_INDEX"`
	PageSize   int                `json:"PAGE_SIZE"`
	Queries    []QuerierListQuery `json:"QUERIES"`
	TimeStart  int64              `json:"time_start"`
	TimeEnd    int64              `json:"time_end"`
	Sort       *ListSort          `json:"SORT,omitempty"`
	IncludeHis bool               `json:"INCLUDE_HISTORY"`
	Top        FlexInt            `json:"TOP"`
	Interval   int                `json:"interval"`
	WindowSize int                `json:"window_size"`
	Fill       string             `json:"fill"`
	RawBody    json.RawMessage    `json:"-"`
}

// ---------------------------------------------------------------------------
// Response types (consolidated)
// ---------------------------------------------------------------------------

// Result is the standard response for List/Top/Histogram/FlowLog queries.
type Result struct {
	Data        []map[string]interface{}
	Count       int
	Type        string
	Fields      map[string]interface{} // SCHEMAS (may be nil)
	OptStatus   string // SUCCESS, PARTIAL_RESULT — from deepflow-server
	Description string // e.g. "最大可查询时间为 1440 分钟"
}

// Envelope wraps the result in the standard DeepFlow response format.
func (r *Result) Envelope() map[string]interface{} {
	status := r.OptStatus
	if status == "" {
		status = "SUCCESS"
	}
	desc := r.Description
	if desc == "" {
		desc = ""
	}
	m := map[string]interface{}{
		"OPT_STATUS":  status,
		"DATA":        r.Data,
		"DESCRIPTION": desc,
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
