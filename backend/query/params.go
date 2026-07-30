package query

import (
	"fmt"
	"strings"
)

// QueryParams holds common query parameters extracted from API requests.
// All handler/query functions should use this instead of manually building WHERE/LIMIT/ORDER BY.
type QueryParams struct {
	TimeStart int64
	TimeEnd   int64
	Limit     int
	Offset    int
	OrderBy   string
	SortedBy  string
	GroupBy   string
	Database  string
	Table     string
}

// Defaults fills in sensible defaults for unset fields.
func (p *QueryParams) Defaults() {
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Database == "" {
		p.Database = "flow_log"
	}
	if p.Table == "" {
		p.Table = "l7_flow_log"
	}
	if p.SortedBy == "" {
		p.SortedBy = "DESC"
	}
}

// WhereSQL returns the time-range WHERE clause.
func (p *QueryParams) WhereSQL() string {
	var parts []string
	if p.TimeStart > 0 {
		parts = append(parts, fmt.Sprintf("time >= %d", p.TimeStart))
	}
	if p.TimeEnd > 0 {
		parts = append(parts, fmt.Sprintf("time <= %d", p.TimeEnd))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " AND ")
}

// BuildSQL builds a complete ClickHouse SELECT query with standard clauses.
// extraWhere and extraGroupBy are appended after the time range and before ORDER BY.
func (p *QueryParams) BuildSQL(sel, extraWhere, extraGroupBy string) string {
	p.Defaults()

	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(sel)
	b.WriteString(" FROM `")
	b.WriteString(p.Database)
	b.WriteString("`.`")
	b.WriteString(p.Table)
	b.WriteString("`")

	where := p.WhereSQL()
	if extraWhere != "" {
		if where != "" {
			where += " AND " + extraWhere
		} else {
			where = extraWhere
		}
	}
	if where != "" {
		b.WriteString(" WHERE ")
		b.WriteString(where)
	}

	groupBy := p.GroupBy
	if extraGroupBy != "" {
		if groupBy != "" {
			groupBy += ", " + extraGroupBy
		} else {
			groupBy = extraGroupBy
		}
	}
	if groupBy != "" {
		b.WriteString(" GROUP BY ")
		b.WriteString(groupBy)
	}

	if p.OrderBy != "" {
		cleaned := strings.Trim(p.OrderBy, "'\"`")
		b.WriteString(fmt.Sprintf(" ORDER BY `%s` %s", cleaned, p.SortedBy))
	}

	if p.Limit > 0 {
		b.WriteString(fmt.Sprintf(" LIMIT %d", p.Limit))
	}
	if p.Offset > 0 {
		b.WriteString(fmt.Sprintf(" OFFSET %d", p.Offset))
	}
	return b.String()
}
