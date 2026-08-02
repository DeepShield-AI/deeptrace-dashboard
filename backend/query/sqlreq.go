package query

import "deeptrace-backend/clickhouse"

// QuerierListRequest implements clickhouse.SqlRequest so the shared SQL
// builder can consume it directly (no field-mapping bridge).
func (r QuerierListRequest) GetDatabase() string   { return r.Database }
func (r QuerierListRequest) GetTable() string      { return r.Table }
func (r QuerierListRequest) GetDataSource() string { return r.DataSource }
func (r QuerierListRequest) GetTimeStart() int64   { return r.TimeStart }
func (r QuerierListRequest) GetTimeEnd() int64     { return r.TimeEnd }
func (r QuerierListRequest) GetInterval() int      { return r.Interval }
func (r QuerierListRequest) GetFill() string       { return r.Fill }
func (r QuerierListRequest) GetPageIndex() int     { return r.PageIndex }
func (r QuerierListRequest) GetPageSize() int      { return r.PageSize }

func (r QuerierListRequest) GetSortOrderBy() string {
	if r.Sort == nil {
		return ""
	}
	return r.Sort.OrderBy
}

func (r QuerierListRequest) GetSortSortedBy() string {
	if r.Sort == nil {
		return ""
	}
	return r.Sort.SortedBy
}

func (r QuerierListRequest) GetNumQueries() int { return len(r.Queries) }

func (r QuerierListRequest) QueryAt(i int) clickhouse.SqlSub {
	q := r.Queries[i]
	return clickhouse.SqlSub{
		QueryID: q.QueryID,
		Select:  q.Select,
		Where:   q.Where,
		Having:  q.Having,
		Tags:    q.Tags,
		Metrics: q.Metrics,
		GroupBy: q.GroupBy,
	}
}

var _ clickhouse.SqlRequest = QuerierListRequest{}
