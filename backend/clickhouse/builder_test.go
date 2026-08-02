package clickhouse

import (
	"strings"
	"testing"
)

// buildRequest builds a QuerierRequest with one sub-query for SQL tests.
func buildRequest(sel, where, groupBy string) QuerierRequest {
	return QuerierRequest{
		Database:   "flow_metrics",
		Table:      "application",
		DataSource: "1m",
		TimeStart:  1785583684,
		TimeEnd:    1785584584,
		Queries: []QuerierSub{{
			QueryID: "R1",
			Select:  sel,
			Where:   where,
			GroupBy: groupBy,
		}},
	}
}

func TestBuildSelectSQL(t *testing.T) {
	tests := []struct {
		name      string
		req       QuerierRequest
		wantTable string
		wantParts []string // substrings the SQL must contain
		notParts  []string // substrings the SQL must NOT contain
	}{
		{
			name: "flow_metrics resolved table with data source",
			req: buildRequest(
				"newTag('R1') as query_id, PerSecond(Avg(`request`)) AS `请求速率`, chost_id, chost, vpc_id, vpc",
				"exist(chost_id)",
				"",
			),
			wantTable: "`flow_metrics`.`application.1m`",
			wantParts: []string{
				"`l3_device_id` != 0",                    // exist(chost_id) → ID predicate
				"dictGetOrDefault('flow_tag.chost_map'",  // chost name resolution
				"dictGetOrDefault('flow_tag.l3_epc_map'", // vpc name resolution
				"time >= 1785583684",                     // Unix integer timestamps
				"time <= 1785584584",
			},
			notParts: []string{"$any(", "`epc_id`", "time >= '"},
		},
		{
			name: "flow_log local table suffix",
			req: QuerierRequest{
				Database: "flow_log",
				Table:    "l7_flow_log",
				Queries: []QuerierSub{{
					QueryID: "R1",
					Select:  "newTag('R1') as query_id, Count(`row`) AS `count`, auto_service_0, is_internet_0",
					Where:   "exist(chost_id)",
				}},
			},
			wantTable: "`flow_log`.`l7_flow_log_local`",
			wantParts: []string{
				"count(*)",                    // Count(`row`) → count(*)
				"dictGetOrDefault('flow_tag.", // auto_service_0 name resolution
				"`l3_device_id_0` != 0 OR `l3_device_id_1` != 0", // exist(chost_id) both sides
			},
			notParts: []string{"$any(", "1=1"},
		},
		{
			name: "group by with virtual tags maps to ID columns",
			req: buildRequest(
				"PerSecond(Avg(`request`)) AS `请求速率`, auto_service, role, Enum(role)",
				"",
				"",
			),
			wantTable: "`flow_metrics`.`application.1m`",
			wantParts: []string{
				"app_service", // application auto_service → string column
				"GROUP BY",    // grouped query
			},
			notParts: []string{"auto_service_id AS `auto_service`"}, // app_service must be used
		},
		{
			name: "order by from sort clause",
			req: func() QuerierRequest {
				r := buildRequest("Avg(`request`) AS `请求速率`, auto_service", "", "")
				r.Sort = &QuerierSort{OrderBy: "请求速率", SortedBy: "DESC"}
				return r
			}(),
			wantTable: "`flow_metrics`.`application.1m`",
			wantParts: []string{
				"ORDER BY `请求速率` DESC",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, err := BuildSelectSQL(tt.req, nil)
			if err != nil {
				t.Fatalf("BuildSelectSQL error: %v", err)
			}
			if !strings.Contains(sql, tt.wantTable) {
				t.Errorf("missing table %q in SQL: %s", tt.wantTable, sql)
			}
			for _, part := range tt.wantParts {
				if !strings.Contains(sql, part) {
					t.Errorf("missing %q in SQL: %s", part, sql)
				}
			}
			for _, part := range tt.notParts {
				if strings.Contains(sql, part) {
					t.Errorf("unexpected %q in SQL: %s", part, sql)
				}
			}
		})
	}
}

func TestBuildSelectSQLUnsupportedColumn(t *testing.T) {
	// A tag with no physical column (pod_service_id does not exist in the
	// local application.1m schema) must surface ErrUnsupportedColumn.
	req := buildRequest("chost_id, pod_service_id", "", "")
	checker := &fakeColumnChecker{cols: map[string]bool{
		"l3_device_id": true,
	}}
	if _, err := BuildSelectSQL(req, checker); err == nil || !strings.Contains(err.Error(), ErrUnsupportedColumn.Error()) {
		t.Fatalf("expected ErrUnsupportedColumn, got %v", err)
	}
}

// fakeColumnChecker reports only the columns it knows.
type fakeColumnChecker struct {
	cols map[string]bool
}

func (c *fakeColumnChecker) HasColumn(db, table, col string) bool {
	return c.cols[col]
}

func TestValidateTableName(t *testing.T) {
	valid := []string{"l7_flow_log", "l7_flow_log_local", "flow_metrics.1m", "application_map", "tbl-2"}
	for _, name := range valid {
		if err := ValidateTableName(name); err != nil {
			t.Errorf("ValidateTableName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"a`b", "a;b", "a b", "a'b", "a\\b", "a\nb", "a\"b", "..", "a/b"}
	for _, name := range invalid {
		if err := ValidateTableName(name); err == nil {
			t.Errorf("ValidateTableName(%q) = nil, want error (injection)", name)
		}
	}
	if err := ValidateTableName(""); err != nil {
		t.Errorf("ValidateTableName(\"\") = %v, want nil (empty passes, callers default)", err)
	}
}

func TestBuildSelectSQLRejectsInjectedTable(t *testing.T) {
	req := QuerierRequest{
		Database: "flow_log",
		Table:    "l7_flow_log`; DROP TABLE x; --",
		Queries:  []QuerierSub{{Select: "count(*)"}},
	}
	_, err := BuildSelectSQL(req, nil)
	if err == nil {
		t.Fatal("BuildSelectSQL with injected table name: want error, got nil")
	}
}

func TestNodeTypeForMatchesOfficialEnum(t *testing.T) {
	// Verified against DeepFlow trident.proto AutoServiceType +
	// zerotrace-server tagrecorder RESOURCE_TYPE_TO_NODE_TYPE + api_cache.
	cases := map[int]string{
		0: "internet_ip", 1: "chost", 10: "pod", 11: "pod_service",
		12: "redis", 13: "rds", 14: "pod_node", 15: "lb", 16: "natgw",
		101: "pod_group", 102: "service", 103: "pod_cluster",
		104: "biz_service", 120: "gprocess",
		130: "pod_group", 133: "pod_group", 255: "ip",
	}
	for typ, want := range cases {
		if got := NodeTypeFor(typ); got != want {
			t.Errorf("NodeTypeFor(%d) = %q, want %q", typ, got, want)
		}
	}
}

func TestIconForMatchesApiCache(t *testing.T) {
	// Verified against api_cache TraceMap node_data:
	// 1→-23, 11→-16, 103→-13, 104→-45, 130→-18, 255→-10.
	cases := map[int]float64{1: -23, 11: -16, 103: -13, 104: -45, 130: -18, 255: -10}
	for typ, want := range cases {
		if got := IconFor(typ); got != want {
			t.Errorf("IconFor(%d) = %v, want %v", typ, got, want)
		}
	}
}

func TestBuildSelectSQLFlowLogCapitalizedRequestRewrites(t *testing.T) {
	// List path: Sum(request) must not reference the missing column.
	req := QuerierRequest{
		Database: "flow_log",
		Table:    "l7_flow_log",
		Queries:  []QuerierSub{{Select: "Sum(request) AS 请求总量"}},
	}
	sql, err := BuildSelectSQL(req, nil)
	if err != nil {
		t.Fatalf("BuildSelectSQL error: %v", err)
	}
	if strings.Contains(sql, "request") {
		t.Fatalf("SQL still references missing 'request' column: %s", sql)
	}
	if !strings.Contains(sql, "count(*)") {
		t.Fatalf("SQL = %q, want count(*) rewrite", sql)
	}
}

func TestBuildSelectSQLOrderByAlias(t *testing.T) {
	// Frontend convention (api_cache 7446e06d6b77): SORT.ORDER_BY equals a
	// SELECT alias — `Count(行数)` for Count(`row`) AS `Count(行数)`.
	req := QuerierRequest{
		Database: "flow_log",
		Table:    "l7_flow_log",
		Queries:  []QuerierSub{{Select: "Count(`row`) AS `Count(行数)`"}},
		Sort:     &QuerierSort{OrderBy: "Count(行数)", SortedBy: "DESC"},
	}
	sql, err := BuildSelectSQL(req, nil)
	if err != nil {
		t.Fatalf("BuildSelectSQL error: %v", err)
	}
	if !strings.Contains(sql, "ORDER BY `Count(行数)` DESC") {
		t.Fatalf("SQL = %q, want ORDER BY backticked alias", sql)
	}

	// Defensive: a quoted ORDER_BY must not produce `'alias'` column refs.
	req.Sort.OrderBy = "'Count(行数)'"
	sql2, err := BuildSelectSQL(req, nil)
	if err != nil {
		t.Fatalf("BuildSelectSQL error: %v", err)
	}
	if strings.Contains(sql2, "`'") || strings.Contains(sql2, "'`") {
		t.Fatalf("SQL = %q, stray quote leaked into ORDER BY", sql2)
	}
	if !strings.Contains(sql2, "ORDER BY `Count(行数)` DESC") {
		t.Fatalf("SQL = %q, want stripped quoted ORDER BY", sql2)
	}
}
