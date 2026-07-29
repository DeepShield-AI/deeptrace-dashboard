package transport

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"deeptrace-backend/clickhouse"
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
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		result, err := flowlog.QueryList(srv.Zerotrace, srv.Enum, string(body))
		if err != nil {
			log.Printf("⚠️  FlowLogDetail error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}

// handleFlowLogDetailInfo handles FlowLogDetailInfo requests.
// Uses flowlog.QueryInfo which is completely independent from QueryList.
// Response does NOT include COUNT (confirmed from real API).
func handleFlowLogDetailInfo(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		result, err := flowlog.QueryInfo(srv.Zerotrace, string(body))
		if err != nil {
			log.Printf("⚠️  FlowLogDetailInfo error: %v", err)
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS":  "SUCCESS",
				"DATA":        []interface{}{},
				"TYPE":        "Flow_Log_Detail_Info",
				"DESCRIPTION": "",
			})
			return
		}
		// Build envelope manually — FlowLogDetailInfo omits COUNT.
		env := map[string]interface{}{
			"OPT_STATUS":  "SUCCESS",
			"DATA":        result.Data,
			"TYPE":        result.Type,
			"DESCRIPTION": "",
		}
		if result.Fields != nil {
			env["SCHEMAS"] = result.Fields
		}
		writeJSON(w, env)
	}
}

func handleShowAttributes(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if srv.CH == nil || !srv.CH.Enabled() {
			writeSuccess(w, []interface{}{})
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req struct {
			Database string `json:"DATABASE"`
			Table    string `json:"TABLE"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeSuccess(w, []interface{}{})
			return
		}
		sql := clickhouse.BuildShowAttributesSQL(req.Database, req.Table)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		rows, err := srv.CH.Query(ctx, sql)
		if err != nil {
			writeSuccess(w, []interface{}{})
			return
		}
		defer rows.Close()
		data, err := clickhouse.ScanRows(rows)
		if err != nil {
			writeSuccess(w, []interface{}{})
			return
		}
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS": "SUCCESS", "DATA": data, "COUNT": len(data), "DESCRIPTION": "",
		})
	}
}
