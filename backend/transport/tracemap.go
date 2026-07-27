package transport

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"deeptrace-backend/clickhouse"
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
			result := &query.TraceMapResult{
				NodeData: []map[string]interface{}{},
				ProgressInfo: map[string]interface{}{
					"total_traces_count":      0,
					"calculated_traces_count": 0,
				},
			}
			writeTraceMap(w, result)
			return
		}

		// Query ClickHouse directly (bypass chain; only source is CH).
		var result *clickhouse.QueryTraceMapResult
		if srv.CH != nil && srv.CH.Enabled() {
			result, err = srv.CH.QueryTraceMap(r.Context(), tReq.TimeStart, tReq.TimeEnd, tReq.QueryCondition)
			if err != nil {
				log.Printf("⚠️  TraceMap CH error: %v", err)
			}
		}

		if result == nil || len(result.Data) == 0 {
			writeTraceMap(w, &query.TraceMapResult{
				NodeData: []map[string]interface{}{},
				ProgressInfo: map[string]interface{}{
					"total_traces_count":      0,
					"calculated_traces_count": 0,
				},
			})
			return
		}

		// Add region and progress info.
		for _, node := range result.Data {
			if _, ok := node["_querier_region"]; !ok {
				node["_querier_region"] = "本地"
			}
		}

		writeTraceMap(w, &query.TraceMapResult{
			NodeData: result.Data,
			ProgressInfo: map[string]interface{}{
				"total_traces_count":      result.TotalTraces,
				"calculated_traces_count": result.CalculatedTraces,
			},
		})
	}
}
