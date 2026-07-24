package transport

import (
	"net/http"
	"strings"
)

// RegisterDashboard adds dashboard and biz API routes.
func RegisterDashboard(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/df-web/v1/dashboards", handleDashboards(deps))
	mux.HandleFunc("/api/df-web/v1/biz", handleBiz(deps))
	mux.HandleFunc("/api/df-web/v1/biz/", handleBiz(deps))
}

func handleDashboards(deps *Dependencies) http.HandlerFunc {
	agg := deps.Aggregator
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		data, err := agg.ReadDataFileJSON("dashboards.json")
		if err != nil {
			writeSuccess(w, []interface{}{})
			return
		}
		writeSuccess(w, data)
	}
}

func handleBiz(deps *Dependencies) http.HandlerFunc {
	agg := deps.Aggregator
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSuffix(r.URL.Path, "/") == "/api/df-web/v1/biz" {
			if checkCache(w, deps, r.Method, "/api/df-web/v1/biz") {
				return
			}
			writeSuccess(w, []interface{}{})
			return
		}

		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}

		// Extract UUID from path: /api/df-web/v1/biz/{uuid}
		parts := strings.Split(r.URL.Path, "/")
		uuid := ""
		for i, p := range parts {
			if p == "biz" && i+1 < len(parts) {
				uuid = parts[i+1]
				break
			}
		}

		filename := "dashboard_" + uuid + ".json"
		data, err := agg.ReadDataFileJSON(filename)
		if err != nil {
			data, err = agg.ReadDataFileJSON("dashboard_default.json")
			if err != nil {
				writeSuccess(w, map[string]interface{}{
					"ID": 1, "NAME": "默认仪表盘", "LCUUID": uuid,
					"JSON_CONFIG": "{}",
				})
				return
			}
		}
		writeSuccess(w, data)
	}
}

// checkCache looks up a cached response and writes it if found.
// Returns true if the response was served from cache.
func checkCache(w http.ResponseWriter, deps *Dependencies, method, path string) bool {
	if deps.Cache != nil {
		if cached := deps.Cache.Find(method, path); cached != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(cached)
			return true
		}
	}
	return false
}
