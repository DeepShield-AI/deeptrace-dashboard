package transport

import (
	"encoding/json"
	"net/http"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/query"
)

// writeJSON writes an arbitrary JSON response. Values implementing
// query.Envelope are wrapped through Envelope() first, so envelope types
// (Result, DBDescriptionResponse, FastListResponse, ...) serialize in the
// DeepFlow wire format automatically.
func writeJSON(w http.ResponseWriter, v interface{}) {
	if env, ok := v.(query.Envelope); ok {
		v = env.Envelope()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// writeSuccess writes a simple success response with OPT_STATUS/DATA/DESCRIPTION.
func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DATA":        data,
		"DESCRIPTION": "",
	})
}

// writeError writes a 400 JSON error response.
func writeError(w http.ResponseWriter, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]interface{}{
		"OPT_STATUS":  "FAIL",
		"DATA":        nil,
		"DESCRIPTION": desc,
	})
}

// writeSourceError writes an HTTP 502 for forced-source verification failures
// (M4: source unavailable/unsupported under a forced no-fallback policy).
func writeSourceError(w http.ResponseWriter, r *http.Request, desc string) {
	if policy := query.SourcePolicyFromContext(r.Context()); policy.ForcedSource != "" {
		w.Header().Set(sourceHeader, policy.ForcedSource)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	writeJSON(w, map[string]interface{}{
		"OPT_STATUS":  "FAIL",
		"DATA":        nil,
		"DESCRIPTION": desc,
	})
}

// writeResult writes a *query.Result using its Envelope() method.
func writeResult(w http.ResponseWriter, r *query.Result) {
	if r == nil {
		r = &query.Result{Data: []map[string]interface{}{}, OptStatus: "SUCCESS"}
	}
	for _, row := range r.Data {
		if _, ok := row["_querier_region"]; !ok {
			row["_querier_region"] = clickhouse.QuerierRegion
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if r.OptStatus == "PARTIAL_RESULT" {
		w.WriteHeader(http.StatusPartialContent)
	}
	json.NewEncoder(w).Encode(r.Envelope())
}

// writeTraceMap is a convenience wrapper for TraceMapResult responses.
func writeTraceMap(w http.ResponseWriter, r *query.TraceMapResult) {
	writeJSON(w, r.Envelope())
}
