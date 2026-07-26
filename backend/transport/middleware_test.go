package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"deeptrace-backend/query"
)

func TestVerificationControlsRejectsForcedSourceWhenDisabled(t *testing.T) {
	handler := VerificationControls(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("wrapped handler should not run")
	}), false)
	request := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	request.Header.Set(forceSourceHeader, "zerotrace")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestVerificationControlsAddsStrictSourcePolicy(t *testing.T) {
	handler := VerificationControls(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		policy := query.SourcePolicyFromContext(request.Context())
		if policy.ForcedSource != "clickhouse" {
			t.Fatalf("forced source = %q, want clickhouse", policy.ForcedSource)
		}
		if !policy.Strict {
			t.Fatal("strict policy = false, want true")
		}
		writer.WriteHeader(http.StatusNoContent)
	}), true)
	request := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	request.Header.Set(forceSourceHeader, "ch")
	request.Header.Set(noFallbackHeader, "true")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get(requestedSourceHeader); got != "clickhouse" {
		t.Fatalf("requested source header = %q, want clickhouse", got)
	}
}

func TestWriteResultEmitsActualSourceHeader(t *testing.T) {
	response := httptest.NewRecorder()

	writeResult(response, &query.Result{
		Data:   []map[string]interface{}{},
		Source: "zerotrace",
	})

	if got := response.Header().Get(actualSourceHeader); got != "zerotrace" {
		t.Fatalf("actual source header = %q, want zerotrace", got)
	}
}
