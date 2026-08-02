package transport

import (
	"encoding/json"
	"io"
	"net/http"

	"deeptrace-backend/client"
	"deeptrace-backend/logging"
)

// RegisterSQLQuery registers the raw SQL query endpoint.
func RegisterSQLQuery(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/querier/v1/query/", handleSQLQuery(deps))
}

// handleSQLQuery proxies native SQL/SHOW queries to deepflow-server.
// Response contract (from api_cache, e.g. 055114024695.json):
//
//	{"OPT_STATUS":"SUCCESS","DESCRIPTION":"","result":{"columns":[...],"schemas":[...],"values":[...]},"debug":null}
func handleSQLQuery(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]string
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "invalid request body: "+err.Error())
			return
		}

		db := req["db"]
		sql := req["sql"]
		logging.Debugf("QUERY db=%s sql=%s", db, sql[:min(len(sql), 80)])

		var zt *client.ZerotraceService
		if deps.Querier != nil {
			zt = deps.Querier.Zerotrace
		}
		if zt == nil || !zt.Available() {
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS":  "FAIL",
				"DESCRIPTION": "zerotrace-server not configured",
				"result":      nil,
				"debug":       nil,
			})
			return
		}

		res, err := zt.QueryRaw(db, sql)
		if err != nil {
			logging.Errorf("QUERY zerotrace failed: %v", err)
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS":  "FAIL",
				"DESCRIPTION": err.Error(),
				"result":      nil,
				"debug":       nil,
			})
			return
		}

		writeJSON(w, map[string]interface{}{
			"OPT_STATUS":  res.OptStatus,
			"DESCRIPTION": "",
			"result": map[string]interface{}{
				"columns": res.Columns,
				"schemas": res.Schemas,
				"values":  res.Values,
			},
			"debug": nil,
		})
	}
}
