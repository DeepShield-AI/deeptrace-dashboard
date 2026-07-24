package query

import (
	"context"
)

// ---------------------------------------------------------------------------
// DataSource interfaces (strategy pattern)
// ---------------------------------------------------------------------------

// DataSource is the base interface for all data sources.
type DataSource interface {
	// Name returns a human-readable name for logging / metrics.
	Name() string
	// Enabled reports whether this data source is available.
	Enabled() bool
}

// QuerierListSource handles List-style aggregation queries.
type QuerierListSource interface {
	DataSource
	QueryList(ctx context.Context, req *QuerierListRequest) (*Result, error)
}

// QuerierTopSource handles TopN / ranking queries.
type QuerierTopSource interface {
	DataSource
	QueryTop(ctx context.Context, req *QuerierListRequest) (*Result, error)
}

// FlowLogDetailSource handles flow log detail queries.
type FlowLogDetailSource interface {
	DataSource
	QueryFlowLogDetail(ctx context.Context, req *QuerierListRequest) (*Result, error)
}

// TraceMapSource handles TraceMap queries.
type TraceMapSource interface {
	DataSource
	QueryTraceMap(ctx context.Context, req *QuerierListRequest) (*TraceMapResult, error)
}

// ---------------------------------------------------------------------------
// DataSourceChain — priority-based fallback chain
// ---------------------------------------------------------------------------

// DataSourceChain runs a list of data sources in priority order.
// The first source that returns a non-nil, non-error result wins.
type DataSourceChain struct {
	listSources       []QuerierListSource
	topSources        []QuerierTopSource
	flowLogSources    []FlowLogDetailSource
	traceMapSources   []TraceMapSource
}

// NewDataSourceChain creates an empty chain.
func NewDataSourceChain() *DataSourceChain {
	return &DataSourceChain{}
}

// AddListSource appends a List source (lower priority = later).
func (c *DataSourceChain) AddListSource(s QuerierListSource) {
	c.listSources = append(c.listSources, s)
}

// AddTopSource appends a Top source.
func (c *DataSourceChain) AddTopSource(s QuerierTopSource) {
	c.topSources = append(c.topSources, s)
}

// AddFlowLogSource appends a FlowLog detail source.
func (c *DataSourceChain) AddFlowLogSource(s FlowLogDetailSource) {
	c.flowLogSources = append(c.flowLogSources, s)
}

// AddTraceMapSource appends a TraceMap source.
func (c *DataSourceChain) AddTraceMapSource(s TraceMapSource) {
	c.traceMapSources = append(c.traceMapSources, s)
}

// QueryList walks the list sources in priority order.
func (c *DataSourceChain) QueryList(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	for _, s := range c.listSources {
		if !s.Enabled() {
			continue
		}
		result, err := s.QueryList(ctx, req)
		if err == nil && result != nil && len(result.Data) > 0 {
			return result, nil
		}
	}
	// Empty fallback (not an error — the caller distinguishes "no data" from "error").
	return &Result{
		Data:  []map[string]interface{}{},
		Type:  "Application_Detail_List",
	}, nil
}

// QueryTop walks the top sources in priority order.
func (c *DataSourceChain) QueryTop(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	for _, s := range c.topSources {
		if !s.Enabled() {
			continue
		}
		result, err := s.QueryTop(ctx, req)
		if err == nil && result != nil && len(result.Data) > 0 {
			return result, nil
		}
	}
	return &Result{
		Data:  []map[string]interface{}{},
	}, nil
}

// QueryFlowLogDetail walks the flow-log detail sources.
func (c *DataSourceChain) QueryFlowLogDetail(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	for _, s := range c.flowLogSources {
		if !s.Enabled() {
			continue
		}
		result, err := s.QueryFlowLogDetail(ctx, req)
		if err == nil && result != nil && len(result.Data) > 0 {
			return result, nil
		}
	}
	return &Result{
		Data:  []map[string]interface{}{},
		Type:  "Flow_Log_Detail_List",
	}, nil
}

// QueryTraceMap walks the trace-map sources.
func (c *DataSourceChain) QueryTraceMap(ctx context.Context, req *QuerierListRequest) (*TraceMapResult, error) {
	for _, s := range c.traceMapSources {
		if !s.Enabled() {
			continue
		}
		result, err := s.QueryTraceMap(ctx, req)
		if err == nil && result != nil {
			return result, nil
		}
	}
	return &TraceMapResult{
		NodeData: []map[string]interface{}{},
		ProgressInfo: map[string]interface{}{
			"total_traces_count":      0,
			"calculated_traces_count": 0,
		},
	}, nil
}

