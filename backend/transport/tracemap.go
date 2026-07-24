package transport

import (
	"log"
	"net/http"

	"deeptrace-backend/query"
)

// RegisterTraceMap registers the TraceMap endpoint.
func RegisterTraceMap(mux *http.ServeMux, srv *query.QuerierService) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/TraceMap", handleTraceMap(srv))
}

func handleTraceMap(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := srv.QueryTraceMap(r.Context(), r.Body)
		if err != nil {
			log.Printf("⚠️  TraceMap error: %v", err)
			writeTraceMap(w, emptyTraceMapResult())
			return
		}
		writeTraceMap(w, result)
	}
}

func emptyTraceMapResult() *query.TraceMapResult {
	return &query.TraceMapResult{
		NodeData: []map[string]interface{}{},
		ProgressInfo: map[string]interface{}{
			"total_traces_count": 0, "calculated_traces_count": 0,
		},
	}
}
