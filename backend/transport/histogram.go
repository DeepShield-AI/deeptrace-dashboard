package transport

import (
	"io"
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"
)

// RegisterHistogram registers the Histogram endpoint.
func RegisterHistogram(mux *http.ServeMux, srv *query.QuerierService) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/Histogram", handleHistogram(srv))
}

func handleHistogram(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body")
			return
		}

		// Verification protocol (M4/M5): Histogram is only served by ClickHouse.
		policy := query.SourcePolicyFromContext(r.Context())
		if policy.ForcedSource != "" && policy.ForcedSource != "clickhouse" {
			writeSourceError(w, r, "histogram is only served by clickhouse")
			return
		}
		if policy.NoFallback && (srv.CH == nil || !srv.CH.Enabled()) {
			writeSourceError(w, r, "clickhouse not available")
			return
		}

		result, err := srv.QueryHistogram(r.Context(), string(body))
		if err != nil {
			logging.Errorf("Histogram error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		w.Header().Set(sourceHeader, "clickhouse")
		writeResult(w, result)
	}
}
