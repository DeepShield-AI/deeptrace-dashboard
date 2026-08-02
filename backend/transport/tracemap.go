package transport

import (
	"errors"
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"
	"deeptrace-backend/query/tracemap"
)

// RegisterTraceMap registers the TraceMap endpoint.
func RegisterTraceMap(mux *http.ServeMux, srv *query.QuerierService) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/TraceMap", handleTraceMap(srv))
}

// traceMapRequest mirrors the cloud TraceMap request body.
type traceMapRequest struct {
	TimeStart      int64  `json:"time_start"`
	TimeEnd        int64  `json:"time_end"`
	QueryCondition string `json:"query_condition"`
}

func handleTraceMap(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse cloud-format request: {time_start, time_end, query_condition}
		parsed, _, err := parseBody[traceMapRequest](r)
		if err != nil {
			if errors.Is(err, ErrBodyRead) {
				writeError(w, "cannot read body")
				return
			}
			writeTraceMap(w, emptyTraceMapResult())
			return
		}
		tReq := *parsed

		policy := query.SourcePolicyFromContext(r.Context())
		chOK := policy.ForcedSource == "" || query.NormalizeSourceName("clickhouse") == policy.ForcedSource

		// Path 1: Try ClickHouse directly (fast path, best format).
		if chOK && srv.CH != nil && srv.CH.Enabled() {
			chResult, chErr := tracemap.QueryTraceMap(srv.CH, r.Context(), tReq.TimeStart, tReq.TimeEnd, tReq.QueryCondition)
			if chErr != nil {
				logging.Errorf("TraceMap CH error: %v", chErr)
			}
			if chResult != nil {
				// CH handled the query (empty or not) — stop.
				w.Header().Set(sourceHeader, "clickhouse")
				writeTraceMap(w, &query.TraceMapResult{
					NodeData: chResult.Data,
					ProgressInfo: map[string]interface{}{
						"total_traces_count":      chResult.TotalTraces,
						"calculated_traces_count": chResult.CalculatedTraces,
					},
				})
				return
			}
			if policy.NoFallback {
				writeSourceError(w, r, "clickhouse trace map query failed")
				return
			}
		}

		// Path 2: Try DataSourceChain.
		if srv.Chain != nil {
			prov := &query.Provenance{}
			ctx := query.WithProvenance(r.Context(), prov)
			chainReq := &query.QuerierListRequest{
				TimeStart: tReq.TimeStart,
				TimeEnd:   tReq.TimeEnd,
			}
			chainResult, chainErr := srv.Chain.QueryTraceMap(ctx, chainReq)
			if chainErr != nil {
				logging.Errorf("TraceMap chain error: %v", chainErr)
				if policy.NoFallback {
					writeSourceError(w, r, "trace map query failed: "+chainErr.Error())
					return
				}
			}
			if chainResult != nil {
				if prov.Source != "" {
					w.Header().Set(sourceHeader, prov.Source)
				}
				writeTraceMap(w, chainResult)
				return
			}
		}

		writeTraceMap(w, emptyTraceMapResult())
	}
}

func emptyTraceMapResult() *query.TraceMapResult {
	return &query.TraceMapResult{
		NodeData: []map[string]interface{}{},
		ProgressInfo: map[string]interface{}{
			"total_traces_count":      0,
			"calculated_traces_count": 0,
		},
	}
}
