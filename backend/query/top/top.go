package top

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"

	"deeptrace-backend/query/tracemap"

	"deeptrace-backend/clickhouse"
)

func QueryTop[T clickhouse.SqlRequest](ch clickhouse.Querier, ctx context.Context, req T) (*query.QueryTopResult, error) {
	db := req.GetDatabase()
	table := req.GetTable()
	if db == "" {
		db = "flow_log"
	}
	if table == "" {
		table = "l7_flow_log"
	}
	resolvedTable := table
	if !strings.Contains(table, ".") && db == "flow_metrics" {
		if req.GetDataSource() != "" {
			resolvedTable = table + "." + req.GetDataSource()
		} else {
			resolvedTable = table + ".1m"
		}
		// Granularity fallback: the environment may only retain coarser
		// tables (e.g. .1d). A requested-but-missing granularity made every
		// column check fail and the whole request fall back to cache.
		if resolved := ch.ResolveTable(db, table, req.GetTimeEnd()-req.GetTimeStart(), req.GetDataSource()); resolved != "" {
			resolvedTable = resolved
		}
	}
	// Use _local table for flow_log to bypass broken Distributed table.
	if db == "flow_log" && !strings.Contains(resolvedTable, "_local") {
		resolvedTable += "_local"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)

	if req.GetNumQueries() == 0 {
		return nil, fmt.Errorf("no queries")
	}
	q := req.QueryAt(0)
	items := clickhouse.ParseSelectList(q.Select)

	constKeys := map[string]bool{}
	var metricExprs []clickhouse.MetricExpr
	isFlowLog := db == "flow_log"

	for _, item := range items {
		lower := strings.ToLower(item.Expr)

		if strings.HasPrefix(lower, "percentile(") {
			inner := item.Expr[len("Percentile(") : len(item.Expr)-1]
			commaIdx := strings.LastIndex(inner, ",")
			if commaIdx > 0 {
				field := strings.TrimSpace(inner[:commaIdx])
				field = strings.Trim(field, "`")
				pct := strings.TrimSpace(inner[commaIdx+1:])
				// Resolve the field like other metrics: flow_log stores
				// rrt/rtt as response_duration; flow_metrics stores them as
				// rrt_sum/rrt_count pairs. Without this, Percentile(`rrt`)
				// produced quantile(0.95)(`rrt`) and ClickHouse failed on
				// the missing 'rrt' column.
				sqlField := field
				if isFlowLog {
					if field == "rrt" || field == "rtt" {
						sqlField = "response_duration"
					}
				} else {
					if !strings.Contains(field, "rrt_sum") && !strings.Contains(field, "rrt_count") {
						sqlField = strings.ReplaceAll(field, "rrt", "rrt_sum / greatest(rrt_count, 1)")
					}
					if !strings.Contains(sqlField, "rtt_sum") && !strings.Contains(sqlField, "rtt_count") {
						sqlField = strings.ReplaceAll(sqlField, "rtt", "rtt_sum / greatest(rtt_count, 1)")
					}
				}
				quantileArg := sqlField
				if !strings.Contains(sqlField, " ") && !strings.Contains(sqlField, "(") {
					quantileArg = "`" + sqlField + "`"
				}
				metricExprs = append(metricExprs, clickhouse.MetricExpr{
					Key: item.Key, SQL: fmt.Sprintf("quantile(%s)(%s)", pct, quantileArg),
				})
			}
			continue
		}

		if strings.HasPrefix(lower, "newtag(") ||
			strings.HasPrefix(lower, "icon_id(") ||
			strings.HasPrefix(lower, "node_type(") ||
			strings.HasPrefix(lower, "enum(") {
			constKeys[item.Key] = true
			continue
		}

		if _, err := fmt.Sscanf(item.Expr, "%f", new(float64)); err == nil {
			constKeys[item.Key] = true
			continue
		}

		if clickhouse.IsAggExpr(item.Expr) {
			sqlExpr := clickhouse.NormalizeExpr(item.Expr)
			if isFlowLog {
				sqlExpr = clickhouse.GetFlowLogExpr(sqlExpr, item.Expr, table, nil, db, resolvedTable)
			}

			if isFlowLog {
				// flow_log table: override metricMaps designed for flow_metrics.
				// Normalize case first: the frontend sends DSL functions
				// capitalized (Sum(request)), and the rewrites below are
				// case-sensitive — otherwise Sum(request) would reach
				// ClickHouse as-is and fail on the missing 'request' column.
				// flow_log column names are all lowercase, so this is safe.
				sqlExpr = strings.ToLower(sqlExpr)
				if strings.Contains(resolvedTable, "l4") {
					// l4_flow_log has a bare rtt column (no rtt_sum pairs).
					sqlExpr = strings.ReplaceAll(sqlExpr, "rtt_sum / nullif(rtt_count, 0)", "rtt")
				} else {
					// l7_flow_log: rrt/rtt stored as response_duration.
					sqlExpr = strings.ReplaceAll(sqlExpr, "rrt_sum / nullif(rrt_count, 0)", "response_duration")
					sqlExpr = strings.ReplaceAll(sqlExpr, "rtt_sum / nullif(rtt_count, 0)", "response_duration")
				}
				// server_error/client_error columns don't exist; use response_status.
				sqlExpr = strings.ReplaceAll(sqlExpr, "nullif(server_error, 0) / nullif(request, 0)", "if(response_status >= 500, 1, 0)")
				sqlExpr = strings.ReplaceAll(sqlExpr, "nullif(client_error, 0) / nullif(request, 0)", "if(response_status >= 400 AND response_status < 500, 1, 0)")
				// request column doesn't exist in flow_log; each row is one request.
				sqlExpr = strings.ReplaceAll(sqlExpr, "avg(request)", "count(*)")
				sqlExpr = strings.ReplaceAll(sqlExpr, "sum(request)", "count(*)")
				sqlExpr = strings.ReplaceAll(sqlExpr, "`request`", "1")

				cleanExpr := strings.ToLower(strings.ReplaceAll(item.Expr, "`", ""))
				if !strings.Contains(cleanExpr, "rrt") && !strings.Contains(cleanExpr, "rtt") &&
					!strings.Contains(cleanExpr, "response_duration") &&
					!strings.Contains(cleanExpr, "response_code") &&
					!strings.Contains(cleanExpr, "response_status") &&
					!strings.Contains(cleanExpr, "request") &&
					!strings.Contains(cleanExpr, "request_type") &&
					!strings.Contains(cleanExpr, "request_domain") &&
					!strings.Contains(cleanExpr, "request_resource") &&
					!strings.Contains(cleanExpr, "response_exception") &&
					!strings.Contains(cleanExpr, "response_result") &&
					!strings.Contains(cleanExpr, "server_error") &&
					!strings.Contains(cleanExpr, "client_error") {
					sqlExpr = "count(*)"
				}
			} else {
				// flow_metrics: rrt → rrt_sum/greatest(rrt_count,1)
				if !strings.Contains(sqlExpr, "rrt_sum") && !strings.Contains(sqlExpr, "rrt_count") {
					sqlExpr = strings.ReplaceAll(sqlExpr, "rrt", "rrt_sum / greatest(rrt_count, 1)")
				}
				if !strings.Contains(sqlExpr, "rtt_sum") && !strings.Contains(sqlExpr, "rtt_count") {
					sqlExpr = strings.ReplaceAll(sqlExpr, "rtt", "rtt_sum / greatest(rtt_count, 1)")
				}
			}
			metricExprs = append(metricExprs, clickhouse.MetricExpr{Key: item.Key, SQL: sqlExpr})
		}
	}

	if len(metricExprs) == 0 {
		return nil, fmt.Errorf("no metric expressions found")
	}

	var wheres []string
	if req.GetTimeStart() > 0 {
		wheres = append(wheres, fmt.Sprintf("time >= %d", req.GetTimeStart()))
	}
	if req.GetTimeEnd() > 0 {
		wheres = append(wheres, fmt.Sprintf("time <= %d", req.GetTimeEnd()))
	}
	if q.Where != "" {
		cleanWhere := clickhouse.CleanWhereClause(q.Where, db, table)
		if cleanWhere != "" {
			wheres = append(wheres, cleanWhere)
		}
	}
	whereClause := ""
	if len(wheres) > 0 {
		whereClause = " WHERE " + strings.Join(wheres, " AND ")
	}

	var metricSelects []string
	for _, m := range metricExprs {
		metricSelects = append(metricSelects, fmt.Sprintf("%s AS `%s`", m.SQL, m.Key))
	}

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Time bucket width in seconds, shared with the per-group HISTORY query.
	intervalSec := req.GetInterval()
	if intervalSec <= 0 {
		intervalSec = 300
	}

	var selectCols []string
	if req.GetInterval() > 0 {
		selectCols = append(selectCols, fmt.Sprintf("toUnixTimestamp(toStartOfInterval(time, toIntervalSecond(%d))) AS `time`", intervalSec))
	}
	selectCols = append(selectCols, metricSelects...)
	limitPart := " LIMIT 1"
	groupPart := ""
	if req.GetInterval() > 0 {
		limitPart = ""
		groupPart = " GROUP BY `time` ORDER BY `time`"
	}
	querySQL := fmt.Sprintf("SELECT %s FROM %s%s%s%s", strings.Join(selectCols, ", "), fullTable, whereClause, groupPart, limitPart)
	logging.Debugf("CH Top SQL: %s", querySQL)

	rows, err := ch.Query(qCtx, querySQL)
	if err != nil {
		logging.Errorf("CH query error: %v", err)
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		logging.Errorf("CH scan error: %v", err)
		return nil, fmt.Errorf("scan: %w", err)
	}
	logging.Infof("CH QueryTop OK: %d rows", len(data))

	// Tag mappings come from the unified maps (clickhouse.TagExpr /
	// TagIDExpr for flow_metrics, ColumnExpr / IDColumn for flow_log,
	// TagSideExpr for _0/_1 client/server views).

	// If tags present, do grouped aggregation.
	var tagCols []string
	var groupCols []string
	for _, t := range q.Tags {
		rawExpr, colName := t, t
		if idx := strings.LastIndex(strings.ToUpper(t), " AS "); idx >= 0 {
			rawExpr = strings.TrimSpace(t[:idx])
			colName = strings.TrimSpace(t[idx+4:])
			colName = strings.Trim(colName, "`")
		}
		if colName == "" {
			continue
		}
		lowerExpr := strings.ToLower(strings.TrimSpace(rawExpr))
		if strings.HasPrefix(lowerExpr, "node_type(") ||
			strings.HasPrefix(lowerExpr, "icon_id(") ||
			strings.HasPrefix(lowerExpr, "newtag(") ||
			strings.HasPrefix(lowerExpr, "enum(") {
			continue
		}
		if _, err := fmt.Sscanf(rawExpr, "%f", new(float64)); err == nil {
			continue
		}
		mappedCol := colName
		if isFlowLog {
			if expr := clickhouse.ColumnExpr(colName, true); expr != "" {
				mappedCol = expr
			}
		} else if expr := clickhouse.TagExpr("flow_metrics", table, colName); expr != "" {
			// $any() placeholders (auto_service IP fallback) expand to any()
			// for SELECT; GROUP BY uses the bare form via TagGroupExpr below.
			mappedCol = clickhouse.ExpandAny(expr, true)
		} else if expr := clickhouse.TagSideExpr("flow_metrics", table, colName); expr != "" {
			mappedCol = expr
		}
		// Bare columns (no mapping) are pre-checked against the physical
		// schema; unsupported signatures fall through the chain.
		if mappedCol == colName && !ch.HasColumn(db, resolvedTable, colName) {
			return nil, fmt.Errorf("%w: %s", clickhouse.ErrUnsupportedColumn, colName)
		}
		// dictGet/IP/computed expressions go in SELECT; grouping uses the
		// underlying physical ID column when one exists. For computed tags
		// without an ID column (is_internet, ...) the GROUP BY key must be
		// the BARE non-aggregated expression — backticking the grouped form
		// (any(if(...))) would make ClickHouse read it as a column name and
		// fail the query.
		if strings.Contains(mappedCol, "dictGet") || strings.Contains(mappedCol, "IPv4") || strings.Contains(mappedCol, "any(") {
			tagCols = append(tagCols, fmt.Sprintf("%s AS `%s`", mappedCol, colName))
			if group := clickhouse.TagGroupExpr("flow_metrics", table, colName); group != "" {
				groupCols = append(groupCols, group)
			} else if idCol := clickhouse.IDColumn(colName); idCol != colName {
				groupCols = append(groupCols, fmt.Sprintf("`%s`", idCol))
			} else {
				// No physical ID column: group by the non-aggregated
				// expression (ColumnExpr with grouped=false strips any()).
				plain := clickhouse.ColumnExpr(colName, false)
				if plain == "" || plain == colName {
					plain = mappedCol
				}
				groupCols = append(groupCols, plain)
			}
		} else if strings.Contains(mappedCol, "(") {
			// Computed expression (flow_metrics is_internet_0/1): SELECT the
			// any(...) form, GROUP BY the bare expression (backticking the
			// whole if(...) would read as a column name).
			tagCols = append(tagCols, fmt.Sprintf("%s AS `%s`", mappedCol, colName))
			if g := clickhouse.TagSideGroupExpr("flow_metrics", table, colName); g != "" {
				groupCols = append(groupCols, g)
			} else {
				groupCols = append(groupCols, mappedCol)
			}
		} else {
			tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mappedCol, colName))
			groupCols = append(groupCols, fmt.Sprintf("`%s`", mappedCol))
		}
	}

	// Also process GROUP_BY (flat Top format) into tag/group cols.
	for _, gb := range strings.Split(q.GroupBy, ",") {
		gb = strings.TrimSpace(gb)
		gb = strings.Trim(gb, "`")
		if gb == "" {
			continue
		}
		// Skip if already in tagCols from TAGS.
		already := false
		for _, tc := range tagCols {
			if strings.Contains(tc, "`"+gb+"`") {
				already = true
				break
			}
		}
		if already {
			continue
		}
		// Skip passthrough DSL functions and numeric literals.
		lowerGb := strings.ToLower(gb)
		if strings.HasPrefix(lowerGb, "node_type(") ||
			strings.HasPrefix(lowerGb, "icon_id(") ||
			strings.HasPrefix(lowerGb, "newtag(") ||
			strings.HasPrefix(lowerGb, "enum(") {
			continue
		}
		if _, err := fmt.Sscanf(gb, "%f", new(float64)); err == nil {
			continue
		}
		// Skip constants and function aliases (newTag('x'), node_type('x'), -42, etc.)
		if constKeys[gb] {
			continue
		}
		mappedCol := gb
		if isFlowLog {
			if expr := clickhouse.ColumnExpr(gb, true); expr != "" {
				mappedCol = expr
			}
		} else if expr := clickhouse.TagExpr("flow_metrics", table, gb); expr != "" {
			mappedCol = clickhouse.ExpandAny(expr, true)
		} else if expr := clickhouse.TagSideExpr("flow_metrics", table, gb); expr != "" {
			mappedCol = expr
		}
		// Bare columns (no mapping) are pre-checked against the physical
		// schema; unsupported signatures fall through the chain.
		if mappedCol == gb && !ch.HasColumn(db, resolvedTable, gb) {
			return nil, fmt.Errorf("%w: %s", clickhouse.ErrUnsupportedColumn, gb)
		}
		// dictGet/IP/computed expressions go in SELECT; grouping uses the
		// underlying physical ID column when one exists. For computed tags
		// without an ID column (is_internet, ...) the GROUP BY key must be
		// the BARE non-aggregated expression — backticking the grouped form
		// (any(if(...))) would make ClickHouse read it as a column name and
		// fail the query.
		if strings.Contains(mappedCol, "dictGet") || strings.Contains(mappedCol, "IPv4") || strings.Contains(mappedCol, "any(") {
			tagCols = append(tagCols, fmt.Sprintf("%s AS `%s`", mappedCol, gb))
			if group := clickhouse.TagGroupExpr("flow_metrics", table, gb); group != "" {
				groupCols = append(groupCols, group)
			} else if idCol := clickhouse.IDColumn(gb); idCol != gb {
				groupCols = append(groupCols, fmt.Sprintf("`%s`", idCol))
			} else {
				// No physical ID column: group by the non-aggregated
				// expression (ColumnExpr with grouped=false strips any()).
				plain := clickhouse.ColumnExpr(gb, false)
				if plain == "" || plain == gb {
					plain = mappedCol
				}
				groupCols = append(groupCols, plain)
			}
		} else if strings.Contains(mappedCol, "(") {
			// Computed expression (flow_metrics is_internet_0/1): GROUP BY
			// the bare expression — backticking if(...) would fail.
			tagCols = append(tagCols, fmt.Sprintf("%s AS `%s`", mappedCol, gb))
			if g := clickhouse.TagSideGroupExpr("flow_metrics", table, gb); g != "" {
				groupCols = append(groupCols, g)
			} else {
				groupCols = append(groupCols, mappedCol)
			}
		} else {
			tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mappedCol, gb))
			groupCols = append(groupCols, fmt.Sprintf("`%s`", mappedCol))
		}
	}

	if len(tagCols) > 0 && len(groupCols) > 0 {
		// Also select the grouping ID columns so per-group HISTORY filters can
		// reference them (virtual tags like region_0 have no physical column).
		groupSelect := append(append([]string{}, tagCols...), groupCols...)
		// Order groups by the requested SORT column (fallback: first metric),
		// so TOP-N actually returns the top groups instead of arbitrary rows.
		orderCol := ""
		orderDir := "DESC"
		if ob := req.GetSortOrderBy(); ob != "" {
			for _, m := range metricExprs {
				if strings.EqualFold(ob, m.Key) {
					orderCol = fmt.Sprintf("`%s`", m.Key)
					break
				}
			}
		}
		if orderCol == "" && len(metricExprs) > 0 {
			orderCol = fmt.Sprintf("`%s`", metricExprs[0].Key)
		}
		if sd := req.GetSortSortedBy(); sd != "" {
			orderDir = strings.ToUpper(sd)
		}
		groupSQL := fmt.Sprintf("SELECT %s, %s FROM %s%s GROUP BY %s ORDER BY %s %s",
			strings.Join(metricSelects, ", "), strings.Join(groupSelect, ", "),
			fullTable, whereClause, strings.Join(groupCols, ", "), orderCol, orderDir)
		if req.GetPageSize() > 0 {
			groupSQL += fmt.Sprintf(" LIMIT %d", req.GetPageSize())
		} else {
			groupSQL += " LIMIT 50"
		}
		logging.Debugf("CH Top grouped SQL: %s", groupSQL)

		rows.Close()
		gRows, gErr := ch.Query(qCtx, groupSQL)
		if gErr == nil {
			defer gRows.Close()
			if gData, gErr2 := clickhouse.ScanRows(gRows); gErr2 == nil {
				data = gData
				// logging.Debugf("CH Top grouped: %d rows", len(data))
			} else {
				logging.Errorf("CH Top grouped scan error: %v", gErr2)
			}
		} else {
			logging.Errorf("CH Top grouped query error: %v", gErr)
		}
	}

	if len(data) == 0 {
		return &query.QueryTopResult{Data: []map[string]interface{}{}}, nil
	}

	var resultRows []map[string]interface{}
	seenUID := map[string]bool{}
	for _, row := range data {
		// Build UID from the SELECTed tag/group columns (tagCols covers both the
		// TAGS format and the flat GROUP_BY format — previously GROUP_BY-only
		// requests got UID "_" for every row and all but the first were dropped).
		var uidParts []string
		for _, tc := range tagCols {
			cn := tc
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(tc[idx+4:])
				cn = strings.Trim(cn, "`")
			}
			if cn != "" {
				if v, ok := row[cn]; ok {
					uidParts = append(uidParts, fmt.Sprintf("%s=%v", cn, v))
				}
			}
		}
		uid := "_"
		if len(uidParts) > 0 {
			uid = strings.Join(uidParts, ",")
		}

		resultRow := map[string]interface{}{"_querier_region": clickhouse.QuerierRegion, "UID": uid}
		// Graph-node contract fields: cloud Top rows always carry
		// UID_NAME/FULL_NAME/NAME/TAGS (verified against api_cache and
		// cloud.deepflow.yunshan.net). Placeholder rows — all tag values are
		// "_" or DSL constants (knowledge-graph queries like
		// node_type('_')/newTag('_')) — get UID_NAME="*" and empty strings;
		// real rows get "tag=value" strings and a TAGS JSON of the row's tag
		// columns. Missing UID_NAME made the knowledge-graph frontend crash
		// (flatMap reading 'key' of undefined).
		placeholder := len(uidParts) == 0
		if !placeholder {
			placeholder = true
			for _, p := range uidParts {
				if !strings.HasSuffix(p, "=_") {
					placeholder = false
					break
				}
			}
		}
		if placeholder {
			resultRow["UID_NAME"] = "*"
			resultRow["FULL_NAME"] = ""
			resultRow["NAME"] = ""
			resultRow["TAGS"] = "{}"
		} else {
			resultRow["UID_NAME"] = uid
			resultRow["FULL_NAME"] = strings.ReplaceAll(uid, ",", "，")
			resultRow["NAME"] = nil
			tagJSON := map[string]interface{}{}
			for _, tc := range tagCols {
				cn := tc
				if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
					cn = strings.TrimSpace(tc[idx+4:])
					cn = strings.Trim(cn, "`")
				}
				if cn != "" {
					if v, ok := row[cn]; ok {
						tagJSON[cn] = v
					}
				}
			}
			if tj, err := json.Marshal(tagJSON); err == nil {
				resultRow["TAGS"] = string(tj)
			} else {
				resultRow["TAGS"] = "{}"
			}
		}
		for _, tc := range q.Tags {
			cn := tc
			rawExpr := strings.TrimSpace(tc)
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(tc[idx+4:])
				cn = strings.Trim(cn, "`")
				rawExpr = strings.TrimSpace(tc[:idx])
			}
			if cn == "" {
				continue
			}
			if v, ok := row[cn]; ok {
				resultRow[cn] = v
			} else {
				// Compute default from DSL tag expression for CH path (no deepflow-server resolution).
				lowerExpr := strings.ToLower(rawExpr)
				if strings.HasPrefix(lowerExpr, "node_type(") || strings.HasPrefix(lowerExpr, "newtag(") {
					s := strings.Index(rawExpr, "(")
					e := strings.LastIndex(rawExpr, ")")
					if s >= 0 && e > s {
						resultRow[cn] = strings.Trim(rawExpr[s+1:e], "`\x27 ")
					}
				} else if strings.HasPrefix(lowerExpr, "-42") {
					resultRow[cn] = int(-42)
				} else if n, _ := fmt.Sscanf(rawExpr, "%d", new(int)); n == 1 {
					var num int
					fmt.Sscanf(rawExpr, "%d", &num)
					resultRow[cn] = num
				}
			}
		}
		// GROUP_BY-only (flat format) requests have no TAGS entries; carry the
		// grouped columns into the result row from tagCols instead.
		for _, tc := range tagCols {
			cn := tc
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(tc[idx+4:])
				cn = strings.Trim(cn, "`")
			}
			if cn == "" {
				continue
			}
			if _, exists := resultRow[cn]; !exists {
				if v, ok := row[cn]; ok {
					resultRow[cn] = v
				}
			}
		}
		for _, m := range metricExprs {
			if v, ok := row[m.Key]; ok {
				resultRow[m.Key] = v
			}
		}

		// Deduplicate before the per-group HISTORY query: a duplicate UID's
		// history would be computed and then discarded (N+1 waste).
		if seenUID[uid] {
			continue
		}
		seenUID[uid] = true

		// Per-group history.
		histWheres := make([]string, len(wheres))
		copy(histWheres, wheres)
		for _, tc := range q.Tags {
			cn := tc
			rawExpr := strings.TrimSpace(tc)
			if idx := strings.LastIndex(strings.ToUpper(tc), " AS "); idx >= 0 {
				cn = strings.TrimSpace(cn[idx+4:])
				cn = strings.Trim(cn, "`")
				rawExpr = strings.TrimSpace(rawExpr[:idx])
			}
			// Skip DSL function tags: they don't map to physical ClickHouse columns.
			lowerExpr := strings.ToLower(rawExpr)
			if strings.HasPrefix(lowerExpr, "node_type(") ||
				strings.HasPrefix(lowerExpr, "icon_id(") ||
				strings.HasPrefix(lowerExpr, "newtag(") ||
				strings.HasPrefix(lowerExpr, "enum(") {
				continue
			}
			if cn != "" {
				if v, ok := row[cn]; ok {
					mappedCN := cn
					if isFlowLog {
						// Virtual tags resolve to ID columns; the grouped query
						// selects them alongside, so compare on the ID value.
						if id := clickhouse.IDColumn(cn); id != cn {
							mappedCN = id
							if v2, ok2 := row[id]; ok2 {
								v = v2
							}
						}
					} else if id := clickhouse.TagIDExpr("flow_metrics", table, cn); id != cn {
						mappedCN = id
						if v2, ok2 := row[id]; ok2 {
							v = v2
						}
					}
					histWheres = append(histWheres, fmt.Sprintf("`%s` = '%v'", mappedCN, v))
				}
			}
		}
		histWhere := ""
		if len(histWheres) > 0 {
			histWhere = " WHERE " + strings.Join(histWheres, " AND ")
		}
		hSQL := fmt.Sprintf("SELECT toUnixTimestamp(toStartOfInterval(time, INTERVAL %d SECOND)) AS toi, %s FROM %s%s GROUP BY toi ORDER BY toi LIMIT 500",
			intervalSec, strings.Join(metricSelects, ", "), fullTable, histWhere)
		histRows, hErr := ch.Query(qCtx, hSQL)
		if hErr == nil {
			if histData, hErr2 := clickhouse.ScanRows(histRows); hErr2 == nil {
				resultRow["HISTORY"] = tracemap.FillNullHistory(tracemap.ConvertHistory(histData, metricExprs), int64(req.GetInterval()), req.GetTimeStart(), req.GetTimeEnd(), req.GetFill(), metricExprs)
			}
			histRows.Close()
		}
		resultRows = append(resultRows, resultRow)
	}

	// Build pre_as map from SELECT expressions (same as List handler).
	preAsMap := map[string]string{}
	if req.GetNumQueries() > 0 && req.QueryAt(0).Select != "" {
		for _, item := range clickhouse.ParseSelectList(req.QueryAt(0).Select) {
			if item.Key != item.Expr {
				preAsMap[item.Key] = strings.ReplaceAll(item.Expr, "`", "")
			}
		}
	}

	// Build SCHEMAS from first data row.
	schemas := map[string]interface{}{}
	if len(resultRows) > 0 {
		for k, v := range resultRows[0] {
			vt, tp := "String", 0
			switch v.(type) {
			case float64:
				vt, tp = "Float64", 1
			case float32:
				vt, tp = "Float64", 1
			case int, int64, uint64:
				vt, tp = "UInt64", 1
				// icon_id is always a tag (even when value is int like -42).
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
	return &query.QueryTopResult{
		Data:   resultRows,
		Fields: schemas,
	}, nil
}
