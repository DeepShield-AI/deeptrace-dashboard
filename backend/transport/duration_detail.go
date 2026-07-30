package transport

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"deeptrace-backend/query/duration_detail"
)

func handleDurationDetail(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req duration_detail.Request
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		if req.Limit <= 0 { req.Limit = 20 }
		if req.TimeStart == 0 || req.TimeEnd == 0 {
			writeSuccess(w, map[string]interface{}{"result": []interface{}{}})
			return
		}
		log.Printf("📊 duration_detail: time=%d-%d limit=%d", req.TimeStart, req.TimeEnd, req.Limit)

		rows := duration_detail.Query(deps.CH, &req, "flow_log", "l7_flow_log")
		writeSuccess(w, map[string]interface{}{"result": rows})
	}
}
