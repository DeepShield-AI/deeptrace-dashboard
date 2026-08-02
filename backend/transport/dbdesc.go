package transport

import (
	"io"
	"net/http"
	"strings"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/query"
	"deeptrace-backend/query/dbdesc"
)

// RegisterDBDescription registers the DBDescription (ShowDatabases/ShowTables/etc.) endpoint.
func RegisterDBDescription(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/ShowMetricsFunctions", handleShowMetricsFunctions)
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/", handleDBDescription(deps))
}

func handleShowMetricsFunctions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
		"DATA":        dbdesc.ShowMetricsFunctions(),
	})
}

func handleDBDescription(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		path := r.URL.Path

		// 1. Try cache first (skipped under a forced no-fallback policy).
		if deps.Cache != nil && !query.SourcePolicyFromContext(r.Context()).NoFallback {
			if cached := deps.Cache.FindWithBody(r.Method, r.URL.RequestURI(), bodyStr); cached != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Write(cached)
				return
			}
		}

		// 2. Try real-time DB description (ClickHouse for DBs/Tables,
		// deepflow-server for Tags).
		var zt *client.ZerotraceService
		if deps.Querier != nil {
			zt = deps.Querier.Zerotrace
		}
		if tryCHDBDescription(zt, deps.CH, w, path, bodyStr) {
			return
		}

		// Under a forced no-fallback policy a failed real query must 502
		// instead of silently serving hardcoded data (M4/M5).
		if query.SourcePolicyFromContext(r.Context()).NoFallback {
			writeSourceError(w, r, "no data source served db description")
			return
		}

		// 3. Fallback: hardcoded data.
		switch {
		case strings.Contains(path, "ShowDatabases"):
			writeSuccess(w, dbdesc.FallbackDatabases())
		case strings.Contains(path, "ShowTables"):
			writeSuccess(w, dbdesc.FallbackTables())
		case strings.Contains(path, "ShowTag"):
			writeSuccess(w, dbdesc.FallbackShowTag())
		default:
			writeSuccess(w, []interface{}{})
		}
	}
}

// tryCHDBDescription queries the query layer for DB description data.
// Returns true if a response was written.
func tryCHDBDescription(zt *client.ZerotraceService, ch *clickhouse.CHService, w http.ResponseWriter, path, bodyStr string) bool {
	data, ok := dbdesc.Query(zt, ch, path, bodyStr)
	if ok {
		// ShowTag goes to deepflow-server, DBs/Tables go to ClickHouse.
		if strings.Contains(path, "ShowTag") {
			w.Header().Set(sourceHeader, "zerotrace")
		} else {
			w.Header().Set(sourceHeader, "clickhouse")
		}
		writeSuccess(w, data)
	}
	return ok
}
