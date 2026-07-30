package transport

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"deeptrace-backend/query"
)


func handleList(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		req.NormalizeQuery()
		result, err := srv.QueryList(r.Context(), &req)
		if err != nil {
			log.Printf("⚠️  QueryList error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}


// --------------------------------------------------------------------------
// MultiPromList
// --------------------------------------------------------------------------

func handleMultiPromList(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req struct {
			TimeStart int64    `json:"time_start"`
			TimeEnd   int64    `json:"time_end"`
			PromQLs   []string `json:"PROMQLS"`
			Metrics   []string `json:"METRICS"`
			TOP       int      `json:"TOP"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			log.Printf("⚠️  MultiPromList unmarshal error: %v", err)
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS": "SUCCESS", "TYPE": "Multi_Prom_List", "DATA": []interface{}{},
			})
			return
		}
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS": "SUCCESS",
			"TYPE":       "Multi_Prom_List",
			"DATA":       []interface{}{},
		})
	}
}

// --------------------------------------------------------------------------
// Top
// --------------------------------------------------------------------------

