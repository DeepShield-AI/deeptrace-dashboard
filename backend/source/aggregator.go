package source

import (
	"context"

	"deeptrace-backend/aggregator"
	"deeptrace-backend/query"
)

// ---------------------------------------------------------------------------
// Aggregator data source — serves data aggregated from traces.json
// ---------------------------------------------------------------------------

// AggregatorDataSource wraps Aggregator as a DataSource.
type AggregatorDataSource struct {
	agg *aggregator.Aggregator
}

// NewAggregatorDataSource creates a new aggregator data source.
func NewAggregatorDataSource(agg *aggregator.Aggregator) *AggregatorDataSource {
	return &AggregatorDataSource{agg: agg}
}

func (d *AggregatorDataSource) Name() string  { return "aggregator" }
func (d *AggregatorDataSource) Enabled() bool { return d.agg != nil }

func (d *AggregatorDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.agg == nil {
		return nil, nil
	}
	if req == nil || len(req.Queries) == 0 {
		return nil, nil
	}
	rows := d.agg.AggregateList(*req)
	if len(rows) == 0 {
		return nil, nil
	}
	return &query.Result{
		Data:  rows,
		Count: len(rows),
		Type:  "Application_Detail_List",
	}, nil
}

func (d *AggregatorDataSource) QueryTraceMap(ctx context.Context, req *query.QuerierListRequest) (*query.TraceMapResult, error) {
	if d.agg == nil {
		return nil, nil
	}
	nodes := d.agg.BuildTraceMapNodes()
	if len(nodes) == 0 {
		return nil, nil
	}
	return &query.TraceMapResult{
		NodeData: nodes,
		ProgressInfo: map[string]interface{}{
			"total_traces_count":      len(nodes),
			"calculated_traces_count": len(nodes),
		},
	}, nil
}
