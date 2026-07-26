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
		log.Printf("📊 QUERY db=%s sql=%s", db, sql[:min(len(sql), 80)])
		writeSuccess(w, []interface{}{})
	}
}
