package query

import (
	"context"
	"fmt"
	"strings"

	"deeptrace-backend/clickhouse"
)

// QueryTop executes a TopN query through the DataSourceChain with fallbacks.
func (s *QuerierService) QueryTop(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	// 1. Try DataSourceChain.
	if s.Chain != nil {
		result, err := s.Chain.QueryTop(ctx, req)
		if err == nil && result != nil && len(result.Data) > 0 {
			// Fill extra fields the frontend expects (query_id, node_type, icon_id, etc.).
			if len(req.Queries) > 0 {
				fillTopExtraFields(req.Queries[0].Select, result.Data)
			}
			return result, nil
		}
	}

	// 2. Empty fallback.
	return &Result{
		Data: []map[string]interface{}{},
		Type: "Application_Detail_Top",
	}, nil
}

// QueryTopForProfile is a convenience wrapper for Profile queries
// that doesn't add the TYPE field (profile responses use a different format).
func (s *QuerierService) QueryTopForProfile(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	if s.Chain != nil {
		result, err := s.Chain.QueryTop(ctx, req)
		if err == nil && result != nil && len(result.Data) > 0 {
			result.Count = len(result.Data)
			return result, nil
		}
	}
	return &Result{
		Data:  []map[string]interface{}{},
		Count: 0,
	}, nil
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
