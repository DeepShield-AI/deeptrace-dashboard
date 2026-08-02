package top

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"deeptrace-backend/clickhouse"
)

// ---------------------------------------------------------------------------
// Fake ClickHouse Querier
// ---------------------------------------------------------------------------

type fakeColumnType struct{}

func (fakeColumnType) Name() string                      { return "" }
func (fakeColumnType) DatabaseTypeName() string          { return "Float64" }
func (fakeColumnType) ScanType() reflect.Type            { return reflect.TypeOf(float64(0)) }
func (fakeColumnType) Length() (int64, bool)             { return 0, false }
func (fakeColumnType) DecimalSize() (int64, int64, bool) { return 0, 0, false }
func (fakeColumnType) Nullable() bool                    { return false }

type fakeRows struct {
	cols []string
	rows [][]interface{}
	cur  []interface{}
	idx  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) ColumnTypes() []driver.ColumnType {
	ct := make([]driver.ColumnType, len(r.cols))
	for i := range ct {
		ct[i] = fakeColumnType{}
	}
	return ct
}
func (r *fakeRows) Close() error              { return nil }
func (r *fakeRows) Err() error                { return nil }
func (r *fakeRows) HasData() bool             { return r.idx < len(r.rows) }
func (r *fakeRows) ScanStruct(dest any) error { return nil }
func (r *fakeRows) Totals(dest ...any) error  { return nil }
func (r *fakeRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.cur = r.rows[r.idx]
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	for i, d := range dest {
		if i >= len(r.cur) {
			continue
		}
		switch t := d.(type) {
		case *float64:
			switch v := r.cur[i].(type) {
			case float64:
				*t = v
			case int:
				*t = float64(v)
			}
		case *interface{}:
			*t = r.cur[i]
		}
	}
	return nil
}

// fakeQuerier dispatches by SQL shape: HISTORY queries (GROUP BY toi) and the
// plain LIMIT 1 probe return no rows; grouped queries return groupRows.
type fakeQuerier struct {
	groupRows [][]interface{}
	queried   []string
	// resolve maps a flow_metrics family to the actual granularity table
	// (granularity fallback tests).
	resolve map[string]string
}

func (f *fakeQuerier) Enabled() bool { return true }
func (f *fakeQuerier) HasColumn(db, table, col string) bool {
	return true // every column exists; ErrUnsupportedColumn paths aren't tested here
}

func (f *fakeQuerier) ResolveTable(db, family string, windowSec int64, dataSource string) string {
	if f.resolve != nil {
		if name, ok := f.resolve[family]; ok {
			return name
		}
	}
	return ""
}

func (f *fakeQuerier) Query(ctx context.Context, query string, args ...interface{}) (driver.Rows, error) {
	f.queried = append(f.queried, query)
	if strings.Contains(query, "GROUP BY toi") || !strings.Contains(query, "GROUP BY") {
		return &fakeRows{cols: []string{"toi"}}, nil
	}
	return &fakeRows{cols: []string{"cnt", "l7_protocol", "l7_protocol"}, rows: f.groupRows}, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func groupByRequest() clickhouse.QuerierRequest {
	return clickhouse.QuerierRequest{
		Database:  "flow_log",
		Table:     "l7_flow_log",
		TimeStart: 1000,
		TimeEnd:   2000,
		Queries: []clickhouse.QuerierSub{{
			Select:  "count(*) AS cnt",
			GroupBy: "l7_protocol",
		}},
	}
}

func TestTopGroupByKeepsAllGroups(t *testing.T) {
	// Regression for the UID bug: a GROUP_BY-only (flat format) request used
	// to give every row UID "_" so all but the first group were dropped.
	fq := &fakeQuerier{
		groupRows: [][]interface{}{
			{float64(10), float64(1), float64(1)},
			{float64(20), float64(2), float64(2)},
		},
	}
	result, err := QueryTop(fq, context.Background(), groupByRequest())
	if err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("got %d rows, want 2 (all groups kept)", len(result.Data))
	}
	uids := map[string]bool{}
	for _, row := range result.Data {
		uids[row["UID"].(string)] = true
		// Group column must be present in the result row (flat format).
		if _, ok := row["l7_protocol"]; !ok {
			t.Fatalf("row missing l7_protocol: %v", row)
		}
	}
	if !uids["l7_protocol=1"] || !uids["l7_protocol=2"] {
		t.Fatalf("UIDs = %v, want l7_protocol=1 and l7_protocol=2", uids)
	}
}

func TestTopGroupByOrdersBySort(t *testing.T) {
	req := groupByRequest()
	req.Sort = &clickhouse.QuerierSort{OrderBy: "cnt", SortedBy: "DESC"}
	fq := &fakeQuerier{
		groupRows: [][]interface{}{{float64(10), float64(1), float64(1)}},
	}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	var groupSQL string
	for _, q := range fq.queried {
		if strings.Contains(q, "GROUP BY") && !strings.Contains(q, "GROUP BY toi") {
			groupSQL = q
		}
	}
	if groupSQL == "" {
		t.Fatal("no grouped query issued")
	}
	if !strings.Contains(groupSQL, "ORDER BY `cnt` DESC") {
		t.Fatalf("grouped SQL = %q, want ORDER BY `cnt` DESC", groupSQL)
	}
}

func TestTopGroupByOrdersByFirstMetricWithoutSort(t *testing.T) {
	fq := &fakeQuerier{
		groupRows: [][]interface{}{{float64(10), float64(1), float64(1)}},
	}
	if _, err := QueryTop(fq, context.Background(), groupByRequest()); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	var groupSQL string
	for _, q := range fq.queried {
		if strings.Contains(q, "GROUP BY") && !strings.Contains(q, "GROUP BY toi") {
			groupSQL = q
		}
	}
	if !strings.Contains(groupSQL, "ORDER BY `cnt`") {
		t.Fatalf("grouped SQL = %q, want ORDER BY first metric `cnt`", groupSQL)
	}
}

func TestTopIntervalBucketUsesRequestInterval(t *testing.T) {
	// Regression: the time bucket was hardcoded to toIntervalSecond(1),
	// mismatching the HISTORY interval (default 300s).
	req := groupByRequest()
	req.Interval = 300
	fq := &fakeQuerier{}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	if len(fq.queried) == 0 {
		t.Fatal("no SQL issued")
	}
	if !strings.Contains(fq.queried[0], "toIntervalSecond(300)") {
		t.Fatalf("main SQL = %q, want toIntervalSecond(300)", fq.queried[0])
	}
	if strings.Contains(fq.queried[0], "toIntervalSecond(1)") {
		t.Fatalf("main SQL = %q, hardcoded 1-second bucket", fq.queried[0])
	}
}

func TestTopTagsFormatKeepsAllGroups(t *testing.T) {
	// TAGS-format requests must also keep every group (regression guard).
	req := clickhouse.QuerierRequest{
		Database:  "flow_log",
		Table:     "l7_flow_log",
		TimeStart: 1000,
		TimeEnd:   2000,
		Queries: []clickhouse.QuerierSub{{
			Select: "count(*) AS cnt",
			Tags:   []string{"l7_protocol"},
		}},
	}
	fq := &fakeQuerier{
		groupRows: [][]interface{}{
			{float64(10), float64(1), float64(1)},
			{float64(20), float64(2), float64(2)},
		},
	}
	result, err := QueryTop(fq, context.Background(), req)
	if err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("got %d rows, want 2", len(result.Data))
	}
}

func TestTopFlowLogCapitalizedRequestMetricRewrites(t *testing.T) {
	// Regression: the frontend sends DSL functions capitalized
	// (Sum(request) AS 请求总量). A case-sensitive rewrite left the
	// expression intact and ClickHouse failed on the missing 'request'
	// column of l7_flow_log.
	req := clickhouse.QuerierRequest{
		Database:  "flow_log",
		Table:     "l7_flow_log",
		TimeStart: 1000,
		TimeEnd:   2000,
		Queries: []clickhouse.QuerierSub{{
			Select: "Sum(request) AS 请求总量",
		}},
	}
	fq := &fakeQuerier{}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	if len(fq.queried) == 0 {
		t.Fatal("no SQL issued")
	}
	sql := fq.queried[0]
	if strings.Contains(sql, "request") {
		t.Fatalf("SQL still references missing 'request' column: %s", sql)
	}
	if !strings.Contains(sql, "count(*)") {
		t.Fatalf("SQL = %q, want count(*) rewrite", sql)
	}
}

func TestTopComputedTagGroupByUsesBareExpression(t *testing.T) {
	// Regression: is_internet_0 is a computed tag without a physical ID
	// column. The GROUP BY key used to be the backticked grouped expression
	// (`any(if(...))`), which ClickHouse reads as a column name and fails.
	req := clickhouse.QuerierRequest{
		Database:  "flow_log",
		Table:     "l7_flow_log",
		TimeStart: 1000,
		TimeEnd:   2000,
		Queries: []clickhouse.QuerierSub{{
			Select: "count(*) AS cnt",
			Tags:   []string{"is_internet_0"},
		}},
	}
	fq := &fakeQuerier{}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	var groupSQL string
	for _, q := range fq.queried {
		if strings.Contains(q, "GROUP BY") && !strings.Contains(q, "GROUP BY toi") {
			groupSQL = q
		}
	}
	if groupSQL == "" {
		t.Fatal("no grouped query issued")
	}
	gbIdx := strings.Index(groupSQL, "GROUP BY ")
	if gbIdx < 0 {
		t.Fatal("no GROUP BY in SQL")
	}
	rest := groupSQL[gbIdx+len("GROUP BY "):]
	// The group key must be a bare expression — never a backticked
	// any(...) aggregate.
	if strings.HasPrefix(rest, "`") {
		t.Fatalf("GROUP BY key is backticked, ClickHouse would treat it as a column: %s", groupSQL)
	}
	if strings.Contains(rest, "`any(") {
		t.Fatalf("GROUP BY contains backticked aggregate: %s", groupSQL)
	}
	if !strings.Contains(rest, "isIPAddressInRange") {
		t.Fatalf("GROUP BY key = %q, want is_internet expression", rest)
	}
	// The SELECT side keeps the grouped (any()) form for display.
	if !strings.Contains(groupSQL, "any(if(is_ipv4") {
		t.Fatalf("SELECT side missing any(...) display form: %s", groupSQL)
	}
}

func TestTopGroupByDictGetTagUsesIDColumnOrBareExpr(t *testing.T) {
	// auto_service_0 resolves to dictGet(...); grouping must not backtick
	// the whole expression either.
	req := clickhouse.QuerierRequest{
		Database:  "flow_log",
		Table:     "l7_flow_log",
		TimeStart: 1000,
		TimeEnd:   2000,
		Queries: []clickhouse.QuerierSub{{
			Select: "count(*) AS cnt",
			Tags:   []string{"auto_service_0"},
		}},
	}
	fq := &fakeQuerier{}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	var groupSQL string
	for _, q := range fq.queried {
		if strings.Contains(q, "GROUP BY") && !strings.Contains(q, "GROUP BY toi") {
			groupSQL = q
		}
	}
	if strings.Contains(groupSQL, "`any(") || strings.Contains(groupSQL, "`dictGet") {
		t.Fatalf("GROUP BY contains backticked expression: %s", groupSQL)
	}
}

func TestTopFlowMetricsGranularityFallback(t *testing.T) {
	// Regression: the frontend asks DATA_SOURCE=1h but the environment only
	// retains application_map.1d — the query must fall back to the real table
	// instead of failing every column check and dropping to cache.
	req := clickhouse.QuerierRequest{
		Database:   "flow_metrics",
		Table:      "application_map",
		DataSource: "1h",
		TimeStart:  1000,
		TimeEnd:    2000,
		Queries: []clickhouse.QuerierSub{{
			Select: "count(*) AS cnt",
			Tags:   []string{"is_internet_0"},
		}},
	}
	fq := &fakeQuerier{resolve: map[string]string{"application_map": "application_map.1d"}}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	if len(fq.queried) == 0 {
		t.Fatal("no SQL issued")
	}
	found := false
	for _, q := range fq.queried {
		if strings.Contains(q, "application_map.1d") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no query used the resolved granularity table; SQL: %v", fq.queried)
	}
	if strings.Contains(fq.queried[0], "application_map.1h`") {
		t.Fatalf("query still references the missing 1h table: %s", fq.queried[0])
	}
}

func TestTopPercentileFieldRewrite(t *testing.T) {
	// Regression: Percentile(`rrt`,0.95) used to build quantile(0.95)(`rrt`)
	// — the 'rrt' column doesn't exist in flow_metrics (it's stored as
	// rrt_sum/rrt_count) so the query failed and fell back to cache.
	req := clickhouse.QuerierRequest{
		Database:   "flow_metrics",
		Table:      "application",
		DataSource: "1h",
		TimeStart:  1000,
		TimeEnd:    2000,
		Queries: []clickhouse.QuerierSub{{
			Select: "Avg(`rrt`) AS `响应时延`, Percentile(`rrt`,0.95) AS `P95 响应时延`",
		}},
	}
	fq := &fakeQuerier{}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	if len(fq.queried) == 0 {
		t.Fatal("no SQL issued")
	}
	all := strings.Join(fq.queried, "\n")
	if strings.Contains(all, "quantile(0.95)(`rrt`)") {
		t.Fatalf("SQL still references bare rrt column: %s", all)
	}
	if !strings.Contains(all, "quantile(0.95)(rrt_sum / greatest(rrt_count, 1))") {
		t.Fatalf("SQL missing rrt_sum rewrite: %s", all)
	}
}

func TestTopPercentileFlowLogUsesResponseDuration(t *testing.T) {
	req := clickhouse.QuerierRequest{
		Database:  "flow_log",
		Table:     "l7_flow_log",
		TimeStart: 1000,
		TimeEnd:   2000,
		Queries: []clickhouse.QuerierSub{{
			Select: "Percentile(`rtt`,0.5) AS p50",
		}},
	}
	fq := &fakeQuerier{}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	if len(fq.queried) == 0 {
		t.Fatal("no SQL issued")
	}
	if !strings.Contains(strings.Join(fq.queried, "\n"), "quantile(0.5)(`response_duration`)") {
		t.Fatalf("SQL missing response_duration rewrite: %v", fq.queried)
	}
}

func TestTopFlowMetricsInternetSideTag(t *testing.T) {
	// Regression: flow_metrics has no is_internet_0 physical column — it must
	// resolve to the CIDR expression. SELECT uses any(...), GROUP BY the bare
	// expression, otherwise ClickHouse fails on non-grouped is_ipv4/ip4_0.
	req := clickhouse.QuerierRequest{
		Database:   "flow_metrics",
		Table:      "application_map",
		DataSource: "1h",
		TimeStart:  1000,
		TimeEnd:    2000,
		Queries: []clickhouse.QuerierSub{{
			Select: "count(*) AS cnt",
			Tags:   []string{"is_internet_0", "auto_service_0"},
		}},
	}
	fq := &fakeQuerier{}
	if _, err := QueryTop(fq, context.Background(), req); err != nil {
		t.Fatalf("QueryTop error: %v", err)
	}
	var groupSQL string
	for _, q := range fq.queried {
		if strings.Contains(q, "GROUP BY") && !strings.Contains(q, "GROUP BY toi") {
			groupSQL = q
		}
	}
	if groupSQL == "" {
		t.Fatal("no grouped SQL")
	}
	// SELECT side: any(...) form.
	if !strings.Contains(groupSQL, "any(if(is_ipv4") {
		t.Fatalf("SELECT side missing any() form: %s", groupSQL)
	}
	// GROUP BY side: bare expression, never backticked or any()-wrapped.
	gbIdx := strings.Index(groupSQL, "GROUP BY ")
	rest := groupSQL[gbIdx+len("GROUP BY "):]
	if strings.Contains(rest, "any(if(is_ipv4") || strings.HasPrefix(rest, "`") {
		t.Fatalf("GROUP BY key must be the bare expression: %s", groupSQL)
	}
	if !strings.Contains(rest, "isIPAddressInRange") {
		t.Fatalf("GROUP BY key missing CIDR expression: %s", groupSQL)
	}
	// auto_service_0 resolves per-side dictGet with IP fallback for type 0/255.
	if !strings.Contains(groupSQL, "IPv4NumToString(any(ip4_0))") {
		t.Fatalf("auto_service_0 missing IP fallback: %s", groupSQL)
	}
}
