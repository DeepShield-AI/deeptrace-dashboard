package query

import (
	"context"
	"encoding/json"
	"io"
)

// QueryTraceMap executes a TraceMap query through the DataSourceChain.
func (s *QuerierService) QueryTraceMap(ctx context.Context, body io.Reader) (*TraceMapResult, error) {
	// Parse request.
	var req QuerierListRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		// Return empty rather than erroring (chain will as well).
		return emptyTraceMapResult(), nil
	}

	// Try DataSourceChain.
	if s.Chain != nil {
		result, err := s.Chain.QueryTraceMap(ctx, &req)
		if err == nil && result != nil && len(result.NodeData) > 0 {
			return result, nil
		}
	}

	return emptyTraceMapResult(), nil
}

func emptyTraceMapResult() *TraceMapResult {
	return &TraceMapResult{
		NodeData: []map[string]interface{}{},
		ProgressInfo: map[string]interface{}{
			"total_traces_count":      0,
			"calculated_traces_count": 0,
		},
	}
}
