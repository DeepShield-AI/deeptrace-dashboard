package list

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"

	"deeptrace-backend/clickhouse"
)

// QueryList builds and executes a List query against ClickHouse.
// schemaMeta derives the SCHEMAS (value_type, type, unit) for a column,
// matching the cloud contract (verified against cloud.deepflow.yunshan.net):
// ID columns carry their physical CH types (auto_service_id UInt32,
// auto_service_type UInt8, icon_id Int64), auto_service is Nullable(String),
// metric columns are Nullable(Float64) with units (us/%/个/s), string tags
// stay String. Falls back to the scanned value type for unknown columns.
func schemaMeta(col string, v interface{}) (vt string, tp int, unit string) {
	low := strings.ToLower(col)
	switch {
	case low == "auto_service_id":
		return "UInt32", 0, ""
	case low == "auto_service_type":
		return "UInt8", 0, ""
	case low == "icon_id" || strings.HasSuffix(low, "_icon_id") || strings.HasPrefix(low, "client_icon_id") || strings.HasPrefix(low, "server_icon_id"):
		return "Int64", 0, ""
	case low == "auto_service" || low == "auto_instance":
		return "Nullable(String)", 0, ""
	case low == "resource_l7_protocol":
		return "String", 1, ""
	case low == "_id" || low == "query_id" || low == "node_type" || low == "_querier_region" || low == "endpoint" || low == "l7_protocol" || low == "role":
		return "String", 0, ""
	case strings.Contains(low, "时延") || strings.Contains(low, "rrt") || strings.Contains(low, "延迟"):
		return "Nullable(Float64)", 1, "us"
	case strings.Contains(low, "比例") || strings.Contains(low, "ratio"):
		return "Nullable(Float64)", 1, "%"
	case low == "count_row" || strings.Contains(low, "count") || strings.Contains(low, "行数") || strings.Contains(low, "个数"):
		// Count columns: cloud carries UInt64 with no unit.
		return "UInt64", 1, ""
	case strings.Contains(low, "速率") || strings.Contains(low, "次数"):
		return "Float64", 1, "个/s"
	}
	switch v.(type) {
	case float64, float32:
		return "Float64", 1, ""
	case int, int64, uint64:
		return "UInt64", 1, ""
	}
	return "String", 0, ""
}

func QueryList[T clickhouse.SqlRequest](ch clickhouse.Querier, ctx context.Context, req T) (*query.QueryListResult, error) {
	sql, err := clickhouse.BuildSelectSQL(req, ch)
	if err != nil {
		if errors.Is(err, clickhouse.ErrUnsupportedColumn) {
			// The table lacks a requested column — this signature is not
			// supported by CH. Return not-served so the chain falls through
			// (to cache) instead of failing the request.
			logging.Warnf("CH List unsupported (%v), falling through", err)
			return nil, nil
		}
		return nil, fmt.Errorf("build sql: %w", err)
	}
	logging.Debugf("CH List: %s", sql)

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := ch.Query(qCtx, sql)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		logging.Errorf("CH scan error: %v", err)
		return nil, fmt.Errorf("scan: %w", err)
	}
	if data == nil {
		data = []map[string]interface{}{}
	}

	preAsMap := map[string]string{}
	if req.GetNumQueries() > 0 && req.QueryAt(0).Select != "" {
		for _, item := range clickhouse.ParseSelectList(req.QueryAt(0).Select) {
			if item.Key != item.Expr {
				preAsMap[item.Key] = strings.ReplaceAll(item.Expr, "`", "")
			}
		}
	}

	schemas := map[string]interface{}{}
	if len(data) > 0 {
		for k, v := range data[0] {
			vt, tp, unit := schemaMeta(k, v)
			preAs := ""
			if p, ok := preAsMap[k]; ok {
				preAs = p
			}
			// resource_l7_protocol: keep the cloud's expression text in pre_as
			// (the frontend renders it; the local CH resolves via int_enum_map).
			if k == "resource_l7_protocol" {
				preAs = "arrayStringConcat(tupleElement(`array_resource_l7_protocol`,1),',')"
			}
			schemas[k] = map[string]interface{}{
				"label_type": "", "pre_as": preAs, "type": tp,
				"unit": unit, "value_type": vt,
			}
		}
	}

	count := len(data)
	if idx := strings.Index(sql, " FROM "); idx >= 0 {
		inner := sql[idx:]
		if oIdx := strings.Index(inner, " ORDER BY "); oIdx >= 0 {
			inner = inner[:oIdx]
		}
		if lIdx := strings.Index(inner, " LIMIT "); lIdx >= 0 {
			inner = inner[:lIdx]
		}
		// Wrap in a subquery: with GROUP BY in the original SQL, a bare
		// "SELECT count(*) ... GROUP BY" returns one row per group and we
		// would read only the first group's row count.
		countSQL := "SELECT count(*) AS cnt FROM (SELECT *" + inner + ")"
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
