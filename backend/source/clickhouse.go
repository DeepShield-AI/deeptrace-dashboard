package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/enum"
	"deeptrace-backend/query"
)

// CHDataSource wraps CHService as a DataSource for the priority chain.
type CHDataSource struct {
	ch   *clickhouse.CHService
	enum *enum.EnumService
}

// NewCHDataSource creates a ClickHouse-backed data source.
func NewCHDataSource(ch *clickhouse.CHService) *CHDataSource {
	return &CHDataSource{ch: ch}
}

// SetEnumService sets the enum service for display name translation.
func (d *CHDataSource) SetEnumService(e *enum.EnumService) {
	d.enum = e
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
		Interval:   req.Interval,
		Fill:       req.Fill,
		DataSource: req.DataSource,
	}
	for _, q := range req.Queries {
		chReq.Queries = append(chReq.Queries, clickhouse.QuerierSub{
			QueryID: q.QueryID,
			Select:  q.Select,
			Where:   q.Where,
			Having:  q.Having,
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
		log.Printf("CH QueryList error: %v", err)
		return nil, err
	}
	if result == nil || len(result.Data) == 0 {
		return nil, nil
	}
	addRegion(result.Data)
	enrichListResults(result.Data, d.enum)
	return &query.Result{
		Data:   result.Data,
		Count:  result.Count,
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
	log.Printf("CH source: %d rows, first keys=%v", len(result.Data), func() []string { var kk []string; for k := range result.Data[0] { kk = append(kk, k) }; return kk }())
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

// enrichListResults post-processes CH List results to fill ZT-resolved fields.
// 1. Maps auto_service_type → node_type/icon_id for client/server sides.
// 2. Translates enum display names via int_enum_map when available.
func enrichListResults(data []map[string]interface{}, enumSvc *enum.EnumService) {
	if len(data) == 0 {
		return
	}

	for _, row := range data {
		// Resolve node_type and icon_id from auto_service_type.
		for _, side := range []string{"client_", "server_"} {
			typeKey := ""
			if side == "client_" {
				typeKey = "auto_service_type_0"
			} else {
				typeKey = "auto_service_type_1"
			}
			if typeVal, ok := row[typeKey]; ok {
				if t, ok2 := toInt(typeVal); ok2 {
					// Fill node_type if it's a raw literal (starts with "auto_service").
					nodeKey := side + "node_type"
					if nv, exists := row[nodeKey]; exists {
						if ns, ok3 := nv.(string); ok3 && (strings.HasPrefix(ns, "auto_service_") || ns == "_") {
							row[nodeKey] = clickhouse.NodeTypeFor(t)
						}
					}
					// Fill icon_id if still default -13/0.
					iconKey := side + "icon_id"
					if iv, exists := row[iconKey]; exists {
						if f, ok3 := toFloat(iv); ok3 && (f == -13 || f == 0) {
							row[iconKey] = clickhouse.IconFor(t)
						}
					}
				}
			}
		}

		// Translate Enum() columns using EnumService.
		if enumSvc != nil {
			for k := range row {
				if strings.HasPrefix(k, "Enum(") {
					inner := strings.TrimPrefix(k, "Enum(")
					inner = strings.TrimSuffix(inner, ")")
					inner = strings.TrimSpace(inner)
					if raw, ok := row[k].(float64); ok {
						row[k] = enumSvc.GetDisplay(inner, int64(raw))
					} else if raw, ok := row[k].(string); ok {
						row[k] = enumSvc.GetDisplay(inner, raw)
					}
				}
			}

			// Translate resource_l7_protocol_0/1 from numeric to display name.
			for _, pk := range []string{"resource_l7_protocol_0", "resource_l7_protocol_1"} {
				if raw, ok := row[pk]; ok {
					if f, ok2 := toFloat(raw); ok2 {
						row[pk] = enumSvc.GetDisplay("l7_protocol", fmt.Sprintf("%.0f", f))
					} else if s, ok2 := raw.(string); ok2 {
						row[pk] = enumSvc.GetDisplay("l7_protocol", s)
					}
				}
			}
		}
	}
}

// toFloat safely converts a value to float64.
func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case int:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

// toInt safely converts a value to int.
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return int(f), true
		}
	}
	return 0, false
}

// Ensure interface compliance.
var _ query.QuerierListSource = (*CHDataSource)(nil)
var _ query.QuerierTopSource = (*CHDataSource)(nil)
var _ query.TraceMapSource = (*CHDataSource)(nil)
