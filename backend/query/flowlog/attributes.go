package flowlog

import (
	"context"
	"time"

	"deeptrace-backend/clickhouse"
)

// ShowAttributesResult wraps the result of a ShowAttributes query.
type ShowAttributesResult struct {
	Data  []map[string]interface{} `json:"DATA"`
	Count int                      `json:"COUNT"`
}

// QueryShowAttributes queries ClickHouse for span attribute names/values for a given table.
// Returns nil if ClickHouse is unavailable.
func QueryShowAttributes(ch *clickhouse.CHService, database, table string) (*ShowAttributesResult, error) {
	if ch == nil || !ch.Enabled() {
		return nil, nil
	}

	sql := clickhouse.BuildShowAttributesSQL(database, table)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := ch.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		return nil, err
	}
	return &ShowAttributesResult{Data: data, Count: len(data)}, nil
}
