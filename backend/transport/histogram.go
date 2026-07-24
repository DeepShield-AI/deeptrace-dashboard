package transport

import (
	"io"
	"log"
	"net/http"

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
			writeError(w, "cannot read body", 400)
			return
		}
		result, err := srv.QueryHistogram(r.Context(), string(body))
		if err != nil {
			log.Printf("⚠️  Histogram error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}
