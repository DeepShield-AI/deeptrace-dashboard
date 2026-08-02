package profile

import (
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"deeptrace-backend/query"
)

func TestBuildSQLFilters(t *testing.T) {
	cases := []struct {
		name string
		req  *query.ProfileRequest
		want string
	}{
		{
			name: "time range only",
			req:  &query.ProfileRequest{TimeStart: 1000, TimeEnd: 2000},
			want: "SELECT profile_location_str, profile_value FROM `profile`.`in_process` WHERE time >= 1000 AND time <= 2000",
		},
		{
			name: "fixed filters",
			req: &query.ProfileRequest{
				TimeStart: 1000, TimeEnd: 2000,
				AppService:          "cart",
				ProfileLanguageType: "eBPF",
				ProfileEventType:    "on-cpu",
			},
			want: "SELECT profile_location_str, profile_value FROM `profile`.`in_process` WHERE time >= 1000 AND time <= 2000 AND profile_language_type = 'eBPF' AND profile_event_type = 'on-cpu' AND app_service = 'cart'",
		},
		{
			name: "string value escaped",
			req:  &query.ProfileRequest{TimeStart: 1, TimeEnd: 2, AppService: "a'b"},
			want: "SELECT profile_location_str, profile_value FROM `profile`.`in_process` WHERE time >= 1 AND time <= 2 AND app_service = 'a\\'b'",
		},
		{
			name: "tag_filter numeric and string",
			req:  &query.ProfileRequest{TimeStart: 1, TimeEnd: 2, TagFilter: "gprocess_id=10308, pod=web-1"},
			want: "SELECT profile_location_str, profile_value FROM `profile`.`in_process` WHERE time >= 1 AND time <= 2 AND `gprocess_id` = 10308 AND `pod` = 'web-1'",
		},
		{
			name: "malformed tag_filter tolerated",
			req:  &query.ProfileRequest{TimeStart: 1, TimeEnd: 2, TagFilter: "garbage"},
			want: "SELECT profile_location_str, profile_value FROM `profile`.`in_process` WHERE time >= 1 AND time <= 2",
		},
		{
			name: "frontend quoted tag_filter",
			req:  &query.ProfileRequest{TimeStart: 1, TimeEnd: 2, TagFilter: "`gprocess_id`=10308, `pod`='web-1'"},
			want: "SELECT profile_location_str, profile_value FROM `profile`.`in_process` WHERE time >= 1 AND time <= 2 AND `gprocess_id` = 10308 AND `pod` = 'web-1'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildSQL(tc.req)
			if err != nil {
				t.Fatalf("buildSQL error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("buildSQL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildSQLRejectsBadTagFilterKey(t *testing.T) {
	_, err := buildSQL(&query.ProfileRequest{
		TimeStart: 1, TimeEnd: 2,
		TagFilter: "gprocess_id=1; DROP TABLE x",
	})
	if err == nil {
		t.Fatal("expected error for invalid tag_filter key")
	}
	if !strings.Contains(err.Error(), "unsupported column") {
		t.Fatalf("error = %v, want ErrUnsupportedColumn", err)
	}
}

func TestFrameType(t *testing.T) {
	cases := []struct {
		frame  string
		isRoot bool
		want   string
	}{
		{"[p] cart", false, "P"},
		{"cart", true, "P"},  // bare root frame → P
		{"cart", false, "A"}, // bare non-root → A (e.g. __vdso_clock_gettime)
		{"[t] .NET TP Worker", false, "T"},
		{"[k] __schedule", false, "K"},
		{"[l] sched_yield", false, "L"},
		{"[/usr/src/app/cart]", false, "?"},
		{"[/proc/32417/fd/8]", true, "?"}, // path frames stay ? even at root
		{"__vdso_clock_gettime", false, "A"},
	}
	for _, tc := range cases {
		if got := frameType(tc.frame, tc.isRoot); got != tc.want {
			t.Fatalf("frameType(%q, %v) = %q, want %q", tc.frame, tc.isRoot, got, tc.want)
		}
	}
}

// toF and toInt normalize node row cells, which are int for the id/parent
// cells and float64 for the value cells (or float64 after a JSON round-trip).
func toF(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return -2
}

// zstdRow encodes a stack text with zstd and wraps it in a scanned row shape.
func zstdRow(t *testing.T, stack string, v float64) map[string]interface{} {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	compressed := enc.EncodeAll([]byte(stack), nil)
	return map[string]interface{}{
		"profile_location_str": string(compressed),
		"profile_value":        v,
	}
}

// TestBuildResultInvariants builds a small tree and checks the contract
// invariants: total == self + Σ(children) per node, Σ(self) == root total,
// function_values == Σ over nodes of the function.
func TestBuildResultInvariants(t *testing.T) {
	data := []map[string]interface{}{
		zstdRow(t, "[p] cart;[t] worker;[k] __schedule", 10),
		zstdRow(t, "[p] cart;[t] worker;[k] __schedule", 5),
		zstdRow(t, "[p] cart;[t] worker;[k] try_to_wake_up", 7),
		zstdRow(t, "[p] cart;[t] worker", 3), // stack truncates at thread
		zstdRow(t, "[p] cart;[t] gc", 2),
		zstdRow(t, "[p] cart", 1), // truncated at process
	}
	res := BuildResult(data)

	if len(res.Functions) != 5 {
		t.Fatalf("functions = %v, want 5 (cart, worker, __schedule, try_to_wake_up, gc)", res.Functions)
	}
	types := map[string]string{}
	for i, f := range res.Functions {
		types[f] = res.FunctionTypes[i]
	}
	if types["[p] cart"] != "P" || types["[t] worker"] != "T" || types["[k] __schedule"] != "K" {
		t.Fatalf("types = %v, want P/T/K for [p] cart/[t] worker/[k] __schedule", types)
	}

	nv := res.NodeValues.Values
	if len(nv) == 0 {
		t.Fatal("no nodes")
	}
	// parent_node_id must reference valid rows (or -1).
	fv := res.FunctionValues.Values
	if len(fv) != len(res.Functions) {
		t.Fatalf("function_values rows = %d, want %d", len(fv), len(res.Functions))
	}

	// Node invariants.
	childSum := make([]float64, len(nv))
	for _, r := range nv {
		par := toInt(r[1])
		if par != -1 {
			if par < 0 || par >= len(nv) {
				t.Fatalf("parent %d out of range", par)
			}
			childSum[par] += toF(r[3])
		}
	}
	var sumSelf float64
	for i, r := range nv {
		self, total := toF(r[2]), toF(r[3])
		if total != self+childSum[i] {
			t.Fatalf("node %d: total %v != self %v + children %v", i, total, self, childSum[i])
		}
		sumSelf += self
	}
	// Single root with parent -1 and total == Σ(self).
	var roots float64
	rootCount := 0
	for _, r := range nv {
		if toInt(r[1]) == -1 {
			rootCount++
			roots = toF(r[3])
		}
	}
	if rootCount != 1 {
		t.Fatalf("root count = %d, want 1", rootCount)
	}
	if roots != sumSelf {
		t.Fatalf("root total %v != Σ(self) %v", roots, sumSelf)
	}

	// function_values == Σ over nodes.
	funcSelf := make(map[int]float64)
	funcTotal := make(map[int]float64)
	for _, r := range nv {
		fid := toInt(r[0])
		funcSelf[fid] += toF(r[2])
		funcTotal[fid] += toF(r[3])
	}
	for i := range res.Functions {
		row := fv[i]
		if row[0].(float64) != funcSelf[i] || row[1].(float64) != funcTotal[i] {
			t.Fatalf("function %d: fval %v != node agg (%v, %v)", i, row, funcSelf[i], funcTotal[i])
		}
	}
}

func TestBuildResultEmptyAndMalformed(t *testing.T) {
	res := BuildResult(nil)
	if res == nil || len(res.Functions) != 0 || len(res.NodeValues.Values) != 0 {
		t.Fatalf("empty result = %+v, want empty ProfileResult", res)
	}

	// Malformed rows (not zstd, empty location) must be skipped, not fatal.
	data := []map[string]interface{}{
		{"profile_location_str": "", "profile_value": float64(1)},
		{"profile_location_str": "not-zstd", "profile_value": float64(2)},
		zstdRow(t, "[p] cart", 3),
	}
	res = BuildResult(data)
	if len(res.Functions) != 1 || res.Functions[0] != "[p] cart" {
		t.Fatalf("functions = %v, want [[p] cart]", res.Functions)
	}
	if res.NodeValues.Values[0][3].(float64) != 3 {
		t.Fatalf("root total = %v, want 3", res.NodeValues.Values[0][3])
	}
}

func TestBuildResultMultipleRoots(t *testing.T) {
	data := []map[string]interface{}{
		zstdRow(t, "[p] proc-a;[t] t1", 4),
		zstdRow(t, "[p] proc-b;[t] t2", 6),
	}
	res := BuildResult(data)
	rootCount := 0
	for _, r := range res.NodeValues.Values {
		if toInt(r[1]) == -1 {
			rootCount++
		}
	}
	if rootCount != 2 {
		t.Fatalf("root count = %d, want 2 (one per first frame)", rootCount)
	}
}
