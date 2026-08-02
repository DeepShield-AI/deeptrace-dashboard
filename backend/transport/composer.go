package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"deeptrace-backend/client"
	"deeptrace-backend/logging"
	"deeptrace-backend/query"
	"deeptrace-backend/query/duration_detail"
	"deeptrace-backend/query/fastlist"
)

// RegisterComposer registers the Composer panel endpoints.
// NOTE: the real frontend prefix is "/api/df-web-composer/" (no slash between
// df-web and composer) — verified against api_cache filenames.
//   - /api/df-web-composer/api/querier/fast_list/...  (fast list, verified in api_cache)
//   - /api/df-web-composer/api/service_topo/...       (service topology, verified in api_cache)
//   - /api/df-web-composer/api/l7_flowlog_analysis/... (duration/timeout detail analysis)
//   - /api/df-web-composer/...                        (fallback)
func RegisterComposer(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/df-web-composer/api/querier/fast_list/", handleFastList(deps))
	mux.HandleFunc("/api/df-web-composer/api/service_topo/", handleServiceTopo)
	mux.HandleFunc("/api/df-web-composer/api/l7_flowlog_analysis/", handleL7FlowlogAnalysis(deps))
	mux.HandleFunc("/api/df-web-composer/", handleComposerFallback(deps))
}

// handleL7FlowlogAnalysis serves the l7_flowlog_analysis endpoints
// (duration_detail_with_max/<agg>, timeout_detail_with_max) used by the
// resource-analysis drawer. The request body is the same shape as
// duration_detail.Request (where.resourceSets condition tree + groupBy).
func handleL7FlowlogAnalysis(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var req duration_detail.Request
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, decodeErrorMessage(err))
			return
		}
		if req.TimeStart == 0 || req.TimeEnd == 0 {
			writeSuccess(w, map[string]interface{}{"result": []interface{}{}})
			return
		}
		if req.Limit <= 0 {
			req.Limit = 20
		}

		// Real data first (M4 semantics), cache as last-resort fallback.
		logging.Debugf("l7_flowlog_analysis %s: time=%d-%d limit=%d", r.URL.Path, req.TimeStart, req.TimeEnd, req.Limit)
		rows := duration_detail.Query(deps.CH, r.Context(), &req, "flow_log", "l7_flow_log")
		if len(rows) > 0 {
			writeSuccess(w, map[string]interface{}{"result": rows})
			return
		}
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		writeSuccess(w, map[string]interface{}{"result": rows})
	}
}

func handleFastList(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		debug := r.URL.Query().Get("debug") == "1"

		var zt *client.ZerotraceService
		if deps.Querier != nil {
			zt = deps.Querier.Zerotrace
		}
		data, di := fastlist.QueryFastList(deps.CH, zt, r.URL.Path, body, debug)
		if data == nil && di == nil {
			writeSuccess(w, []interface{}{})
			return
		}
		var dbg interface{}
		if debug && di != nil {
			dbg = fastlist.BuildFastListDebug(body, di)
		}
		writeJSON(w, query.FastListResponse{Data: data, Debug: dbg})
	}
}

func handleServiceTopo(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "alert_event") {
		writeSuccess(w, map[string]interface{}{
			"alertLevelCount": map[string]int{}, "alertTrend": []interface{}{},
			"alertActiveLevelTrend": []interface{}{}, "alertActiveLevelIntervals": []interface{}{},
		})
		return
	}
	writeSuccess(w, map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}})
}

func handleComposerFallback(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		logging.Warnf("Composer fallback %s %s", r.Method, r.URL.Path)
		writeSuccess(w, []interface{}{})
	}
}
