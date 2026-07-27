package source

import (
	"context"
	"encoding/json"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/query"
)

// CHDataSource wraps CHService as a DataSource for the priority chain.
type CHDataSource struct {
	ch *clickhouse.CHService
}

// NewCHDataSource creates a ClickHouse-backed data source.
func NewCHDataSource(ch *clickhouse.CHService) *CHDataSource {
	return &CHDataSource{ch: ch}
}

func (d *CHDataSource) Name() string { return "ClickHouse" }
func (d *CHDataSource) Enabled() bool {
	return d.ch != nil && d.ch.Enabled()
}

// toCHRequest converts a QuerierListRequest to a clickhouse.QuerierRequest.
func toCHRequest(req *query.QuerierListRequest) *clickhouse.QuerierRequest {
	if req == nil {
		return nil
	}
	chReq := &clickhouse.QuerierRequest{
		Database:  req.Database,
		Table:     req.Table,
		PageIndex: req.PageIndex,
		PageSize:  req.PageSize,
		TimeStart: req.TimeStart,
		TimeEnd:   req.TimeEnd,
	}
	for _, q := range req.Queries {
		chReq.Queries = append(chReq.Queries, clickhouse.QuerierSub{
			QueryID: q.QueryID,
			Select:  q.Select,
			Where:   q.Where,
			Tags:    q.Tags,
			Metrics: q.Metrics,
			GroupBy: q.GroupBy,
		})
	}
	return chReq
}

// QueryList implements QuerierListSource.
func (d *CHDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if !d.Enabled() {
		return nil, nil
	}
	chReq := toCHRequest(req)
	result, err := d.ch.QueryList(ctx, chReq)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Data) == 0 {
		return nil, nil
	}
	addRegion(result.Data)
	return &query.Result{
		Data:   result.Data,
		Count:  len(result.Data),
		Fields: result.Fields,
		Type:   "Application_Detail_List",
	}, nil
}

// QueryTop implements QuerierTopSource.
func (d *CHDataSource) QueryTop(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.ch == nil || !d.ch.Enabled() {
		return nil, nil
	}
	chReq := toCHRequest(req)
	result, err := d.ch.QueryTop(ctx, chReq)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Data) == 0 {
		return nil, nil
	}
	addRegion(result.Data)
	return &query.Result{
		Data:   result.Data,
		Fields: result.Fields,
	}, nil
}

func (d *CHDataSource) QueryFlowLogDetail(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.ch == nil || !d.ch.Enabled() {
		return nil, nil
	}
	bodyBytes, _ := json.Marshal(req)
	result, err := d.ch.QueryFlowLogDetail(ctx, string(bodyBytes))
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Data) == 0 {
		return nil, nil
	}
	// Build SCHEMAS from first row.
	schemas := map[string]interface{}{}
	if len(result.Data) > 0 {
		for k, v := range result.Data[0] {
			vt, tp := "String", 0
			switch v.(type) {
			case float64:
				vt, tp = "Float64", 1
			case float32:
				vt, tp = "Float64", 1
			case int, int64, uint64:
				vt, tp = "UInt64", 1
			}
			schemas[k] = map[string]interface{}{
				"label_type": "", "pre_as": "", "type": tp,
				"unit": "", "value_type": vt,
			}
		}
	}
	return &query.Result{
		Data:   result.Data,
		Count:  len(result.Data),
		Fields: schemas,
	}, nil
}

// QueryTraceMap implements TraceMapSource.
func (d *CHDataSource) QueryTraceMap(ctx context.Context, req *query.QuerierListRequest) (*query.TraceMapResult, error) {
	if d.ch == nil || !d.ch.Enabled() {
		return nil, nil
	}
	result, err := d.ch.QueryTraceMap(ctx, req.TimeStart, req.TimeEnd, "")
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Data) == 0 {
		return nil, nil
	}
	return &query.TraceMapResult{
		NodeData: result.Data,
	}, nil
}

// addRegion adds _querier_region to each data row.
func addRegion(data []map[string]interface{}) {
	for _, row := range data {
		if _, ok := row["_querier_region"]; !ok {
			row["_querier_region"] = "本地"
		}
	}
}

// Ensure interface compliance.
var _ query.QuerierListSource = (*CHDataSource)(nil)
var _ query.QuerierTopSource = (*CHDataSource)(nil)
var _ query.TraceMapSource = (*CHDataSource)(nil)
