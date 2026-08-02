package transport

import (
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query/duration_detail"
)

// RegisterDurationDetail registers the duration_detail endpoint.
func RegisterDurationDetail(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DurationDetail", handleDurationDetail(deps))
}

func handleDurationDetail(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[duration_detail.Request](r)
		if err != nil {
			writeError(w, decodeErrorMessage(err))
			return
		}
		req := *parsed
		if req.Limit <= 0 {
			req.Limit = 20
		}
		if req.TimeStart == 0 || req.TimeEnd == 0 {
			writeSuccess(w, map[string]interface{}{"result": []interface{}{}})
			return
		}
		logging.Debugf("duration_detail: time=%d-%d limit=%d", req.TimeStart, req.TimeEnd, req.Limit)

		rows := duration_detail.Query(deps.CH, r.Context(), &req, "flow_log", "l7_flow_log")
		writeSuccess(w, map[string]interface{}{"result": rows})
	}
}
