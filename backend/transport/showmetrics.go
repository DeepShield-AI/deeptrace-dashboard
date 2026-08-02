package transport

import (
	"io"
	"net/http"

	"deeptrace-backend/client"
	"deeptrace-backend/query"
	"deeptrace-backend/query/showmetrics"
)

// RegisterShowMetrics registers the ShowMetrics endpoint.
func RegisterShowMetrics(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/ShowMetrics", handleShowMetrics(deps))
}

func handleShowMetrics(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var zt *client.ZerotraceService
		if deps.Querier != nil {
			zt = deps.Querier.Zerotrace
		}

		// Verification protocol (M4/M5): ShowMetrics is served by ZT (primary)
		// merged with ClickHouse column metadata.
		policy := query.SourcePolicyFromContext(r.Context())
		if policy.ForcedSource != "" &&
			policy.ForcedSource != "zerotrace" && policy.ForcedSource != "clickhouse" {
			writeSourceError(w, r, "show metrics is only served by zerotrace/clickhouse")
			return
		}
		if policy.NoFallback && (zt == nil || !zt.Available()) {
			writeSourceError(w, r, "no data source served show metrics")
			return
		}

		metrics := showmetrics.QueryMetricsMerged(zt, string(body), r.Header.Get("X-Language"))
		w.Header().Set(sourceHeader, "zerotrace")

		writeJSON(w, query.DBDescriptionResponse{Data: metrics})
	}
}
