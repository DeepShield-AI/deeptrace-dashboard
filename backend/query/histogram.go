package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"deeptrace-backend/clickhouse"
)

// QueryHistogram executes a histogram (time distribution / flame graph) query
// through ClickHouse directly.
func (s *QuerierService) QueryHistogram(ctx context.Context, bodyStr string) (*Result, error) {
	if s.CH == nil || !s.CH.Enabled() {
		return emptyHistogramResult(), nil
	}

	var req clickhouse.QuerierRequest
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	sql, err := clickhouse.BuildHistogramSQL(clickhouse.HistogramRequest{
		Database:  req.Database,
		Table:     req.Table,
		TimeStart: req.TimeStart,
		TimeEnd:   req.TimeEnd,
	})
	if err != nil {
		return emptyHistogramResult(), nil
	}

	log.Printf("CH Histogram SQL: %s", sql)
	histCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := s.CH.Query(histCtx, sql)
	if err != nil {
		return emptyHistogramResult(), nil
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		return emptyHistogramResult(), nil
	}

	return &Result{
		Data:  data,
		Count: len(data),
		Type:  "Histogram",
	}, nil
}

func emptyHistogramResult() *Result {
	return &Result{
		Data:  []map[string]interface{}{},
		Count: 0,
		Type:  "Histogram",
	}
}
