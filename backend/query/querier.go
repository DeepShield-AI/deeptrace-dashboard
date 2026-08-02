package query

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/enum"
)

// QuerierService is the single entry point for all query-related business logic.
type QuerierService struct {
	Chain     *DataSourceChain
	CH        *clickhouse.CHService
	Zerotrace *client.ZerotraceService
	Enum      *enum.EnumService
}

func (s *QuerierService) QueryList(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	// 1. Try DataSourceChain (ZT → CH). The chain stops at the first source
	//    that handles the query — empty results are final (M4), errors bubble
	//    up for the handler to turn into 502 under a forced no-fallback policy.
	if s.Chain != nil {
		result, err := s.Chain.QueryList(ctx, req)
		if err != nil {
			return nil, err
		}
		if result != nil {
			// Preserve the real total COUNT computed by the source (CH path
			// runs a dedicated count query); only fill it from the returned
			// rows when the source didn't provide one (ZT path).
			if result.Count == 0 {
				result.Count = len(result.Data)
			}
			return result, nil
		}
	}

	// 2. Empty fallback.
	return &Result{
		Data:  []map[string]interface{}{},
		Count: 0,
		Type:  "Application_Detail_List",
	}, nil
}

func (s *QuerierService) QueryTop(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	// 1. Try DataSourceChain.
	if s.Chain != nil {
		result, err := s.Chain.QueryTop(ctx, req)
		if err != nil {
			return nil, err
		}
		if result != nil {
			// Fill extra fields the frontend expects (query_id, node_type, icon_id, etc.).
			if len(req.Queries) > 0 {
				fillTopExtraFields(req.Queries[0].Select, result.Data)
				if req.IncludeHis && req.Interval > 0 && len(result.Data) > 0 {
					// If data already has HISTORY (from CH post-processing), skip BuildHistory.
					if _, hasHist := result.Data[0]["HISTORY"]; !hasHist {
						result.Data = BuildHistory(result.Data, req.Queries[0].Select,
							req.Queries[0].Metrics, req.TimeStart, req.TimeEnd, int64(req.Interval), req.Fill)
					}
				}
			}
			return result, nil
		}
	}

	// 2. Empty fallback.
	return &Result{
		Data: []map[string]interface{}{},
	}, nil
}

// QueryProfile runs the Profile (flame-graph) chain. The Profile wire format
// differs from Top (result.functions/node_values, no DATA), so it has its own
// chain and result type.
func (s *QuerierService) QueryProfile(ctx context.Context, req *ProfileRequest) (*ProfileResult, error) {
	if s.Chain != nil {
		result, err := s.Chain.QueryProfile(ctx, req)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
	}
	return EmptyProfileResult(), nil
}

// fillTopExtraFields adds default values for SELECT items that ClickHouse can't compute
// (newTag, node_type, icon_id, Enum, numeric literals). These are expected in the response
// by the frontend but get skipped during SQL generation.
func fillTopExtraFields(sel string, data []map[string]interface{}) {
	if sel == "" || len(data) == 0 {
		return
	}
	items := clickhouse.ParseSelectList(sel)
	for _, item := range items {
		lower := strings.ToLower(item.Expr)
		var val interface{}
		needsFill := false
		switch {
		case strings.HasPrefix(lower, "newtag("):
			tagVal := strings.TrimSpace(item.Expr[len("newTag(") : len(item.Expr)-1])
			tagVal = strings.Trim(tagVal, "'\"")
			val = tagVal
			needsFill = true
		case strings.HasPrefix(lower, "node_type("):
			val = "_"
			needsFill = true
		case strings.HasPrefix(lower, "icon_id("):
			val = float64(-42)
			needsFill = true
		case strings.HasPrefix(lower, "enum("):
			inner := strings.TrimSpace(item.Expr[len("Enum(") : len(item.Expr)-1])
			val = nil
			needsFill = true
			if len(data) > 0 {
				if v, ok := data[0][inner]; ok {
					val = v
				}
			}
		default:
			// Check if it's a numeric literal (e.g., -42, 0.5)
			var n float64
			if _, err := fmt.Sscanf(item.Expr, "%f", &n); err == nil {
				val = n
				needsFill = true
			}
		}
		if needsFill {
			for _, row := range data {
				if _, exists := row[item.Key]; !exists {
					row[item.Key] = val
				}
			}
		}
	}
}

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
		if err != nil {
			return nil, err
		}
		if result != nil {
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
