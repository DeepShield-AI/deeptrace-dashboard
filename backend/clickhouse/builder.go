package clickhouse

import (
	"fmt"
	"strings"
	"time"
)

// SelectItem represents one column in a SELECT clause.
type SelectItem struct {
	Expr string
	Key  string
}

// QuerierRequest mirrors the JSON structure of querier API requests.
type QuerierRequest struct {
	Database  string           `json:"DATABASE"`
	Table     string           `json:"TABLE"`
	PageIndex int              `json:"PAGE_INDEX"`
	PageSize  int              `json:"PAGE_SIZE"`
	Sort      *QuerierSort     `json:"SORT"`
	Queries   []QuerierSub     `json:"QUERIES"`
	TimeStart int64            `json:"time_start"`
	TimeEnd   int64            `json:"time_end"`
	Regions   []string         `json:"REGIONS"`
	Total     bool             `json:"TOTAL"`
}

// QuerierSort holds sort parameters.
type QuerierSort struct {
	OrderBy  string `json:"ORDER_BY"`
	SortedBy string `json:"SORTED_BY"`
}

// QuerierSub represents one sub-query.
type QuerierSub struct {
	QueryID string   `json:"QUERY_ID"`
	Select  string   `json:"SELECT"`
	Where   string   `json:"WHERE"`
	Tags    []string `json:"TAGS"`
	CTags   []string `json:"CTAGS"`
	STags   []string `json:"STAGS"`
	Metrics []string `json:"METRICS"`
	GroupBy string   `json:"GROUP_BY"`
}

// ---------------------------------------------------------------------------
// SELECT parser
// ---------------------------------------------------------------------------

// ParseSelectList splits a SELECT expression into items, respecting parentheses.
func ParseSelectList(sel string) []SelectItem {
	var items []SelectItem
	depth := 0
	start := 0
	for i, ch := range sel {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(sel[start:i])
				if part != "" {
					items = append(items, parseOneSelect(part))
				}
				start = i + 1
			}
		}
	}
	if start < len(sel) {
		part := strings.TrimSpace(sel[start:])
		if part != "" {
			items = append(items, parseOneSelect(part))
		}
	}
	return items
}

func parseOneSelect(part string) SelectItem {
	expr, key := part, part
	upper := strings.ToUpper(part)
	asIdx := strings.LastIndex(upper, " AS ")
	if asIdx >= 0 {
		expr = strings.TrimSpace(part[:asIdx])
		key = strings.TrimSpace(part[asIdx+4:])
		key = strings.Trim(key, "`")
	}
	if key == "" {
		key = expr
	}
	return SelectItem{Expr: expr, Key: key}
}

// IsAggExpr reports whether the expression is an aggregate function.
func IsAggExpr(expr string) bool {
	lower := strings.ToLower(expr)
	return strings.Contains(lower, "count(") ||
		strings.Contains(lower, "avg(") ||
		strings.Contains(lower, "sum(") ||
		strings.Contains(lower, "max(") ||
		strings.Contains(lower, "min(") ||
		strings.Contains(lower, "persecond(") ||
		strings.Contains(lower, "uniq(")
}

// NormalizeExpr converts a querier expression to ClickHouse SQL.
func NormalizeExpr(expr string) string {
	lower := strings.ToLower(expr)
	if strings.HasPrefix(lower, "newtag(") {
		return ""
	}
	if strings.HasPrefix(lower, "persecond(") {
		inner := expr[len("PerSecond("):len(expr)-1]
		return fmt.Sprintf("(%s)/60.0", NormalizeExpr(inner))
	}
	if strings.HasPrefix(lower, "node_type(") {
		return strings.TrimSpace(expr[len("node_type("):len(expr)-1])
	}
	if strings.HasPrefix(lower, "icon_id(") {
		return "toInt64(0)"
	}
	if strings.HasPrefix(lower, "enum(") {
		return strings.TrimSpace(expr[len("Enum("):len(expr)-1])
	}
	if lower == "count(`row`)" || lower == "count(row)" {
		return "count(*)"
	}
	// Metric name mapping: DeepFlow DSL → ClickHouse expressions.
	noBacktick := strings.ReplaceAll(expr, "`", "")
	noBacktickLower := strings.ToLower(noBacktick)
	// Map common DeepFlow metric names to actual ClickHouse column expressions.
	metricMaps := map[string]string{
		"avg(rrt)":               "avg(rrt_sum / nullif(rrt_count, 0))",
		"sum(rrt)":               "sum(rrt_sum / nullif(rrt_count, 0))",
		"max(rrt)":               "max(rrt_sum / nullif(rrt_count, 0))",
		"min(rrt)":               "min(rrt_sum / nullif(rrt_count, 0))",
		"avg(response_delay)":    "avg(response_duration)",
		"sum(response_delay)":    "sum(response_duration)",
		"avg(response_duration)": "avg(response_duration)",
		"avg(server_error_ratio)":   "avg(nullif(server_error, 0) / nullif(request, 0))",
		"sum(server_error_ratio)":   "sum(nullif(server_error, 0) / nullif(request, 0))",
		"avg(client_error_ratio)":   "avg(nullif(client_error, 0) / nullif(request, 0))",
		"avg(error_ratio)":          "avg(nullif(error, 0) / nullif(request, 0))",
		"avg(request)":             "avg(request)",
	}
	if mapped, ok := metricMaps[noBacktickLower]; ok {
		return mapped
	}
	return noBacktick
}

// ---------------------------------------------------------------------------
// SQL builder for querier List requests
// ---------------------------------------------------------------------------

// BuildSelectSQL converts a querier List request into a ClickHouse SQL string.
func BuildSelectSQL(req QuerierRequest) (string, error) {
	if len(req.Queries) == 0 {
		return "", fmt.Errorf("no queries in request")
	}
	q := req.Queries[0]
	table := req.Table
	if table == "" {
		table = "l7_flow_log"
	}

	resolvedTable := table
	resolvedDB := req.Database
	if resolvedDB == "flow_metrics" && !strings.Contains(table, ".") {
		resolvedTable = table + ".1m"
	}

	fullTable := ""
	if resolvedDB != "" {
		fullTable = "`" + resolvedDB + "`.`" + resolvedTable + "`"
	} else {
		fullTable = "`" + resolvedTable + "`"
	}

	items := ParseSelectList(q.Select)
	var selectParts []string
	var groupByParts []string
	var constParts []string

	gbSet := map[string]bool{}
	for _, t := range q.Tags {
		upper := strings.ToUpper(t)
		if idx := strings.LastIndex(upper, " AS "); idx >= 0 {
			key := strings.Trim(strings.TrimSpace(t[idx+4:]), "`")
			gbSet[key] = true
		} else {
			gbSet[strings.Trim(t, "`")] = true
		}
	}
	if q.GroupBy != "" {
		for _, part := range strings.Split(q.GroupBy, ",") {
			part = strings.TrimSpace(strings.ReplaceAll(part, "`", ""))
			if part != "" {
				gbSet[part] = true
			}
		}
	}

	// Column name mapping for DeepFlow DSL → ClickHouse column names.
	// Tables from flow_metrics use app_service (string) while flow_log tables
	// use auto_service_id_0/1 (UInt) for per-side IDs.
	isFlowMetrics := resolvedDB == "flow_metrics"
	isFlowLog := resolvedDB == "flow_log"
	colMap := map[string]string{
		"auto_service":    "app_service",           // application.* tables
		"auto_instance":   "app_instance",          // application.* tables
		"service_id_0":    "auto_service_id_0",
		"service_id_1":    "auto_service_id_1",
		"instance_id_0":   "auto_instance_id_0",
		"instance_id_1":   "auto_instance_id_1",
	}
	// For flow_log tables, map _0/_1 suffixed tag names to ID columns.
	if isFlowLog {
		colMap["auto_service_0"] = "toString(auto_service_id_0)"
		colMap["auto_service_1"] = "toString(auto_service_id_1)"
		colMap["auto_instance_0"] = "toString(auto_instance_id_0)"
		colMap["auto_instance_1"] = "toString(auto_instance_id_1)"
		colMap["app_service"] = "app_service"
	}
	if isFlowMetrics {
		colMap["auto_service_0"] = "app_service"
		colMap["auto_service_1"] = "app_service"
	}

	for _, item := range items {
		lower := strings.ToLower(item.Expr)
		expr := NormalizeExpr(item.Expr)
		isAgg := IsAggExpr(item.Expr)
		_, isTag := gbSet[item.Key]
		isFunc := strings.HasPrefix(lower, "node_type(") ||
			strings.HasPrefix(lower, "icon_id(") ||
			strings.HasPrefix(lower, "enum(") ||
			strings.HasPrefix(lower, "newtag(")

		switch {
		case strings.HasPrefix(lower, "newtag("):
			tagVal := item.Expr[len("newTag("):len(item.Expr)-1]
			tagVal = strings.Trim(tagVal, "'\"")
			constParts = append(constParts, fmt.Sprintf("'%s' AS `%s`", tagVal, item.Key))

		case isTag || (!isAgg && !isFunc):
			col := strings.Trim(item.Expr, "`")
			// Apply column name mapping.
			if mapped, ok := colMap[col]; ok {
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", mapped, item.Key))
				groupByParts = append(groupByParts, mapped)
			} else {
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", col))
			}

		case isAgg:
			selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", expr, item.Key))

		case strings.HasPrefix(lower, "node_type("):
			inner := strings.TrimSpace(item.Expr[len("node_type("):len(item.Expr)-1])
			selectParts = append(selectParts, fmt.Sprintf("toString(`%s`) AS `%s`", inner, item.Key))
			groupByParts = append(groupByParts, fmt.Sprintf("`%s`", inner))

		case strings.HasPrefix(lower, "icon_id("):
			selectParts = append(selectParts, fmt.Sprintf("toInt64(-13) AS `%s`", item.Key))

		case strings.HasPrefix(lower, "enum("):
			inner := strings.TrimSpace(item.Expr[len("enum("):len(item.Expr)-1])
			selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", inner, item.Key))
			groupByParts = append(groupByParts, fmt.Sprintf("`%s`", inner))

		default:
			col := strings.Trim(item.Expr, "`")
			if mapped, ok := colMap[col]; ok {
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", mapped, item.Key))
				groupByParts = append(groupByParts, mapped)
			} else {
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", col))
			}
		}
	}

	var sql strings.Builder
	sql.WriteString("SELECT ")
	allSelects := append(constParts, selectParts...)
	sql.WriteString(strings.Join(allSelects, ", "))
	sql.WriteString(fmt.Sprintf(" FROM %s", fullTable))

	var wheres []string
	if req.TimeStart > 0 {
		ts := time.Unix(req.TimeStart, 0).In(time.UTC).Format("2006-01-02 15:04:05")
		wheres = append(wheres, fmt.Sprintf("time >= '%s'", ts))
	}
	if req.TimeEnd > 0 {
		ts := time.Unix(req.TimeEnd, 0).In(time.UTC).Format("2006-01-02 15:04:05")
		wheres = append(wheres, fmt.Sprintf("time <= '%s'", ts))
	}
	if q.Where != "" {
		cleanWhere := cleanWhereClause(q.Where)
		if cleanWhere != "" {
			wheres = append(wheres, cleanWhere)
		}
	}
	if len(wheres) > 0 {
		sql.WriteString(" WHERE ")
		sql.WriteString(strings.Join(wheres, " AND "))
	}

	if len(groupByParts) > 0 {
		sql.WriteString(" GROUP BY ")
		sql.WriteString(strings.Join(groupByParts, ", "))
	}

	if req.Sort != nil && req.Sort.OrderBy != "" {
		dir := "ASC"
		if strings.ToUpper(req.Sort.SortedBy) == "DESC" {
			dir = "DESC"
		}
		sql.WriteString(fmt.Sprintf(" ORDER BY `%s` %s", strings.ReplaceAll(req.Sort.OrderBy, "`", ""), dir))
	}

	if req.PageSize > 0 {
		sql.WriteString(fmt.Sprintf(" LIMIT %d", req.PageSize))
		if req.PageIndex > 1 {
			sql.WriteString(fmt.Sprintf(" OFFSET %d", (req.PageIndex-1)*req.PageSize))
		}
	} else {
		sql.WriteString(" LIMIT 500")
	}

	return sql.String(), nil
}

// ---------------------------------------------------------------------------
// Histogram SQL builder
// ---------------------------------------------------------------------------

// HistogramRequest carries parameters for a histogram query.
type HistogramRequest struct {
	Database  string
	Table     string
	TimeStart int64
	TimeEnd   int64
}

// BuildHistogramSQL builds a time-bucketed count query for histogram/flame-graph views.
// Uses 1-minute buckets for short ranges, 1-hour buckets for ranges > 2 hours.
func BuildHistogramSQL(req HistogramRequest) (string, error) {
	db := req.Database
	if db == "" {
		db = "flow_log"
	}
	table := req.Table
	if table == "" {
		table = "l7_flow_log"
	}
	resolvedTable := table
	if db == "flow_metrics" && !strings.Contains(table, ".") {
		resolvedTable = table + ".1m"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)

	interval := "toStartOfMinute(time)"
	if req.TimeEnd-req.TimeStart > 7200 {
		interval = "toStartOfHour(time)"
	}

	var wheres []string
	if req.TimeStart > 0 {
		ts := time.Unix(req.TimeStart, 0).In(time.UTC).Format("2006-01-02 15:04:05")
		wheres = append(wheres, fmt.Sprintf("time >= '%s'", ts))
	}
	if req.TimeEnd > 0 {
		ts := time.Unix(req.TimeEnd, 0).In(time.UTC).Format("2006-01-02 15:04:05")
		wheres = append(wheres, fmt.Sprintf("time <= '%s'", ts))
	}
	whereClause := ""
	if len(wheres) > 0 {
		whereClause = " WHERE " + strings.Join(wheres, " AND ")
	}

	sql := fmt.Sprintf("SELECT toUnixTimestamp(%s) AS time_bucket, count(*) AS count FROM %s%s GROUP BY time_bucket ORDER BY time_bucket LIMIT 1000",
		interval, fullTable, whereClause)
	return sql, nil
}

// ---------------------------------------------------------------------------
// ShowAttributes SQL builder
// ---------------------------------------------------------------------------

// BuildShowAttributesSQL builds a query to fetch span attribute names/values.
func BuildShowAttributesSQL(database, table string) string {
	db := database
	if db == "" {
		db = "flow_log"
	}
	tbl := table
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, tbl)
	return fmt.Sprintf("SELECT attribute_names, attribute_values, _id FROM %s LIMIT 100", fullTable)
}

// cleanWhereClause strips exist() calls from the WHERE clause.
func cleanWhereClause(where string) string {
	for strings.Contains(where, "exist(") {
		start := strings.Index(where, "exist(")
		end := start + 5
		for end < len(where) && where[end] != ')' {
			end++
		}
		if end < len(where) {
			end++
		}
		before := strings.TrimSpace(where[:start])
		after := strings.TrimSpace(where[end:])
		if strings.HasSuffix(before, "AND") {
			before = strings.TrimSpace(before[:len(before)-3])
		}
		if strings.HasSuffix(before, "OR") {
			before = strings.TrimSpace(before[:len(before)-2])
		}
		if strings.HasPrefix(after, "AND") {
			after = strings.TrimSpace(after[3:])
		}
		if strings.HasPrefix(after, "OR") {
			after = strings.TrimSpace(after[2:])
		}
		where = strings.TrimSpace(before + " " + after)
	}
	return where
}
