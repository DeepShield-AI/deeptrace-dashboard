package source

import (
	"context"
	"encoding/json"

	"deeptrace-backend/client"
	"deeptrace-backend/query"
	"deeptrace-backend/query/flowlog"
)

type ZerotraceDataSource struct {
	zt *client.ZerotraceService
}

func NewZerotraceDataSource(zt *client.ZerotraceService) *ZerotraceDataSource {
	return &ZerotraceDataSource{zt: zt}
}

func (d *ZerotraceDataSource) Name() string  { return "zerotrace" }
func (d *ZerotraceDataSource) Enabled() bool { return d.zt != nil }

// rawBody returns the request's original JSON — unknown frontend fields must
// survive (M1 Step 2), and a Marshal round-trip would drop them while
// injecting zero-valued fields.
func (d *ZerotraceDataSource) rawBody(req *query.QuerierListRequest) []byte {
	if len(req.RawBody) > 0 {
		return req.RawBody
	}
	body, _ := json.Marshal(req)
	return body
}

func (d *ZerotraceDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.zt == nil {
		return nil, nil
	}
	return flowlog.QueryListZT(d.zt, string(d.rawBody(req)))
}

func (d *ZerotraceDataSource) QueryTop(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.zt == nil {
		return nil, nil
	}
	return flowlog.QueryTopZT(d.zt, string(d.rawBody(req)))
}

var (
	_ query.QuerierListSource = (*ZerotraceDataSource)(nil)
	_ query.QuerierTopSource  = (*ZerotraceDataSource)(nil)
)
