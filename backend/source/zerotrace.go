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

func (d *ZerotraceDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.zt == nil { return nil, nil }
	body, _ := json.Marshal(req)
	return flowlog.QueryListZT(d.zt, string(body))
}

func (d *ZerotraceDataSource) QueryTop(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.zt == nil { return nil, nil }
	body, _ := json.Marshal(req)
	return flowlog.QueryTopZT(d.zt, string(body))
}

var _ query.QuerierListSource = (*ZerotraceDataSource)(nil)
var _ query.QuerierTopSource = (*ZerotraceDataSource)(nil)
