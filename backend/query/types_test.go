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
	request.RawBody = append(request.RawBody, raw...)

	body, err := request.RequestBody()

	if err != nil {
		t.Fatalf("RequestBody returned error: %v", err)
	}
	if string(body) != string(raw) {
		t.Fatalf("body = %s, want original %s", body, raw)
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
