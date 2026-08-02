package clickhouse

// SqlRequest is the SQL-builder view of a querier request. Both the wire
// type (clickhouse.QuerierRequest) and the transport type
// (query.QuerierListRequest) implement it, so SQL construction is shared
// without a field-mapping bridge.
//
// Methods are Get*-prefixed because Go forbids zero-argument methods whose
// name collides with a struct field (the types have Database/Table/... fields).
type SqlRequest interface {
	GetDatabase() string
	GetTable() string
	GetDataSource() string
	GetTimeStart() int64
	GetTimeEnd() int64
	GetInterval() int
	GetFill() string
	GetPageIndex() int
	GetPageSize() int
	// GetSortOrderBy / GetSortSortedBy return "" when no SORT clause is present.
	GetSortOrderBy() string
	GetSortSortedBy() string
	GetNumQueries() int
	QueryAt(i int) SqlSub
}

// SqlSub is the sub-query projection the builder needs (Roles/CTags/STags
// are not used by the ClickHouse path).
type SqlSub struct {
	QueryID string
	Select  string
	Where   string
	Having  string
	Tags    []string
	Metrics []string
	GroupBy string
}

// --- clickhouse.QuerierRequest implementation --------------------------------

func (r QuerierRequest) GetDatabase() string   { return r.Database }
func (r QuerierRequest) GetTable() string      { return r.Table }
func (r QuerierRequest) GetDataSource() string { return r.DataSource }
func (r QuerierRequest) GetTimeStart() int64   { return r.TimeStart }
func (r QuerierRequest) GetTimeEnd() int64     { return r.TimeEnd }
func (r QuerierRequest) GetInterval() int      { return r.Interval }
func (r QuerierRequest) GetFill() string       { return r.Fill }
func (r QuerierRequest) GetPageIndex() int     { return r.PageIndex }
func (r QuerierRequest) GetPageSize() int      { return r.PageSize }

func (r QuerierRequest) GetSortOrderBy() string {
	if r.Sort == nil {
		return ""
	}
	return r.Sort.OrderBy
}

func (r QuerierRequest) GetSortSortedBy() string {
	if r.Sort == nil {
		return ""
	}
	return r.Sort.SortedBy
}

func (r QuerierRequest) GetNumQueries() int { return len(r.Queries) }
func (r QuerierRequest) QueryAt(i int) SqlSub {
	q := r.Queries[i]
	return SqlSub{
		QueryID: q.QueryID,
		Select:  q.Select,
		Where:   q.Where,
		Having:  q.Having,
		Tags:    q.Tags,
		Metrics: q.Metrics,
		GroupBy: q.GroupBy,
	}
}

var _ SqlRequest = QuerierRequest{}
