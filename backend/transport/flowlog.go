package transport

import (
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"
	"deeptrace-backend/query/flowlog"
)

// RegisterFlowLog registers all FlowLogDetail-related endpoints.
func RegisterFlowLog(mux *http.ServeMux, srv *query.QuerierService) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/FlowLogDetailList", handleFlowLogDetail(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/FlowLogTimingDetailList", handleFlowLogDetail(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/FlowLogDetailInfo", handleFlowLogDetailInfo(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/FlowLogDetailHistory", handleFlowLogDetail(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/FlowLogDetailSearch", handleFlowLogDetail(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/FlowLogAsyncDetail", handleFlowLogDetail(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/FlowLogTimingDetailHistory", handleFlowLogDetail(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/TracingDetailList", handleFlowLogDetail(srv))

	// FlowMap and ShowAttributes.
	mux.HandleFunc("/api/statistics/v1/stats/querier/FlowMap", handleFlowLogDetail(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/ShowAttributes", handleShowAttributes(srv))
}

// handleFlowLogDetail handles all FlowLogDetailList-type endpoints.
// Uses flowlog.QueryList which bypasses DataSourceChain and goes direct to zerotrace.
func handleFlowLogDetail(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readRawBody(r)
		if err != nil {
			writeError(w, "cannot read body")
			return
		}
		result, err := flowlog.QueryList(srv.Zerotrace, srv.Enum, string(body))
		if err != nil {
			logging.Errorf("FlowLogDetail error: %v", err)
			if query.SourcePolicyFromContext(r.Context()).NoFallback {
				writeSourceError(w, r, "flow log detail query failed: "+err.Error())
				return
			}
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		w.Header().Set(sourceHeader, "zerotrace")
		writeResult(w, result)
	}
}

// handleFlowLogDetailInfo handles FlowLogDetailInfo requests.
// Uses flowlog.QueryInfo which is completely independent from QueryList.
// Response does NOT include COUNT (confirmed from real API).
func handleFlowLogDetailInfo(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readRawBody(r)
		if err != nil {
			writeError(w, "cannot read body")
			return
		}
		result, err := flowlog.QueryInfo(srv.Zerotrace, string(body))
		if err != nil {
			logging.Errorf("FlowLogDetailInfo error: %v", err)
			if query.SourcePolicyFromContext(r.Context()).NoFallback {
				writeSourceError(w, r, "flow log detail info query failed: "+err.Error())
				return
			}
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS":  "SUCCESS",
				"DATA":        []interface{}{},
				"TYPE":        "Flow_Log_Detail_Info",
				"DESCRIPTION": "",
			})
			return
		}
		w.Header().Set(sourceHeader, "zerotrace")
		// FlowLogDetailInfo omits COUNT (confirmed from real API).
		writeJSON(w, query.FlowLogDetailInfoResponse{
			Data:   result.Data,
			Type:   result.Type,
			Fields: result.Fields,
		})
	}
}

func handleShowAttributes(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type showAttributesRequest struct {
			Database string `json:"DATABASE"`
			Table    string `json:"TABLE"`
		}
		parsed, _, err := parseBody[showAttributesRequest](r)
		if err != nil {
			writeSuccess(w, []interface{}{})
			return
		}
		req := *parsed
		result, err := flowlog.QueryShowAttributes(srv.CH, req.Database, req.Table)
		if err != nil || result == nil {
			writeSuccess(w, []interface{}{})
			return
		}
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS": "SUCCESS", "DATA": result.Data, "COUNT": result.Count, "DESCRIPTION": "",
		})
	}
}
