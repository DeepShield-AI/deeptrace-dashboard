package transport

import (
	"encoding/json"
	"net/http"

	"deeptrace-backend/query"
)

// writeJSON writes an arbitrary JSON response.
func writeJSON(w http.ResponseWriter, v interface{}) {
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

// writeError writes a JSON error response with the given status code.
func writeError(w http.ResponseWriter, desc string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
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
			row["_querier_region"] = "本地"
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
