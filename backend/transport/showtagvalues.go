package transport

import (
	"net/http"

	"deeptrace-backend/query"
	"deeptrace-backend/query/showtagvalues"
)

// RegisterShowTagValues registers the ShowTagValues endpoint.
func RegisterShowTagValues(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/ShowTagValues", handleShowTagValues(deps))
}

// svRequest is the request body for ShowTagValues.
func handleShowTagValues(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[showtagvalues.SvRequest](r)
		if err != nil {
			writeError(w, decodeErrorMessage(err))
			return
		}
		req := *parsed
		if req.Database == "" {
			req.Database = "flow_log"
		}
		if req.Table == "" {
			req.Table = "l7_flow_log"
		}

		// 1. Try ClickHouse direct query (system.columns.comment → flow_tag → DISTINCT).
		if data := showtagvalues.ChQueryShowTagValues(req); data != nil {
			// Verify protocol: ShowTagValues is only served by ClickHouse.
			w.Header().Set(sourceHeader, "clickhouse")
			writeJSON(w, query.DBDescriptionResponse{Data: data})
			return
		}

		// Verification protocol: forced no-fallback must fail instead of
		// silently returning empty (M4/M5).
		policy := query.SourcePolicyFromContext(r.Context())
		if policy.NoFallback ||
			(policy.ForcedSource != "" && policy.ForcedSource != "clickhouse") {
			writeSourceError(w, r, "no data source served show tag values")
			return
		}

		// 3. Fallback: empty.
		writeJSON(w, query.DBDescriptionResponse{Data: []interface{}{}})
	}
}
