package transport

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// RegisterSQLQuery registers the raw SQL query endpoint.
func RegisterSQLQuery(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/querier/v1/query/", handleSQLQuery(deps))
}

func handleSQLQuery(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]string
		json.Unmarshal(body, &req)

		db := req["db"]
		sql := strings.ToLower(req["sql"])
		log.Printf("📊 QUERY db=%s sql=%s", db, sql[:min(len(sql), 80)])

		// Route to different data files based on SQL content.
		var dataFile string
		switch {
		case strings.Contains(sql, "l7_flow_log") || strings.Contains(sql, "trace"):
			dataFile = "traces.json"
		case strings.Contains(sql, "flow_metrics") || strings.Contains(sql, "vtap_app_port"):
			dataFile = "metrics.json"
		case strings.Contains(sql, "event"):
			dataFile = "events.json"
		default:
			dataFile = "query_default.json"
		}

		data, err := deps.Aggregator.ReadDataFileJSON(dataFile)
		if err != nil {
			writeSuccess(w, []interface{}{})
			return
		}
		writeSuccess(w, data)
	}
}
