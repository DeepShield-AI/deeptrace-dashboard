package query

import (
	"fmt"
	"strings"
)

// BuildBaseSQL builds common SQL shared by List, Top, FlowLogDetailList, fast_list.
// Defined in the query package so all sub-packages (querier, flowlog) can use it
// without circular imports.
//
//	sel       → SELECT expressions
//	tbl       → FROM table
//	extras    → extra WHERE clauses
//	timeStart → time >= N (0 = skip)
//	timeEnd   → time <= N (0 = skip)
//	groupBy   → GROUP BY ("" = skip)
//	orderBy   → ORDER BY column ("" = skip)
//	sortedBy  → ASC/DESC
//	limit     → LIMIT (0 = no limit)
//	offset    → OFFSET (0 = no offset)
func BuildBaseSQL(sel, tbl string, extras []string, timeStart, timeEnd int64,
	groupBy, orderBy, sortedBy string, limit, offset int) string {
	if sel == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(sel)
	b.WriteString(" FROM `")
	b.WriteString(tbl)
	b.WriteString("`")

	var clauses []string
	if timeStart > 0 {
		clauses = append(clauses, fmt.Sprintf("time >= %d", timeStart))
	}
	if timeEnd > 0 {
		clauses = append(clauses, fmt.Sprintf("time <= %d", timeEnd))
	}
	clauses = append(clauses, extras...)
	if len(clauses) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(clauses, " AND "))
	}
	if groupBy != "" {
		b.WriteString(" GROUP BY ")
		b.WriteString(groupBy)
	}
	if orderBy != "" {
		dir := "ASC"
		if strings.ToUpper(sortedBy) == "DESC" {
			dir = "DESC"
		}
		// Strip surrounding quotes from ORDER BY.
		// The Statistics internal format sometimes sends single-quoted identifiers,
		// e.g. ORDER_BY: "'Maxresponse_duration'". Wrapping those in backticks as-is
		// would produce `` `'Maxresponse_duration'` `` which ZT cannot resolve.
		cleaned := strings.Trim(orderBy, "'\"`")
		fmt.Fprintf(&b, " ORDER BY `%s` %s", cleaned, dir)
	}
	if limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", limit)
		if offset > 0 {
			fmt.Fprintf(&b, " OFFSET %d", offset)
		}
	}
	return b.String()
}
