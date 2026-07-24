package query

import (
	"context"

	"deeptrace-backend/engine"
)

// QueryList executes a List query through the DataSourceChain with fallbacks.
func (s *QuerierService) QueryList(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	// 1. Try DataSourceChain (cache → mock → CH → zerotrace → aggregator).
	if s.Chain != nil {
		result, err := s.Chain.QueryList(ctx, req)
		if err == nil && result != nil && len(result.Data) > 0 {
			result.Count = len(result.Data)
			return result, nil
		}
	}

	// 2. Legacy: direct Zerotrace fallback.
	if s.Zerotrace != nil {
		sql := buildCHSQL(*req)
		if sql != "" {
			rows, err := s.Zerotrace.Query(req.Database, sql)
			if err == nil && len(rows) > 0 {
				return &Result{
					Data:   rows,
					Count:  len(rows),
					Type:   "Application_Detail_List",
					Fields: engine.BuildSchemas(rows[0]),
				}, nil
			}
		}
	}

	// 3. Empty fallback.
	return &Result{
		Data:  []map[string]interface{}{},
		Count: 0,
		Type:  "Application_Detail_List",
	}, nil
}

// buildCHSQL extracts the SELECT query from a QuerierListRequest.
func buildCHSQL(req QuerierListRequest) string {
	if len(req.Queries) == 0 {
		return ""
	}
	return req.Queries[0].Select
}
