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
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if n, err := strconv.Atoi(s); err == nil {
			*f = FlexInt(n)
			return nil
		}
	}
	return fmt.Errorf("cannot unmarshal %s into FlexInt", string(b))
}

// QuerierListQuery represents a single query inside a querier List request.
type QuerierListQuery struct {
	QueryID string   `json:"QUERY_ID"`
	Roles   []string `json:"ROLES"`
	Select  string   `json:"SELECT"`
	Where   string   `json:"WHERE"`
	Having  string   `json:"HAVING"`
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

// QueryListResult holds the result of a List query.
// QueryFlowLogResult holds the result of a FlowLogDetail query.
type QueryFlowLogResult struct {
	Data []map[string]interface{}
}

type QueryListResult struct {
	Data   []map[string]interface{}
	Fields map[string]interface{}
	Count  int
}

// QueryTopResult holds the result of a Top query.
type QueryTopResult struct {
	Data   []map[string]interface{}
	Fields map[string]interface{}
}

// QueryTraceMapResult holds the result of a TraceMap query.
type QueryTraceMapResult struct {
	Data             []map[string]interface{}
	TotalTraces      int
	CalculatedTraces int
}

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
	DataSource string             `json:"DATA_SOURCE"`
	Interval   int                `json:"interval"`
	WindowSize int                `json:"window_size"`
	Fill       string             `json:"fill"`

	// Flat Top format fields (not wrapped in QUERIES array).
	SelectField  string `json:"SELECT,omitempty"`
	WhereField   string `json:"WHERE,omitempty"`
	GroupByField string `json:"GROUP_BY,omitempty"`
	OrderByField string `json:"ORDER_BY,omitempty"`
	OrderDir     string `json:"ORDER,omitempty"`
	Limit        int    `json:"LIMIT,omitempty"`

	RawBody json.RawMessage `json:"-"`
}

// NormalizeQuery populates Queries[0] from flat SELECT/WHERE/GROUP_BY if QUERIES is empty.
func (r *QuerierListRequest) NormalizeQuery() {
	if len(r.Queries) == 0 && r.SelectField != "" {
		r.Queries = []QuerierListQuery{{
			Select:  r.SelectField,
			Where:   r.WhereField,
			GroupBy: r.GroupByField,
		}}
		// Map flat LIMIT → TOP.
		if r.Limit > 0 && r.Top == 0 {
			r.Top = FlexInt(r.Limit)
		}
		// Propagate flat ORDER_BY to Sort field.
		if r.OrderByField != "" && r.Sort == nil {
			r.Sort = &ListSort{OrderBy: r.OrderByField, SortedBy: r.OrderDir}
		}
	}
}

func (r *QuerierListRequest) UnmarshalJSON(b []byte) error {
	type Alias QuerierListRequest
	aux := &struct{ *Alias }{Alias: (*Alias)(r)}
	if err := json.Unmarshal(b, aux); err != nil {
		return err
	}
	// Preserve the raw body verbatim: cache matching depends on the exact
	// request shape (field presence/omission), which a marshal round-trip
	// would not reproduce.
	r.RawBody = append(r.RawBody[:0], b...)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	if r.TimeStart == 0 {
		if v, ok := raw["TIME_START"]; ok {
			json.Unmarshal(v, &r.TimeStart)
		}
	}
	if r.TimeEnd == 0 {
		if v, ok := raw["TIME_END"]; ok {
			json.Unmarshal(v, &r.TimeEnd)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Response types (consolidated)
// ---------------------------------------------------------------------------

// Envelope is implemented by response types that wrap themselves in the
// DeepFlow wire format (OPT_STATUS/DATA/DESCRIPTION + conditional fields).
// All writers (writeResult, writeTraceMap, ...) dispatch through it.
type Envelope interface {
	Envelope() map[string]interface{}
}

// DBDescriptionResponse is the envelope shared by ShowTagValues / ShowMetrics:
// fixed TYPE "DBDescription" and an always-present empty SCHEMAS object.
type DBDescriptionResponse struct {
	Data    interface{}
	Schemas map[string]interface{}
}

// Envelope implements Envelope.
func (r DBDescriptionResponse) Envelope() map[string]interface{} {
	schemas := r.Schemas
	if schemas == nil {
		schemas = map[string]interface{}{}
	}
	return map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
		"DATA":        r.Data,
		"TYPE":        "DBDescription",
		"SCHEMAS":     schemas,
	}
}

// FlowLogDetailInfoResponse is the FlowLogDetailInfo envelope — it omits
// COUNT (confirmed from the real API) and adds SCHEMAS only when present.
type FlowLogDetailInfoResponse struct {
	Data   []map[string]interface{}
	Type   string
	Fields map[string]interface{}
}

// Envelope implements Envelope.
func (r FlowLogDetailInfoResponse) Envelope() map[string]interface{} {
	env := map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DATA":        r.Data,
		"TYPE":        r.Type,
		"DESCRIPTION": "",
	}
	if r.Fields != nil {
		env["SCHEMAS"] = r.Fields
	}
	return env
}

// FastListResponse is the fast_list envelope: OPT_STATUS + DATA with no
// DESCRIPTION, plus an optional _debug object.
type FastListResponse struct {
	Data  interface{}
	Debug interface{} // nil → omitted
}

// Envelope implements Envelope.
func (r FastListResponse) Envelope() map[string]interface{} {
	env := map[string]interface{}{
		"OPT_STATUS": "SUCCESS",
		"DATA":       r.Data,
	}
	if r.Debug != nil {
		env["_debug"] = r.Debug
	}
	return env
}

// Result is the standard response for List/Top/Histogram/FlowLog queries.
type Result struct {
	Data        []map[string]interface{}
	Count       int
	Type        string
	Fields      map[string]interface{} // SCHEMAS (may be nil)
	OptStatus   string                 // SUCCESS, PARTIAL_RESULT — from deepflow-server
	Description string                 // e.g. "最大可查询时间为 1440 分钟"
	// TotalRequested marks a request that asked for TOTAL: the envelope then
	// always emits COUNT (including 0), per the api_cache contract.
	TotalRequested bool
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
	if r.Count > 0 || r.TotalRequested {
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

// ---------------------------------------------------------------------------
// Profile (flame graph)
// ---------------------------------------------------------------------------

// ProfileRequest is the body of a querier Profile (flame-graph) request.
// It has no QUERIES/DATABASE/TABLE fields — filters are flat fields plus a
// raw tag_filter string (e.g. "gprocess_id=10074").
type ProfileRequest struct {
	TimeStart           int64  `json:"time_start"`
	TimeEnd             int64  `json:"time_end"`
	Region              string `json:"region"`
	AppService          string `json:"app_service"`
	ProfileLanguageType string `json:"profile_language_type"`
	ProfileEventType    string `json:"profile_event_type"`
	TagFilter           string `json:"tag_filter"`

	// RawBody preserves the request verbatim for cache matching, mirroring
	// QuerierListRequest.UnmarshalJSON.
	RawBody json.RawMessage `json:"-"`
}

// UnmarshalJSON preserves the raw body verbatim (cache matching depends on
// the exact request shape; mirrors QuerierListRequest.UnmarshalJSON).
func (r *ProfileRequest) UnmarshalJSON(b []byte) error {
	type Alias ProfileRequest
	aux := &struct{ *Alias }{Alias: (*Alias)(r)}
	if err := json.Unmarshal(b, aux); err != nil {
		return err
	}
	r.RawBody = append(r.RawBody[:0], b...)
	return nil
}

// ProfileValueTable is the columns/values pair of function_values/node_values.
type ProfileValueTable struct {
	Columns []string        `json:"columns"`
	Values  [][]interface{} `json:"values"`
}

// ProfileResult is the flame-graph result: deduped functions, per-node tree
// rows, and per-function self/total aggregates. The wire shape (functions /
// function_types / function_values / node_values) is fixed by the real API
// contract — no DATA/SCHEMAS/TYPE keys.
type ProfileResult struct {
	Functions      []string          `json:"functions"`
	FunctionTypes  []string          `json:"function_types"`
	FunctionValues ProfileValueTable `json:"function_values"`
	NodeValues     ProfileValueTable `json:"node_values"`
}

// EmptyProfileResult returns a handled-but-empty Profile result (M4): non-nil
// so the chain stops here instead of falling through to the next source.
func EmptyProfileResult() *ProfileResult {
	return &ProfileResult{
		Functions:     []string{},
		FunctionTypes: []string{},
		FunctionValues: ProfileValueTable{
			Columns: []string{"self_value", "total_value"},
			Values:  [][]interface{}{},
		},
		NodeValues: ProfileValueTable{
			Columns: []string{"function_id", "parent_node_id", "self_value", "total_value"},
			Values:  [][]interface{}{},
		},
	}
}

// Envelope implements Envelope. The Profile wire format has no DATA key and
// carries the flame-graph under "result" (replay tool flags any DATA key).
func (r *ProfileResult) Envelope() map[string]interface{} {
	if r == nil {
		r = EmptyProfileResult()
	}
	return map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
		"result":      r,
		"debug":       nil,
	}
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
