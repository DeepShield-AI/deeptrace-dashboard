package duration_detail

import (
	"testing"
)

func TestQueryID(t *testing.T) {
	cases := []struct {
		name string
		req  *Request
		want string
	}{
		{"no where", &Request{}, ""},
		{"single pair", &Request{Where: &struct {
			Paths        []map[string]string `json:"paths"`
			ResourceSets []struct {
				ID        string        `json:"id"`
				Condition []interface{} `json:"condition"`
				GroupBy   []string      `json:"groupBy"`
			} `json:"resourceSets"`
		}{Paths: []map[string]string{{"client": "R1", "server": "R2"}}}}, "R1-R2"},
		{"two pairs", &Request{Where: &struct {
			Paths        []map[string]string `json:"paths"`
			ResourceSets []struct {
				ID        string        `json:"id"`
				Condition []interface{} `json:"condition"`
				GroupBy   []string      `json:"groupBy"`
			} `json:"resourceSets"`
		}{Paths: []map[string]string{{"client": "R1", "server": "R2"}, {"client": "R3", "server": "R4"}}}}, "R1-R2,R3-R4"},
	}
	for _, tc := range cases {
		if got := queryID(tc.req); got != tc.want {
			t.Fatalf("queryID(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDecorateRows(t *testing.T) {
	rows := []map[string]interface{}{
		{
			"auto_service_type_0": float64(11), // pod_service
			"auto_service_type_1": float64(0),  // internet_ip
			"observation_point":   "c-p",
		},
	}
	decorateRows(rows, "R1-R2", "本地")

	r := rows[0]
	if r["query_id"] != "R1-R2" {
		t.Fatalf("query_id = %v, want R1-R2", r["query_id"])
	}
	if r["client_node_type"] != "pod_service" || r["client_icon_id"] != float64(-16) {
		t.Fatalf("client = %v/%v, want pod_service/-16", r["client_node_type"], r["client_icon_id"])
	}
	if r["server_node_type"] != "internet_ip" || r["server_icon_id"] != float64(-1) {
		t.Fatalf("server = %v/%v, want internet_ip/-1", r["server_node_type"], r["server_icon_id"])
	}
	if r["_querier_region"] != "本地" {
		t.Fatalf("region = %v, want 本地", r["_querier_region"])
	}
}

func TestDecorateRowsMissingTypeStaysUntouched(t *testing.T) {
	rows := []map[string]interface{}{{"observation_point": "s-p"}}
	decorateRows(rows, "", "")
	if _, ok := rows[0]["client_node_type"]; ok {
		t.Fatal("client_node_type must not be added without auto_service_type_0")
	}
}
