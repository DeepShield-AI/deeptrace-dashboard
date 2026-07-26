package querier

import (
	"fmt"
	"deeptrace-backend/query"
	"regexp"
	"strings"

	"deeptrace-backend/clickhouse"
)


// buildSQL wraps buildBaseSQL with List/Top-specific logic.
// For Top with GROUP BY: strips DSL functions, adds time HISTORY.
// For List / Top without GROUP BY: passes through.
func buildSQL(sel, tbl, whereCond string, timeStart, timeEnd int64,
	groupBy, origSel string, topN, pageSize, pageIndex, interval int,
	orderBy, sortedBy string, isTop bool) (string, bool) {

	if sel == "" {
		return "", false
	}
	extras := []string{}
	if whereCond != "" {
		extras = append(extras, whereCond)
	}

	if isTop {
		sel = cleanSelect(sel)
		if groupBy != "" {
			// HISTORY mode: add time, build GROUP BY with passthrough stripping
			sel = "`time`, " + sel
			var gbParts []string
			for _, rc := range stripPassthroughGroupBy(groupBy, origSel) {
				gbParts = append(gbParts, rc)
			}
			gb := "`time`"
			if len(gbParts) > 0 {
				gb += ", " + strings.Join(gbParts, ", ")
			}
			span := 100
			if s := int(timeEnd - timeStart); s > 0 {
				span = s
			}
			return query.BuildBaseSQL(sel, tbl, extras, timeStart, timeEnd,
				gb, "time", "DESC", span, 0), true
		}
	}

	// Non-HISTORY (List, Top without groupBy)
	limit := pageSize
	if topN > 0 {
		limit = topN
	}
	if limit <= 0 {
		limit = 100
	}
	offset := 0
	if pageIndex > 1 {
		offset = (pageIndex - 1) * pageSize
	}
	return query.BuildBaseSQL(sel, tbl, extras, timeStart, timeEnd,
		"", orderBy, sortedBy, limit, offset), false
}

// cleanSelect strips DeepFlow DSL functions for Top GROUP BY queries.
func cleanSelect(sel string) string {
	if sel == "" {
		return ""
	}
	re := regexp.MustCompile(`(?i)(?:newTag|node_type|Enum|icon_id)\s*\([^)]*\)\s+AS\s+(?:[a-zA-Z_0-9]+|` + "`[^`]+`" + `)\s*,?\s*`)
	sel = re.ReplaceAllString(sel, "")
	re2 := regexp.MustCompile(`(?i)-?\d+\.?\d*\s+AS\s+(?:[a-zA-Z_0-9]+|` + "`[^`]+`" + `)\s*,?\s*`)
	sel = re2.ReplaceAllString(sel, "")
	sel = strings.TrimSpace(sel)
	return strings.TrimRight(sel, ", ")
}

// stripPassthroughGroupBy removes passthrough aliases from GROUP BY.
func stripPassthroughGroupBy(groupBy, sel string) []string {
	if groupBy == "" {
		return nil
	}
	aliases := map[string]bool{}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		if strings.HasPrefix(lower, "newtag(") ||
			strings.HasPrefix(lower, "enum(") ||
			strings.HasPrefix(lower, "node_type(") ||
			strings.HasPrefix(lower, "icon_id(") ||
			isNum(item.Expr) {
			aliases[item.Key] = true
		}
	}
	var result []string
	for _, item := range clickhouse.ParseSelectList(groupBy) {
		col := strings.Trim(item.Key, "`")
		if aliases[col] {
			continue
		}
		result = append(result, fmt.Sprintf("`%s`", col))
	}
	return result
}
