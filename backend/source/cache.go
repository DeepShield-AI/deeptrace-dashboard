package source

import (
	"context"
	"encoding/json"

	"deeptrace-backend/cache"
	"deeptrace-backend/query"
)

// CacheDataSource serves cached real API responses from api_cache/ as the
// final link of the chain (M4: ZT → ClickHouse → exact cache → empty).
// Under a forced no-fallback policy the chain skips it via policy matching.
type CacheDataSource struct {
	c *cache.Cache
}

// NewCacheDataSource creates a new cache data source.
func NewCacheDataSource(c *cache.Cache) *CacheDataSource {
	return &CacheDataSource{c: c}
}

func (d *CacheDataSource) Name() string  { return "cache" }
func (d *CacheDataSource) Enabled() bool { return d.c != nil && d.c.Len() > 0 }

// QueryList looks up a cached querier List response using body-aware matching.
func (d *CacheDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	raw := d.lookup("POST", "/api/statistics/v1/stats/querier/List", req)
	if raw == nil {
		return nil, nil
	}
	return d.DecodeCacheResponse(raw)
}

// QueryTop looks up a cached querier Top response using body-aware matching.
func (d *CacheDataSource) QueryTop(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	raw := d.lookup("POST", "/api/statistics/v1/stats/querier/Top", req)
	if raw == nil {
		return nil, nil
	}
	return d.DecodeCacheResponse(raw)
}

// QueryProfile reads the cached Profile response using exact body matching
// (the Profile body has no DATABASE/TABLE/QUERIES, so structured scoring can
// never match — only the verbatim body hits). The cached response carries the
// flame-graph under "result", which decodes straight into ProfileResult.
func (d *CacheDataSource) QueryProfile(ctx context.Context, req *query.ProfileRequest) (*query.ProfileResult, error) {
	if d.c == nil || len(req.RawBody) == 0 {
		return nil, nil
	}
	raw := d.c.FindWithBody("POST", "/api/statistics/v1/stats/querier/Profile", string(req.RawBody))
	if raw == nil {
		return nil, nil
	}
	var resp struct {
		Result *query.ProfileResult `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Result == nil {
		return nil, nil // cache data problem → treat as miss (TraceMap precedent)
	}
	return resp.Result, nil
}

// QueryTraceMap reads the cached TraceMap response (path-only matching,
// since the cached body may have different time_start/time_end).
func (d *CacheDataSource) QueryTraceMap(ctx context.Context, req *query.QuerierListRequest) (*query.TraceMapResult, error) {
	if d.c == nil {
		return nil, nil
	}
	raw := d.c.Find("POST", "/api/statistics/v1/stats/querier/TraceMap")
	if raw == nil {
		return nil, nil
	}
	var resp struct {
		DATA struct {
			NodeData     []map[string]interface{} `json:"node_data"`
			ProgressInfo map[string]interface{}   `json:"progress_info"`
		} `json:"DATA"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, nil
	}
	if len(resp.DATA.NodeData) == 0 {
		return nil, nil
	}
	return &query.TraceMapResult{
		NodeData:     resp.DATA.NodeData,
		ProgressInfo: resp.DATA.ProgressInfo,
	}, nil
}

// lookup performs a body-aware cache lookup. The raw request body is used
// when available (cache matching depends on the exact request shape);
// otherwise the request is serialized back.
func (d *CacheDataSource) lookup(method, path string, req *query.QuerierListRequest) []byte {
	if d.c == nil {
		return nil
	}
	body := req.RawBody
	if len(body) == 0 {
		var err error
		body, err = json.Marshal(req)
		if err != nil {
			return nil
		}
	}
	return d.c.FindWithBody(method, path, string(body))
}

// Ensure interface compliance.
var (
	_ query.QuerierListSource = (*CacheDataSource)(nil)
	_ query.QuerierTopSource  = (*CacheDataSource)(nil)
	_ query.TraceMapSource    = (*CacheDataSource)(nil)
	_ query.ProfileSource     = (*CacheDataSource)(nil)
)

// DecodeCacheResponse parses a cached response body into a Result.
func (d *CacheDataSource) DecodeCacheResponse(data []byte) (*query.Result, error) {
	var raw struct {
		Data   []map[string]interface{} `json:"DATA"`
		Count  int                      `json:"COUNT"`
		Type   string                   `json:"TYPE"`
		Fields map[string]interface{}   `json:"SCHEMAS"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return &query.Result{
		Data:   raw.Data,
		Count:  raw.Count,
		Type:   raw.Type,
		Fields: raw.Fields,
	}, nil
}
