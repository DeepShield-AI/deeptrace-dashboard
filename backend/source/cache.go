package source

import (
	"context"
	"encoding/json"

	"deeptrace-backend/cache"
	"deeptrace-backend/query"
)

// ---------------------------------------------------------------------------
// Cache data source — serves cached API responses from api_cache/
// ---------------------------------------------------------------------------

// CacheDataSource wraps cache.Cache as a DataSource.
// It provides trace-related data from cached real API responses when
// the primary data sources (ClickHouse, Zerotrace) are not available.
type CacheDataSource struct {
	c *cache.Cache
}

// NewCacheDataSource creates a new cache data source.
func NewCacheDataSource(c *cache.Cache) *CacheDataSource {
	return &CacheDataSource{c: c}
}

func (d *CacheDataSource) Name() string  { return "cache" }
func (d *CacheDataSource) Enabled() bool { return d.c != nil && d.c.Len() > 0 }

// ---------------------------------------------------------------------------
// List — reads from cached List response
// ---------------------------------------------------------------------------

// QueryList looks up a cached querier List response using body-aware matching.
func (d *CacheDataSource) QueryList(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.c == nil {
		return nil, nil
	}
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, nil
	}
	raw := d.c.FindWithBody("POST", "/api/statistics/v1/stats/querier/List", string(bodyBytes))
	if raw == nil {
		return nil, nil
	}
	return d.DecodeCacheResponse(raw)
}

// ---------------------------------------------------------------------------
// TraceMap — reads from cached TraceMap response
// ---------------------------------------------------------------------------

// QueryTraceMap reads the cached TraceMap response and returns node data.
// Uses path-only matching (ignores request body) since the cached body
// may have different time_start/time_end from the current request.
// ---------------------------------------------------------------------------
// Top — reads from cached Top response
// ---------------------------------------------------------------------------

// QueryTop looks up a cached querier Top response using body-aware matching.
func (d *CacheDataSource) QueryTop(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.c == nil {
		return nil, nil
	}
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, nil
	}
	raw := d.c.FindWithBody("POST", "/api/statistics/v1/stats/querier/Top", string(bodyBytes))
	if raw == nil {
		return nil, nil
	}
	return d.DecodeCacheResponse(raw)
}

// ---------------------------------------------------------------------------
// TraceMap — reads from cached TraceMap response
// ---------------------------------------------------------------------------

func (d *CacheDataSource) QueryTraceMap(ctx context.Context, req *query.QuerierListRequest) (*query.TraceMapResult, error) {
	if d.c == nil {
		return nil, nil
	}
	// Look up using path only (body-aware matching may fail due to time range differences).
	raw := d.c.Find("POST", "/api/statistics/v1/stats/querier/TraceMap")
	if raw == nil {
		return nil, nil
	}

	var resp struct {
		TYPE   string `json:"TYPE"`
		DATA   struct {
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

// ---------------------------------------------------------------------------
// FlowLog detail — reads from cached FlowLogDetailList response
// ---------------------------------------------------------------------------

// QueryFlowLogDetail reads the cached FlowLogDetailList response.
func (d *CacheDataSource) QueryFlowLogDetail(ctx context.Context, req *query.QuerierListRequest) (*query.Result, error) {
	if d.c == nil {
		return nil, nil
	}
	raw := d.c.Find("POST", "/api/statistics/v1/stats/querier/FlowLogDetailList")
	if raw == nil {
		return nil, nil
	}
	return d.DecodeCacheResponse(raw)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// FindRaw performs a raw cache lookup (used by handlers that have the actual URL path).
func (d *CacheDataSource) FindRaw(method, path, body string) []byte {
	if d.c == nil {
		return nil
	}
	return d.c.FindWithBody(method, path, body)
}

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

// ensure imports used
