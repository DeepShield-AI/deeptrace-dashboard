package clickhouse

import (
	"strings"
	"testing"
)

func TestTagExprProfileVirtualColumns(t *testing.T) {
	cases := []struct {
		tag  string
		want string // substring expected in the expression
	}{
		{"pod", "flow_tag.pod_map"},
		{"gprocess", "flow_tag.gprocess_map"},
		{"pod_node", "flow_tag.pod_node_map"},
		{"pod_cluster", "flow_tag.pod_cluster_map"},
		{"vpc", "flow_tag.l3_epc_map"},
		{"region", "flow_tag.region_map"},
	}
	for _, tc := range cases {
		expr := TagExpr("profile", "in_process", tc.tag)
		if expr == "" {
			t.Fatalf("TagExpr(profile, in_process, %q) = empty, want a dict expression", tc.tag)
		}
		if !strings.Contains(expr, tc.want) {
			t.Fatalf("TagExpr(%q) = %q, want it to reference %q", tc.tag, expr, tc.want)
		}
	}
	// Physical columns stay bare.
	if got := TagExpr("profile", "in_process", "app_service"); got != "" {
		t.Fatalf("TagExpr(app_service) = %q, want empty (bare column handled elsewhere)", got)
	}
}

func TestTagIDExprProfileGroupsOnPhysicalColumn(t *testing.T) {
	cases := map[string]string{
		"pod":         "pod_id",
		"pod_node":    "pod_node_id",
		"pod_cluster": "pod_cluster_id",
		"vpc":         "l3_epc_id",
		"gprocess":    "gprocess_id",
	}
	for tag, want := range cases {
		if got := TagIDExpr("profile", "in_process", tag); got != want {
			t.Fatalf("TagIDExpr(profile, %q) = %q, want %q", tag, got, want)
		}
	}
}
