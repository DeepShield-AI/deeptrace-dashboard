package transport

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"deeptrace-backend/query"
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
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}

		// Parse cloud-format request: {time_start, time_end, query_condition}
		var tReq traceMapRequest
		if err := json.Unmarshal(body, &tReq); err != nil {
			writeTraceMap(w, emptyTraceMapResult())
			return
		}

		// Path 1: Try ClickHouse directly (fast path, best format).
		if srv.CH != nil && srv.CH.Enabled() {
			chResult, chErr := srv.CH.QueryTraceMap(r.Context(), tReq.TimeStart, tReq.TimeEnd, tReq.QueryCondition)
			if chErr != nil {
				log.Printf("⚠️  TraceMap CH error: %v", chErr)
			}
			if chResult != nil && len(chResult.Data) > 0 {
				writeTraceMap(w, &query.TraceMapResult{
					NodeData: chResult.Data,
					ProgressInfo: map[string]interface{}{
						"total_traces_count":      chResult.TotalTraces,
						"calculated_traces_count": chResult.CalculatedTraces,
					},
				})
				return
			}
		}

		// Path 2: Try DataSourceChain (cache → mock fallback).
		if srv.Chain != nil {
			chainReq := &query.QuerierListRequest{
				TimeStart: tReq.TimeStart,
				TimeEnd:   tReq.TimeEnd,
			}
			chainResult, chainErr := srv.Chain.QueryTraceMap(r.Context(), chainReq)
			if chainErr == nil && chainResult != nil && len(chainResult.NodeData) > 0 {
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
