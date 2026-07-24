package source

import (
	"context"

	"deeptrace-backend/client"
	"deeptrace-backend/query"
)

// ---------------------------------------------------------------------------
// Zerotrace external-service data source
// ---------------------------------------------------------------------------

// ZerotraceDataSource wraps client.ZerotraceService as a DataSource.
type ZerotraceDataSource struct {
	zt *client.ZerotraceService
}

// NewZerotraceDataSource creates a new zerotrace data source.
func NewZerotraceDataSource(zt *client.ZerotraceService) *ZerotraceDataSource {
	return &ZerotraceDataSource{zt: zt}
}

func (d *ZerotraceDataSource) Name() string  { return "zerotrace" }
func (d *ZerotraceDataSource) Enabled() bool { return d.zt != nil }

func (d *ZerotraceDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.zt == nil {
		return nil, nil
	}
	if req == nil || len(req.Queries) == 0 {
		return nil, nil
	}
	sql := req.Queries[0].Select
	if sql == "" {
		return nil, nil
	}

	rows, err := d.zt.Query(req.Database, sql)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Add _querier_region to each row.
	for _, row := range rows {
		if _, ok := row["_querier_region"]; !ok {
			row["_querier_region"] = "本地"
		}
	}

	// Build SCHEMAS from the first row.
	schemas := map[string]interface{}{}
	for k, v := range rows[0] {
		vt, tp := "String", 0
		switch v.(type) {
		case float64:
			vt, tp = "Float64", 1
		case int:
			vt, tp = "UInt64", 1
		}
		schemas[k] = map[string]interface{}{
			"label_type": "", "pre_as": "", "type": tp,
			"unit": "", "value_type": vt,
		}
	}

	return &query.Result{
		Data:   rows,
		Count:  len(rows),
		Type:   "Application_Detail_List",
		Fields: schemas,
	}, nil
}

// Ensure imports are used.
