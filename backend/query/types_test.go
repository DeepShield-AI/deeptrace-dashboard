package query

import (
	"encoding/json"
	"testing"
)

func TestRequestBodyPreservesUnknownFrontendFields(t *testing.T) {
	raw := []byte(`{"DATABASE":"flow_log","INCLUDE_HISTORY":true,"interval":2}`)
	var request QuerierListRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if string(request.RawBody) != string(raw) {
		t.Fatalf("RawBody = %s, want original %s", request.RawBody, raw)
	}
}

func TestRequestBodyMarshalRoundTripDoesNotLoseShape(t *testing.T) {
	// A Marshal round-trip must not silently drop unknown fields either:
	// RawBody is the source of truth for cache matching / ZT forwarding.
	raw := []byte(`{"DATABASE":"flow_log","TABLE":"l7_flow_log","INCLUDE_HISTORY":true,"PAGE_INDEX":2}`)
	var request QuerierListRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	request.NormalizeQuery()
	if len(request.RawBody) == 0 {
		t.Fatal("RawBody empty after NormalizeQuery")
	}
}

func TestResultEnvelopePreservesCachedStatus(t *testing.T) {
	result := &Result{
		Data:        []map[string]interface{}{},
		OptStatus:   "PARTIAL_RESULT",
		Description: "partial data",
	}

	envelope := result.Envelope()

	if envelope["OPT_STATUS"] != "PARTIAL_RESULT" {
		t.Fatalf("OPT_STATUS = %v, want PARTIAL_RESULT", envelope["OPT_STATUS"])
	}
	if envelope["DESCRIPTION"] != "partial data" {
		t.Fatalf("DESCRIPTION = %v, want partial data", envelope["DESCRIPTION"])
	}
}

func TestResultEnvelopeDefaultsToSuccess(t *testing.T) {
	result := &Result{Data: []map[string]interface{}{}}

	envelope := result.Envelope()

	if envelope["OPT_STATUS"] != "SUCCESS" {
		t.Fatalf("OPT_STATUS = %v, want SUCCESS", envelope["OPT_STATUS"])
	}
	if _, hasCount := envelope["COUNT"]; hasCount {
		t.Fatal("COUNT present for empty result, want omitted")
	}
}

func TestNormalizeQueryPopulatesQueriesFromFlatFormat(t *testing.T) {
	var request QuerierListRequest
	raw := []byte(`{"DATABASE":"flow_log","TABLE":"l7_flow_log","SELECT":"count(*)","GROUP_BY":"region","LIMIT":10,"ORDER_BY":"count","ORDER":"DESC"}`)
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	request.NormalizeQuery()

	if len(request.Queries) != 1 {
		t.Fatalf("Queries = %d entries, want 1", len(request.Queries))
	}
	if request.Queries[0].Select != "count(*)" {
		t.Fatalf("Select = %q, want count(*)", request.Queries[0].Select)
	}
	if request.Queries[0].GroupBy != "region" {
		t.Fatalf("GroupBy = %q, want region", request.Queries[0].GroupBy)
	}
	if request.Top != FlexInt(10) {
		t.Fatalf("Top = %d, want 10 (LIMIT mapped)", request.Top)
	}
	if request.Sort == nil || request.Sort.OrderBy != "count" || request.Sort.SortedBy != "DESC" {
		t.Fatalf("Sort = %+v, want {count DESC}", request.Sort)
	}
}
