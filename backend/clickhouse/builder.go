package clickhouse

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrUnsupportedColumn marks a query that references a column the target
// table doesn't have: the source does not support this signature, so the
// chain falls through (to cache) instead of surfacing a 502.
var ErrUnsupportedColumn = errors.New("unsupported column")

// checkHasColumn is a nil-safe wrapper for the column pre-check.
func checkHasColumn(check ColumnChecker, db, table, col string) bool {
	return check == nil || check.HasColumn(db, table, col)
}

// SelectItem represents one column in a SELECT clause.
type SelectItem struct {
	Expr string
	Key  string
}

// QuerierRequest mirrors the JSON structure of querier API requests.
type QuerierRequest struct {
	Database   string       `json:"DATABASE"`
	Table      string       `json:"TABLE"`
	PageIndex  int          `json:"PAGE_INDEX"`
	PageSize   int          `json:"PAGE_SIZE"`
	Sort       *QuerierSort `json:"SORT"`
	Queries    []QuerierSub `json:"QUERIES"`
	TimeStart  int64        `json:"time_start"`
	TimeEnd    int64        `json:"time_end"`
	Regions    []string     `json:"REGIONS"`
	Total      bool         `json:"TOTAL"`
	DataSource string       `json:"DATA_SOURCE"`
	Interval   int          `json:"interval"`
	Fill       string       `json:"fill"`
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
	Having  string   `json:"HAVING"`
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

// normalizeHavingExpr normalizes aggregate expressions in HAVING clause.
func normalizeHavingExpr(having string) string {
	result := having
	// Expand DSL functions first — repeat until none remain, because
	// replaceDSLFunc handles one call per invocation and HAVING conditions
	// may reference the same expression multiple times
	// (PerSecond(Avg(`request`))>=1 AND PerSecond(Avg(`request`))<=10).
	for {
		expanded := replaceDSLFunc(result)
		if expanded == result {
			break
		}
		result = expanded
	}
	// Normalize remaining aggregate expressions (metric name mappings).
	result = NormalizeExpr(result)

	havingExprs := map[string]string{
		"avg(`server_error_ratio`)": "avg(nullif(server_error, 0) / nullif(request, 0))",
		"avg(server_error_ratio)":   "avg(nullif(server_error, 0) / nullif(request, 0))",
		"sum(server_error_ratio)":   "sum(nullif(server_error, 0) / nullif(request, 0))",
		"avg(`client_error_ratio`)": "avg(nullif(client_error, 0) / nullif(request, 0))",
		"avg(client_error_ratio)":   "avg(nullif(client_error, 0) / nullif(request, 0))",
		"avg(`error_ratio`)":        "avg(nullif(error, 0) / nullif(request, 0))",
		"avg(error_ratio)":          "avg(nullif(error, 0) / nullif(request, 0))",
		"avg(`rrt`)":                "avg(rrt_sum / nullif(rrt_count, 0))",
		"avg(rrt)":                  "avg(rrt_sum / nullif(rrt_count, 0))",
	}
	// Case-insensitive replacement (NormalizeExpr output is lowercase).
	lowerResult := strings.ToLower(result)
	for expr, chExpr := range havingExprs {
		if strings.Contains(lowerResult, expr) {
			result = strings.ReplaceAll(result, expr, chExpr)
			lowerResult = strings.ToLower(result)
		}
	}
	return result
}

// replaceDSLFunc replaces DeepFlow DSL function calls in expr with ClickHouse equivalents.
// Handles: PerSecond(...), node_type(...), icon_id(...), Enum(...), newTag(...)
func replaceDSLFunc(expr string) string {
	lower := strings.ToLower(expr)

	// PerSecond(x) → (normalized_x)/60.0
	if idx := strings.Index(lower, "persecond("); idx >= 0 {
		// Find the matching closing paren.
		argStart := idx + len("persecond(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			arg := expr[argStart:argEnd]
			prefix := expr[:idx]
			suffix := expr[argEnd+1:]
			return prefix + "(" + NormalizeExpr(arg) + ")/60.0" + suffix
		}
	}
	// node_type(x) → x (passthrough)
	if idx := strings.Index(lower, "node_type("); idx >= 0 {
		argStart := idx + len("node_type(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			return expr[:idx] + strings.TrimSpace(expr[argStart:argEnd]) + expr[argEnd+1:]
		}
	}
	// icon_id(x) → toInt64(0)
	if idx := strings.Index(lower, "icon_id("); idx >= 0 {
		argStart := idx + len("icon_id(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			return expr[:idx] + "toInt64(0)" + expr[argEnd+1:]
		}
	}
	// Enum(x) → x
	if idx := strings.Index(lower, "enum("); idx >= 0 {
		argStart := idx + len("enum(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			return expr[:idx] + strings.TrimSpace(expr[argStart:argEnd]) + expr[argEnd+1:]
		}
	}
	// newTag(x) → ''
	if idx := strings.Index(lower, "newtag("); idx >= 0 {
		argStart := idx + len("newtag(")
		argEnd := findMatchingParen(expr, argStart-1)
		if argEnd > argStart {
			return expr[:idx] + "''" + expr[argEnd+1:]
		}
	}
	return expr
}

// BalanceParens drops unmatched parentheses from a WHERE fragment. The
// is_internet/role condition stripper consumes the closing ')' of a
// parenthesized condition ("(subnet_id=0 AND role=0)" → "(subnet_id=0"),
// leaving an orphan '(' that ClickHouse rejects ("Unmatched parentheses").
func BalanceParens(s string) string {
	var stack []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			stack = append(stack, i)
		case ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			} else {
				// Orphan ')': drop it (the '(' was consumed earlier).
				s = s[:i] + s[i+1:]
				i--
			}
		}
	}
	// Orphan '(': drop from right to left so inner parens stay valid.
	for len(stack) > 0 {
		idx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		s = s[:idx] + s[idx+1:]
	}
	return s
}

// findMatchingParen returns the index of the ')' that matches the '(' at openIdx.
func findMatchingParen(s string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func NormalizeExpr(expr string) string {
	// Strip outer parens so expressions like (x*8) get re-wrapped after normalization.
	outer := strings.TrimSpace(expr)
	if strings.HasPrefix(outer, "(") && strings.HasSuffix(outer, ")") {
		argEnd := findMatchingParen(outer, 0)
		if argEnd == len(outer)-1 {
			// Entire expression is wrapped.
			inner := strings.TrimSpace(outer[1:argEnd])
			normalized := NormalizeExpr(inner)
			return "(" + normalized + ")"
		}
	}

	// Replace known DSL functions in the expression.
	expr = replaceDSLFunc(expr)

	lower := strings.ToLower(expr)
	if lower == "count(`row`)" || lower == "count(row)" {
		return "count(*)"
	}
	if lower == "count(`row`)" || lower == "count(row)" {
		return "count(*)"
	}
	// Metric name mapping: DeepFlow DSL → ClickHouse expressions.
	noBacktick := strings.ReplaceAll(expr, "`", "")
	noBacktickLower := strings.ToLower(noBacktick)
	// Map common DeepFlow metric names to actual ClickHouse column expressions.
	metricMaps := map[string]string{
		"avg(rrt)":                "avg(rrt_sum / nullif(rrt_count, 0))",
		"sum(rrt)":                "sum(rrt_sum / nullif(rrt_count, 0))",
		"max(rrt)":                "max(rrt_sum / nullif(rrt_count, 0))",
		"min(rrt)":                "min(rrt_sum / nullif(rrt_count, 0))",
		"avg(rtt)":                "avg(rtt_sum / nullif(rtt_count, 0))",
		"sum(rtt)":                "sum(rtt_sum / nullif(rtt_count, 0))",
		"max(rtt)":                "max(rtt_sum / nullif(rtt_count, 0))",
		"min(rtt)":                "min(rtt_sum / nullif(rtt_count, 0))",
		"avg(response_delay)":     "avg(response_duration)",
		"sum(response_delay)":     "sum(response_duration)",
		"avg(response_duration)":  "avg(response_duration)",
		"avg(server_error_ratio)": "avg(nullif(server_error, 0) / nullif(request, 0))",
		"sum(server_error_ratio)": "sum(nullif(server_error, 0) / nullif(request, 0))",
		"avg(client_error_ratio)": "avg(nullif(client_error, 0) / nullif(request, 0))",
		"avg(error_ratio)":        "avg(nullif(error, 0) / nullif(request, 0))",
		"avg(request)":            "avg(request)",
		// flow_metrics.network column mappings (DSL names → actual CH columns).
		// The cloud *_ratio metrics are PERCENTAGES (0-100, verified against
		// api_cache: retrans_ratio 2.200795249) — the frontend displays the
		// value directly, so the ratio is derived ×100. flow_log (l4) gets the
		// column names rewritten in getFlowLogExpr (it stores retrans_tx/
		// byte_tx instead of the network-family retrans/byte single columns).
		"avg(retrans_ratio)":            "avg(retrans / nullif(byte, 0)) * 100",
		"avg(zero_win_ratio)":           "avg(zero_win_tx)",
		"avg(tcp_establish_fail_ratio)": "avg(tcp_establish_fail / nullif(syn_count, 0)) * 100",
		"sum(tcp_establish_fail_ratio)": "sum(tcp_establish_fail / nullif(syn_count, 0)) * 100",
		"max(tcp_establish_fail_ratio)": "max(tcp_establish_fail / nullif(syn_count, 0)) * 100",
		"avg(tcp_transfer_fail_ratio)":  "avg(tcp_transfer_fail / nullif(syn_count, 0)) * 100",
		"sum(tcp_transfer_fail_ratio)":  "sum(tcp_transfer_fail / nullif(syn_count, 0)) * 100",
		"max(tcp_transfer_fail_ratio)":  "max(tcp_transfer_fail / nullif(syn_count, 0)) * 100",
		"avg(tcp_rst_fail_ratio)":       "avg(tcp_rst_fail / nullif(syn_count, 0)) * 100",
		"sum(tcp_rst_fail_ratio)":       "sum(tcp_rst_fail / nullif(syn_count, 0)) * 100",
		"max(tcp_rst_fail_ratio)":       "max(tcp_rst_fail / nullif(syn_count, 0)) * 100",
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

// flowLogMetricMap maps bare metric names that don't exist as columns in
// flow_log tables (request/error/rrt...) to derived ClickHouse expressions.
// Each flow_log row is one request, so counts derive from response_status.
// All values are aggregates — never added to GROUP BY.
var flowLogMetricMap = map[string]string{
	"request":        "count(*)",
	"response":       "count(*)",
	"error":          "countIf(response_status >= 400)",
	"client_error":   "countIf(response_status >= 400 AND response_status < 500)",
	"server_error":   "countIf(response_status >= 500)",
	"timeout":        "countIf(response_status = 2)",
	"rrt":            "avg(response_duration)",
	"rtt":            "avg(response_duration)",
	"response_delay": "response_duration",
	"session_length": "request_length + response_length",
}

// getFlowLogExpr rewrites aggregate expressions for flow_log tables where the
// flow_metrics-style columns (request, rrt, server_error...) don't exist.
// It maps normalized names to flow_log columns and falls back to count(*)
// for expressions that reference no flow_log column at all.
// l7_flow_log stores rrt/rtt as response_duration; l4_flow_log has a bare
// rtt column (plus rrt_sum/rrt_count) and must not get the l7 rewrite.
// flowLogLocalCol maps cloud flow_log metric columns to the physical column
// this deployment stores them under. The mapping is checked before an
// expression falls back — a missing mapped column means the metric cannot be
// computed here and the expression becomes NULL (the cloud contract itself
// carries null metrics, e.g. TCP 建连失败比例).
var flowLogLocalCol = map[string]string{
	"byte":          "total_byte_tx", // traffic bytes split tx/rx on this deployment
	"retrans_ratio": "retrans_tx",    // retransmission share derived from retrans bytes
}

func GetFlowLogExpr(sqlExpr, originalExpr, table string, check ColumnChecker, db, tbl string) string {
	normalized := sqlExpr
	if strings.Contains(table, "l4") {
		// l4_flow_log: bare rtt column for TCP connect latency.
		normalized = strings.ReplaceAll(normalized, "rtt_sum / nullif(rtt_count, 0)", "rtt")
		// byte/retrans: this deployment stores them split (byte_tx/rx,
		// retrans_tx) instead of the network-family single columns.
		normalized = strings.ReplaceAll(normalized, "`byte`", "(total_byte_tx + total_byte_rx)")
		normalized = strings.ReplaceAll(normalized, "avg(byte)", "avg((total_byte_tx + total_byte_rx))")
		normalized = strings.ReplaceAll(normalized, "Avg(byte)", "Avg((total_byte_tx + total_byte_rx))")
		normalized = strings.ReplaceAll(normalized, "retrans / nullif(byte, 0)", "retrans_tx / nullif(byte_tx, 0)")
	} else {
		// l7_flow_log: rrt/rtt stored as response_duration.
		normalized = strings.ReplaceAll(normalized, "rrt_sum / nullif(rrt_count, 0)", "response_duration")
		normalized = strings.ReplaceAll(normalized, "rtt_sum / nullif(rtt_count, 0)", "response_duration")
	}
	// server_error/client_error columns don't exist; derive from
	// response_status (enum: 0=正常, 2=超时, 3=服务端异常, 4=客户端异常,
	// 5=未知, 6=解析失败 — NOT HTTP codes).
	normalized = strings.ReplaceAll(normalized, "nullif(server_error, 0) / nullif(request, 0)", "if(response_status = 3, 1, 0)")
	normalized = strings.ReplaceAll(normalized, "nullif(client_error, 0) / nullif(request, 0)", "if(response_status = 4, 1, 0)")
	normalized = strings.ReplaceAll(normalized, "sum(server_error)", "sum(if(response_status = 3, 1, 0))")
	normalized = strings.ReplaceAll(normalized, "avg(server_error)", "avg(if(response_status = 3, 1, 0))")
	normalized = strings.ReplaceAll(normalized, "sum(client_error)", "sum(if(response_status = 4, 1, 0))")
	normalized = strings.ReplaceAll(normalized, "avg(client_error)", "avg(if(response_status = 4, 1, 0))")
	// request column doesn't exist in flow_log; each row is one request.
	normalized = strings.ReplaceAll(normalized, "avg(request)", "count(*)")
	normalized = strings.ReplaceAll(normalized, "sum(request)", "count(*)")
	normalized = strings.ReplaceAll(normalized, "`request`", "1")

	if !strings.Contains(strings.ToLower(normalized), "count(") {
		// Check the REWRITTEN expression, not the original DSL: server_error
		// ratio rewrites to if(response_status >= 500, 1, 0) which is a valid
		// flow_log expression — checking the original would see no flow_log
		// keyword and wrongly collapse it to count(*) (the total row count).
		cleanExpr := strings.ToLower(normalized)
		existsInFlowLog := strings.Contains(cleanExpr, "response_duration") ||
			strings.Contains(cleanExpr, "response_delay") ||
			strings.Contains(cleanExpr, "rrt") ||
			strings.Contains(cleanExpr, "rtt") ||
			strings.Contains(cleanExpr, "response_code") ||
			strings.Contains(cleanExpr, "response_status") ||
			strings.Contains(cleanExpr, "request_type") ||
			strings.Contains(cleanExpr, "request_domain") ||
			strings.Contains(cleanExpr, "request_resource") ||
			strings.Contains(cleanExpr, "request_length") ||
			strings.Contains(cleanExpr, "response_exception") ||
			strings.Contains(cleanExpr, "response_result") ||
			strings.Contains(cleanExpr, "captured_request_byte") ||
			strings.Contains(cleanExpr, "syscall_trace_id") ||
			strings.Contains(cleanExpr, "signal_source") ||
			strings.Contains(cleanExpr, "l7_protocol") ||
			strings.Contains(cleanExpr, "endpoint") ||
			strings.Contains(cleanExpr, "x_request_id")
		if !existsInFlowLog {
			// The expression references concrete columns: keep it when every
			// column (after the local mapping) exists — collapsing unknown
			// metrics to count(*) fabricated wrong values (e.g. a "ratio" of
			// 234505 rows). A missing column means the metric cannot be
			// computed here: return NULL like the cloud contract does.
			cols := backtickColumns(originalExpr)
			if len(cols) > 0 {
				for _, c := range cols {
					if mapped, ok := flowLogLocalCol[c]; ok {
						c = mapped
					}
					if !checkHasColumn(check, db, tbl, c) {
						return "NULL"
					}
				}
				return normalized
			}
			return "count(*)"
		}
	}
	return normalized
}

// backtickColumns extracts backtick-quoted identifiers from a DSL expression.
func backtickColumns(expr string) []string {
	re := regexp.MustCompile("`([^`]+)`")
	ms := re.FindAllStringSubmatch(expr, -1)
	cols := make([]string, 0, len(ms))
	for _, m := range ms {
		cols = append(cols, m[1])
	}
	return cols
}

func BuildSelectSQL[T SqlRequest](req T, check ColumnChecker) (string, error) {
	if req.GetNumQueries() == 0 {
		return "", fmt.Errorf("no queries in request")
	}
	q := req.QueryAt(0)
	table := req.GetTable()
	if table == "" {
		table = "l7_flow_log"
	}

	resolvedTable := table
	resolvedDB := req.GetDatabase()
	if resolvedDB == "flow_metrics" && !strings.Contains(table, ".") {
		if req.GetDataSource() != "" {
			resolvedTable = table + "." + req.GetDataSource()
		} else {
			resolvedTable = table + ".1m"
		}
		// Granularity fallback: the environment may only retain coarser
		// tables (e.g. .1d). A requested-but-missing granularity made every
		// column check fail and the whole request fall back to cache.
		if resolved := ResolveFlowMetricsTable(check, resolvedDB, table, req.GetTimeEnd()-req.GetTimeStart(), req.GetDataSource()); resolved != "" {
			resolvedTable = resolved
		}
	}

	// Use _local table for flow_log to bypass broken Distributed table.
	if resolvedDB == "flow_log" && !strings.Contains(resolvedTable, "_local") {
		resolvedTable += "_local"
	}
	// Names are spliced into SQL inside backticks — validate before quoting so
	// a crafted request cannot break out of the identifier (SQL injection).
	if err := ValidateTableName(resolvedDB); err != nil {
		return "", err
	}
	if err := ValidateTableName(resolvedTable); err != nil {
		return "", err
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

	// Tag/column mappings come from the unified maps (clickhouse.TagExpr /
	// TagIDExpr / ColumnExpr / IDColumn), routed by database — no inline
	// per-database map here.
	isFlowLog := resolvedDB == "flow_log"

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
			tagVal := item.Expr[len("newTag(") : len(item.Expr)-1]
			tagVal = strings.Trim(tagVal, "'\"")
			// Escape single quotes in the constant value — it is spliced into
			// a quoted string literal (injection / SQL corruption).
			tagVal = strings.ReplaceAll(tagVal, "'", "\\'")
			constParts = append(constParts, fmt.Sprintf("'%s' AS `%s`", tagVal, item.Key))

		case isTag || (!isAgg && !isFunc):
			col := strings.Trim(item.Expr, "`")
			// Check for node_type, icon_id, enum function calls disguised as tags.
			if strings.HasPrefix(lower, "node_type(") {
				inner := strings.TrimSpace(item.Expr[len("node_type(") : len(item.Expr)-1])
				inner = strings.Trim(inner, "\"'")
				// node_type(auto_service_0/1) resolves through the
				// flow_tag.node_type_map dictionary on the per-side
				// auto_service_type column (verified against the live
				// dictionary: 0→internet_ip, 1→chost, ...).
				if inner == "auto_service_0" || inner == "auto_service_1" {
					side := inner[len(inner)-1:]
					selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", nodeTypeExpr("_"+side), item.Key))
					groupByParts = append(groupByParts, "`auto_service_type_"+side+"`")
				} else if side, ok := autoServiceSide(inner); ok && (check == nil || checkHasColumn(check, resolvedDB, resolvedTable, "auto_service_type"+side)) {
					// Unsuffixed auto_service: same dictionary on the plain
					// auto_service_type column (flow_metrics tables carry it;
					// flow_log does not, so the check falls back to literal).
					selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", nodeTypeExpr(side), item.Key))
					groupByParts = append(groupByParts, "`auto_service_type"+side+"`")
				} else {
					selectParts = append(selectParts, fmt.Sprintf("'%s' AS `%s`", inner, item.Key))
				}
			} else if strings.HasPrefix(lower, "icon_id(") {
				// icon_id(auto_service[_0/_1]) resolves through the device_map
				// dictionary (composite key on type+id), which carries the
				// per-device icon_id attribute (verified against the live
				// dictionary; node_type_map has no icon column). Unknown tags
				// keep the -13 placeholder.
				inner := strings.TrimSpace(item.Expr[len("icon_id(") : len(item.Expr)-1])
				inner = strings.Trim(inner, "\"'")
				if side, ok := autoServiceSide(inner); ok && (check == nil || checkHasColumn(check, resolvedDB, resolvedTable, "auto_service_type"+side)) {
					selectParts = append(selectParts, fmt.Sprintf("dictGetOrDefault('flow_tag.device_map', 'icon_id', (toUInt64(auto_service_type%s), toUInt64(auto_service_id%s)), toInt64(-13)) AS `%s`", side, side, item.Key))
					groupByParts = append(groupByParts, "`auto_service_id"+side+"`", "`auto_service_type"+side+"`")
				} else {
					selectParts = append(selectParts, fmt.Sprintf("toInt64(-13) AS `%s`", item.Key))
				}
			} else if strings.HasPrefix(lower, "enum(") {
				inner := strings.TrimSpace(item.Expr[len("enum(") : len(item.Expr)-1])
				inner = strings.Trim(inner, "\"'")
				if check != nil && !checkHasColumn(check, resolvedDB, resolvedTable, inner) {
					return "", fmt.Errorf("%w: %s", ErrUnsupportedColumn, inner)
				}
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", inner, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", inner))
			} else if _, err := fmt.Sscanf(col, "%f", new(float64)); err == nil {
				// Numeric literal (e.g., -42)
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", col, item.Key))
			} else if mapped := TagExpr(resolvedDB, table, col); mapped != "" {
				// Virtual tag: name-resolving expression for SELECT; physical
				// ID column for GROUP BY when one exists, otherwise the
				// expression itself (valid for pure computed tags like
				// is_internet — dictGet expressions must not be grouped).
				// flow_log: SELECT needs the any() form (non-grouped columns
				// like is_ipv4/ip4_0), GROUP BY the bare form.
				selExpr, gbExpr := mapped, mapped
				if resolvedDB == "flow_log" {
					selExpr = ColumnExpr(col, true)
					gbExpr = ColumnExpr(col, false)
				} else if strings.Contains(mapped, "$any(") {
					selExpr = ExpandAny(mapped, true)
					gbExpr = ExpandAny(mapped, false)
				}
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", selExpr, item.Key))
				if group := TagGroupExpr(resolvedDB, table, col); group != "" {
					groupByParts = append(groupByParts, group)
				} else if resolvedDB == "flow_log" && (col == "auto_service_0" || col == "auto_service_1") {
					// flow_log auto_service name resolution uses the
					// device_map composite key (type, id) — group on both
					// physical columns or the SELECT's dictGet reference to
					// auto_service_type_0 fails ("not in GROUP BY").
					side := col[len(col)-1:]
					groupByParts = append(groupByParts, "`auto_service_type_"+side+"`", "`auto_service_id_"+side+"`")
				} else if idCol := TagIDExpr(resolvedDB, table, col); idCol != "" && idCol != col {
					groupByParts = append(groupByParts, idCol)
				} else if !strings.Contains(gbExpr, "dictGet") {
					groupByParts = append(groupByParts, gbExpr)
				}
			} else if side := TagSideExpr(resolvedDB, table, col); side != "" {
				// _0/_1 client/server-side virtual tags (auto_service_0 etc.):
				// TagExpr doesn't resolve them (its map holds an empty
				// placeholder), so resolve through the side map like the Top
				// path does.
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", side, item.Key))
				if group := TagGroupExpr(resolvedDB, table, col); group != "" {
					groupByParts = append(groupByParts, group)
				} else if idCol := TagIDExpr(resolvedDB, table, col); idCol != "" && idCol != col {
					groupByParts = append(groupByParts, idCol)
				} else if g := TagSideGroupExpr(resolvedDB, table, col); g != "" {
					// Computed expression (is_internet_0/1): GROUP BY the
					// bare (non-any) expression.
					groupByParts = append(groupByParts, g)
				} else if strings.Contains(side, "(") {
					groupByParts = append(groupByParts, side)
				}
			} else if isFlowLog {
				if mapped := ColumnExpr(col, false); mapped != "" {
					selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", ColumnExpr(col, true), item.Key))
					groupByParts = append(groupByParts, mapped)
				} else if col == "is_internet_0" || col == "is_internet_1" {
					selectParts = append(selectParts, fmt.Sprintf("0 AS `%s`", col))
				} else if metric, ok := flowLogMetricMap[col]; ok {
					// flow_log has no request/error columns; each row is one
					// request, so metrics are derived from response_status.
					selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", metric, item.Key))
				} else {
					selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
					groupByParts = append(groupByParts, fmt.Sprintf("`%s`", col))
				}
			} else if col == "is_internet_0" || col == "is_internet_1" {
				selectParts = append(selectParts, fmt.Sprintf("0 AS `%s`", col))
			} else {
				if !checkHasColumn(check, resolvedDB, resolvedTable, col) {
					return "", fmt.Errorf("%w: %s", ErrUnsupportedColumn, col)
				}
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", col))
			}
		case isAgg:
			sqlExpr := expr
			if isFlowLog {
				sqlExpr = GetFlowLogExpr(sqlExpr, item.Expr, table, check, resolvedDB, resolvedTable)
			}
			selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", sqlExpr, item.Key))

		case strings.HasPrefix(lower, "node_type("):
			inner := strings.TrimSpace(item.Expr[len("node_type(") : len(item.Expr)-1])
			inner = strings.Trim(inner, "\"'")
			if side, ok := autoServiceSide(inner); ok && (check == nil || checkHasColumn(check, resolvedDB, resolvedTable, "auto_service_type"+side)) {
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", nodeTypeExpr(side), item.Key))
				groupByParts = append(groupByParts, "`auto_service_type"+side+"`")
			} else {
				selectParts = append(selectParts, fmt.Sprintf("toString(`%s`) AS `%s`", inner, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", inner))
			}

		case strings.HasPrefix(lower, "icon_id("):
			inner := strings.TrimSpace(item.Expr[len("icon_id(") : len(item.Expr)-1])
			inner = strings.Trim(inner, "\"'")
			if side, ok := autoServiceSide(inner); ok && (check == nil || checkHasColumn(check, resolvedDB, resolvedTable, "auto_service_type"+side)) {
				selectParts = append(selectParts, fmt.Sprintf("dictGetOrDefault('flow_tag.device_map', 'icon_id', (toUInt64(auto_service_type%s), toUInt64(auto_service_id%s)), toInt64(-13)) AS `%s`", side, side, item.Key))
				groupByParts = append(groupByParts, "`auto_service_id"+side+"`", "`auto_service_type"+side+"`")
			} else {
				selectParts = append(selectParts, fmt.Sprintf("toInt64(-13) AS `%s`", item.Key))
			}

		case strings.HasPrefix(lower, "enum("):
			inner := strings.TrimSpace(item.Expr[len("enum(") : len(item.Expr)-1])
			selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", inner, item.Key))
			groupByParts = append(groupByParts, fmt.Sprintf("`%s`", inner))

		default:
			col := strings.Trim(item.Expr, "`")
			if mapped := TagExpr(resolvedDB, table, col); mapped != "" {
				selExpr := mapped
				if strings.Contains(mapped, "$any(") {
					selExpr = ExpandAny(mapped, true)
				}
				selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", selExpr, item.Key))
				if group := TagGroupExpr(resolvedDB, table, col); group != "" {
					groupByParts = append(groupByParts, group)
				} else if idCol := TagIDExpr(resolvedDB, table, col); idCol != "" && idCol != col {
					groupByParts = append(groupByParts, idCol)
				} else if !strings.Contains(mapped, "dictGet") {
					groupByParts = append(groupByParts, ExpandAny(mapped, false))
				}
			} else if isFlowLog {
				if mapped := ColumnExpr(col, false); mapped != "" {
					selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", mapped, item.Key))
					groupByParts = append(groupByParts, mapped)
				} else if metric, ok := flowLogMetricMap[col]; ok {
					selectParts = append(selectParts, fmt.Sprintf("%s AS `%s`", metric, item.Key))
				} else {
					selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
					groupByParts = append(groupByParts, fmt.Sprintf("`%s`", col))
				}
			} else {
				if !checkHasColumn(check, resolvedDB, resolvedTable, col) {
					return "", fmt.Errorf("%w: %s", ErrUnsupportedColumn, col)
				}
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", col))
			}
		}
	}

	// Auto-add required columns that the cloud backend always returns.
	// These are expected by the frontend even when not in SELECT/TAGS.
	if resolvedDB == "flow_metrics" {
		// auto_service_type_0/1 — needed for node_type/icon_id resolution
		for _, suffix := range []string{"_0", "_1"} {
			colName := "auto_service_type" + suffix
			already := false
			for _, sp := range selectParts {
				if strings.Contains(sp, "`"+colName+"`") {
					already = true
					break
				}
			}
			if !already && gbSet["auto_service"+suffix] {
				selectParts = append(selectParts, fmt.Sprintf("`%s` AS `%s`", colName, colName))
				groupByParts = append(groupByParts, fmt.Sprintf("`%s`", colName))
			}
		}
		// resource_l7_protocol_0/1 — protocol label for topology. Only added
		// when the table carries l7_protocol (network_map has only protocol —
		// the cloud's network_map Top rows carry no resource_l7_protocol_0/1
		// either, so skipping matches the contract).
		for _, suffix := range []string{"_0", "_1"} {
			colName := "resource_l7_protocol" + suffix
			already := false
			for _, sp := range selectParts {
				if strings.Contains(sp, "`"+colName+"`") {
					already = true
					break
				}
			}
			if !already && gbSet["auto_service"+suffix] && checkHasColumn(check, resolvedDB, resolvedTable, "l7_protocol") {
				selectParts = append(selectParts, fmt.Sprintf("`l7_protocol` AS `%s`", colName))
				groupByParts = append(groupByParts, "`l7_protocol`")
			}
		}
		// Unsuffixed auto_service (List view): the cloud response carries
		// auto_service_type and resource_l7_protocol rows regardless of the
		// request. Only add them when the physical columns exist.
		if gbSet["auto_service"] {
			if !selectHasColumn(selectParts, "auto_service_type") && checkHasColumn(check, resolvedDB, resolvedTable, "auto_service_type") {
				selectParts = append(selectParts, "`auto_service_type` AS `auto_service_type`")
				groupByParts = append(groupByParts, "`auto_service_type`")
			}
			// resource_l7_protocol: the cloud's physical column is an array
			// (arrayStringConcat of the first element); environments without
			// it (local CH) resolve the plain l7_protocol enum value through
			// the flow_tag.int_enum_map dictionary, matching the cloud string
			// contract ("HTTP", "DNS", ...).
			if !selectHasColumn(selectParts, "resource_l7_protocol") {
				if checkHasColumn(check, resolvedDB, resolvedTable, "array_resource_l7_protocol") {
					selectParts = append(selectParts, "arrayStringConcat(tupleElement(`array_resource_l7_protocol`, 1), ',') AS `resource_l7_protocol`")
				} else if checkHasColumn(check, resolvedDB, resolvedTable, "l7_protocol") {
					selectParts = append(selectParts, "dictGetOrDefault('flow_tag.int_enum_map', 'name_zh', ('l7_protocol', toUInt64(l7_protocol)), '') AS `resource_l7_protocol`")
					groupByParts = append(groupByParts, "`l7_protocol`")
				}
			}
		}
	}

	var sql strings.Builder
	sql.WriteString("SELECT ")
	allSelects := append(constParts, selectParts...)
	sql.WriteString(strings.Join(allSelects, ", "))
	sql.WriteString(fmt.Sprintf(" FROM %s", fullTable))

	var wheres []string
	if req.GetTimeStart() > 0 {
		// Unix integers avoid timezone shifts (see CLAUDE.md rule 4).
		wheres = append(wheres, fmt.Sprintf("time >= %d", req.GetTimeStart()))
	}
	if req.GetTimeEnd() > 0 {
		wheres = append(wheres, fmt.Sprintf("time <= %d", req.GetTimeEnd()))
	}
	if q.Where != "" {
		// CleanWhereClause handles profile Enum rewrite, exist() conversion,
		// ip→ip4 mapping, is_internet/role stripping and parenthesis balance.
		cleanWhere := CleanWhereClause(q.Where, resolvedDB, table)
		cleanWhere = strings.TrimSpace(cleanWhere)
		cleanWhere = strings.TrimPrefix(cleanWhere, "AND ")
		cleanWhere = strings.TrimPrefix(cleanWhere, "OR ")
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

	// HAVING clause — normalize aggregate expressions to CH equivalents.
	if q.Having != "" {
		cleanHaving := CleanWhereClause(q.Having, resolvedDB, table)
		cleanHaving = strings.ReplaceAll(cleanHaving, "`ip_0`", "`ip4_0`")
		cleanHaving = strings.ReplaceAll(cleanHaving, "`ip_1`", "`ip4_1`")
		cleanHaving = strings.ReplaceAll(cleanHaving, "ip_0", "ip4_0")
		cleanHaving = strings.ReplaceAll(cleanHaving, "ip_1", "ip4_1")
		// Normalize aggregate function references in HAVING using NormalizeExpr.
		// This maps Avg(server_error_ratio) → avg(nullif(server_error, 0) / nullif(request, 0)) etc.
		normalized := normalizeHavingExpr(cleanHaving)
		sql.WriteString(" HAVING ")
		sql.WriteString(normalized)
	}

	if req.GetSortOrderBy() != "" {
		dir := "ASC"
		if strings.ToUpper(req.GetSortSortedBy()) == "DESC" {
			dir = "DESC"
		}
		// ORDER_BY matches a SELECT alias (frontend sends e.g. `Count(行数)`,
		// the alias of Count(`row`)), so it must end up as a bare identifier
		// here. Strip quote characters defensively: a stray quote would make
		// the whole `'alias'` read as a column name and fail the query.
		sortBy := strings.Trim(strings.ReplaceAll(req.GetSortOrderBy(), "`", ""), "' ")
		sql.WriteString(fmt.Sprintf(" ORDER BY `%s` %s", sortBy, dir))
	}

	if req.GetPageSize() > 0 {
		sql.WriteString(fmt.Sprintf(" LIMIT %d", req.GetPageSize()))
		if req.GetPageIndex() > 1 {
			sql.WriteString(fmt.Sprintf(" OFFSET %d", (req.GetPageIndex()-1)*req.GetPageSize()))
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
	// Use _local table for flow_log to bypass the broken Distributed table
	// (same workaround as BuildSelectSQL).
	if db == "flow_log" && !strings.Contains(resolvedTable, "_local") {
		resolvedTable += "_local"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)

	interval := "toStartOfMinute(time)"
	if req.TimeEnd-req.TimeStart > 7200 {
		interval = "toStartOfHour(time)"
	}

	var wheres []string
	if req.TimeStart > 0 {
		// Unix integers avoid timezone shifts (see CLAUDE.md rule 4).
		wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart))
	}
	if req.TimeEnd > 0 {
		wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd))
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

// parenLeadingAndRE matches a dangling AND/OR right after "(", left by
// stripping the first condition of a parenthesized group.
var parenLeadingAndRE = regexp.MustCompile(`\(\s*(?:AND|OR)\s+`)

// ipCondRE matches an ip = 'x.x.x.x' condition (with optional backticks
// and whitespace around the operator).
var ipCondRE = regexp.MustCompile("(?:`?)ip(?:`?)\\s*=\\s*'([^']+)'")

// IPConditionToSide maps the virtual ip tag to the physical IP columns:
// flow_log tables carry per-side columns (ip4_0/ip4_1/ip6_0/ip6_1);
// application_log's log table carries single-sided ip4/ip6 columns.
func IPConditionToSide(where string) string {
	return ipCondRE.ReplaceAllStringFunc(where, func(m string) string {
		v := ipCondRE.FindStringSubmatch(m)[1]
		if strings.Contains(v, ":") {
			return fmt.Sprintf("(ip6_0 = IPv6StringToNum('%s') OR ip6_1 = IPv6StringToNum('%s'))", v, v)
		}
		return fmt.Sprintf("(ip4_0 = IPv4StringToNum('%s') OR ip4_1 = IPv4StringToNum('%s'))", v, v)
	})
}

// IPConditionToSingle maps the virtual ip tag to the single-sided IP columns
// of application_log's log table (ip4/ip6 — no _0/_1 variants).
func IPConditionToSingle(where string) string {
	return ipCondRE.ReplaceAllStringFunc(where, func(m string) string {
		v := ipCondRE.FindStringSubmatch(m)[1]
		if strings.Contains(v, ":") {
			return fmt.Sprintf("(ip6 = IPv6StringToNum('%s'))", v)
		}
		return fmt.Sprintf("(ip4 = IPv4StringToNum('%s'))", v)
	})
}

// CleanWhereClause converts exist() calls into physical column predicates:
// exist(tag) means "the resource tag has a value", i.e. its physical ID
// column is non-zero (e.g. exist(chost_id) → `l3_device_id` != 0).
func CleanWhereClause(where, db, table string) string {
	// profile tables store enum names directly in the column (e.g.
	// profile_language_type = 'eBPF'), so Enum(`x`) = 'v' collapses to
	// `x` = 'v'. flow_log/flow_metrics are NOT rewritten — their enum
	// columns hold numeric IDs resolved through string_enum_map.
	if db == "profile" {
		enumColRE := regexp.MustCompile("(?i)Enum\\((?:`[^`]+`|[A-Za-z_][A-Za-z0-9_]*)\\)")
		where = enumColRE.ReplaceAllStringFunc(where, func(m string) string {
			return m[len("Enum(") : len(m)-1]
		})
	}
	for {
		idx := strings.Index(strings.ToLower(where), "exist(")
		if idx < 0 {
			break
		}
		// Find the matching closing paren (handles nested calls).
		depth := 1
		end := idx + len("exist(")
		for end < len(where) && depth > 0 {
			switch where[end] {
			case '(':
				depth++
			case ')':
				depth--
			}
			end++
		}
		if depth != 0 {
			return where
		}
		inner := strings.TrimSpace(where[idx+len("exist(") : end-1])
		inner = strings.Trim(inner, "`")
		replacement := "1=1"
		if idCol := TagIDExpr(db, table, inner); idCol != "" && idCol != inner {
			replacement = fmt.Sprintf("`%s` != 0", idCol)
		} else if db == "flow_log" && !strings.HasSuffix(inner, "_0") && !strings.HasSuffix(inner, "_1") {
			// flow_log has per-side columns: exist(tag) means either side
			// has a value (e.g. exist(chost_id) → l3_device_id_0/1).
			id0, id1 := IDColumn(inner+"_0"), IDColumn(inner+"_1")
			if id0 != inner+"_0" && id1 != inner+"_1" {
				replacement = fmt.Sprintf("(`%s` != 0 OR `%s` != 0)", id0, id1)
			}
		}
		where = where[:idx] + replacement + where[end:]
	}

	// Map API column names to physical CH column names.
	where = strings.ReplaceAll(where, "`ip_0`", "`ip4_0`")
	where = strings.ReplaceAll(where, "`ip_1`", "`ip4_1`")
	where = strings.ReplaceAll(where, "ip_0", "ip4_0")
	where = strings.ReplaceAll(where, "ip_1", "ip4_1")
	// Strip is_internet/role conditions (ZT virtual cols, not in CH). Shared
	// by List (BuildSelectSQL) and Top (query/top) — the Top history query
	// previously passed them through and failed with "Missing columns".
	for _, vcol := range []string{"is_internet_0", "is_internet_1", "role"} {
		for _, pat := range []string{"`" + vcol + "`", vcol} {
			for {
				idx := strings.Index(where, pat)
				if idx < 0 {
					break
				}
				// Find the end of this condition.
				scan := idx
				for scan < len(where) {
					if where[scan] == ')' {
						scan++
						break
					}
					if scan > idx && scan+4 < len(where) && (where[scan:scan+5] == " AND " || where[scan:scan+4] == " OR ") {
						break
					}
					scan++
				}
				// Backtrack to the start of this condition (AND/OR/EOL).
				start := idx
				preAnd := strings.LastIndex(where[:start], "AND ")
				preOr := strings.LastIndex(where[:start], "OR ")
				if preOr > preAnd {
					preAnd = preOr
				}
				if start > 0 && where[start-1] == '(' {
					preAnd = strings.LastIndex(where[:start-1], "(")
				}
				if preAnd >= 0 {
					start = preAnd
				}
				where = where[:start] + where[scan:]
			}
		}
	}
	// Stripping a parenthesized condition ("(subnet_id=0 AND role=0)")
	// removes "AND role=0)" and leaves an unmatched "(" — drop orphan parens.
	// A stripped condition at the start of a parenthesized group leaves
	// "( AND subnet_id=0 ..." (parens still balanced, so BalanceParens keeps
	// them) — drop the dangling AND/OR right after "(".
	where = parenLeadingAndRE.ReplaceAllString(where, "(")
	where = BalanceParens(where)
	where = strings.TrimSpace(where)
	where = strings.TrimPrefix(where, "AND ")
	where = strings.TrimPrefix(where, "OR ")
	return where
}
