package list

import (
	"deeptrace-backend/query"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
)

// QueryList builds and executes a List query against ClickHouse.
func QueryList(ch *clickhouse.CHService, ctx context.Context, req *clickhouse.QuerierRequest) (*query.QueryListResult, error) {
	sql, err := clickhouse.BuildSelectSQL(*req)
	if err != nil {
		return nil, fmt.Errorf("build sql: %w", err)
	}
	log.Printf("CH List: %s", sql)

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := ch.Query(qCtx, sql)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		log.Printf("CH scan error: %v", err)
		return nil, fmt.Errorf("scan: %w", err)
	}
	if data == nil {
		data = []map[string]interface{}{}
	}

	preAsMap := map[string]string{}
	if len(req.Queries) > 0 && req.Queries[0].Select != "" {
		for _, item := range clickhouse.ParseSelectList(req.Queries[0].Select) {
			if item.Key != item.Expr {
				preAsMap[item.Key] = strings.ReplaceAll(item.Expr, "`", "")
			}
		}
	}

	schemas := map[string]interface{}{}
	if len(data) > 0 {
		for k, v := range data[0] {
			vt, tp := "String", 0
			switch v.(type) {
			case float64, float32:
				vt, tp = "Float64", 1
			case int, int64, uint64:
				vt, tp = "UInt64", 1
				if strings.Contains(strings.ToLower(k), "icon_id") {
					tp = 0
					vt = "String"
				}
			}
			preAs := ""
			if p, ok := preAsMap[k]; ok {
				preAs = p
			}
			schemas[k] = map[string]interface{}{
				"label_type": "", "pre_as": preAs, "type": tp,
				"unit": "", "value_type": vt,
			}
		}
	}

	count := len(data)
	if idx := strings.Index(sql, " FROM "); idx >= 0 {
		countSQL := "SELECT count(*) AS cnt" + sql[idx:]
		if oIdx := strings.Index(countSQL, " ORDER BY "); oIdx >= 0 {
			countSQL = countSQL[:oIdx]
		}
		if lIdx := strings.Index(countSQL, " LIMIT "); lIdx >= 0 {
			countSQL = countSQL[:lIdx]
		}
		cRows, cErr := ch.Query(ctx, countSQL)
		if cErr == nil {
			if cData, cScanErr := clickhouse.ScanRows(cRows); cScanErr == nil && len(cData) > 0 {
				if cv, ok := cData[0]["cnt"]; ok {
					if f, ok2 := cv.(float64); ok2 {
						count = int(f)
					}
				}
			}
			if cRows != nil {
				cRows.Close()
			}
		}
	}
	return &query.QueryListResult{
		Data:   data,
		Fields: schemas,
		Count:  count,
	}, nil
}
