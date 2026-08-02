package source

import (
	"context"
	"fmt"
	"strings"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/enum"
	"deeptrace-backend/logging"
	"deeptrace-backend/query"
	"deeptrace-backend/query/list"
	"deeptrace-backend/query/profile"
	"deeptrace-backend/query/top"
	"deeptrace-backend/query/tracemap"
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

// QueryList implements QuerierListSource. QuerierListRequest implements
// clickhouse.SqlRequest directly, so no field-mapping bridge is needed.
func (d *CHDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if !d.Enabled() {
		return nil, nil
	}
	result, err := list.QueryList(d.ch, ctx, req)
	if err != nil {
		logging.Errorf("CH QueryList error: %v", err)
		return nil, err
	}
	if result == nil {
		return nil, nil // not supported — the chain tries the next source
	}
	if len(result.Data) == 0 {
		// CH handled the query and it is genuinely empty: stop the chain
		// with an empty result (M4) instead of falling through to cache.
		return &query.Result{
			Data:   []map[string]interface{}{},
			Count:  result.Count,
			Fields: result.Fields,
			Type:   "Application_Detail_List",
		}, nil
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
	result, err := top.QueryTop(d.ch, ctx, req)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil // not supported — the chain tries the next source
	}
	if len(result.Data) == 0 {
		// CH handled the query and it is genuinely empty: stop the chain
		// with an empty result (M4) instead of falling through to cache.
		return &query.Result{Data: []map[string]interface{}{}, Type: "Application_Detail_Top"}, nil
	}
	addRegion(result.Data)
	logging.Infof("CH source: %d rows, first keys=%v", len(result.Data), func() []string {
		var kk []string
		for k := range result.Data[0] {
			kk = append(kk, k)
		}
		return kk
	}())
	return &query.Result{
		Data:   result.Data,
		Fields: result.Fields,
	}, nil
}

// QueryProfile implements ProfileSource. The raw profile rows are scanned
// and aggregated into the flame-graph tree by query/profile.
func (d *CHDataSource) QueryProfile(ctx context.Context, req *query.ProfileRequest) (*query.ProfileResult, error) {
	if !d.Enabled() {
		return nil, nil
	}
	result, err := profile.QueryProfile(d.ch, ctx, req)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil // unsupported signature — the chain tries the next source
	}
	// Non-nil (including empty) means CH handled the query: stop the chain (M4).
	return result, nil
}

// QueryTraceMap implements TraceMapSource.
func (d *CHDataSource) QueryTraceMap(ctx context.Context, req *query.QuerierListRequest) (*query.TraceMapResult, error) {
	if d.ch == nil || !d.ch.Enabled() {
		return nil, nil
	}
	result, err := tracemap.QueryTraceMap(d.ch, ctx, req.TimeStart, req.TimeEnd, "")
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
			row["_querier_region"] = clickhouse.QuerierRegion
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
				if t, ok2 := clickhouse.ToIntOK(typeVal); ok2 {
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
						if f, ok3 := clickhouse.ToFloat64(iv); ok3 && (f == -13 || f == 0) {
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
					if f, ok2 := clickhouse.ToFloat64(raw); ok2 {
						row[pk] = enumSvc.GetDisplay("l7_protocol", fmt.Sprintf("%.0f", f))
					} else if s, ok2 := raw.(string); ok2 {
						row[pk] = enumSvc.GetDisplay("l7_protocol", s)
					}
				}
			}
		}
	}
}

// Ensure interface compliance.
var (
	_ query.QuerierListSource = (*CHDataSource)(nil)
	_ query.QuerierTopSource  = (*CHDataSource)(nil)
	_ query.TraceMapSource    = (*CHDataSource)(nil)
	_ query.ProfileSource     = (*CHDataSource)(nil)
)
