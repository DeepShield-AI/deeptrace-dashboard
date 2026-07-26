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
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		writeSuccess(w, []interface{}{})
	}
}

func handleBiz(deps *Dependencies) http.HandlerFunc {
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
		writeSuccess(w, map[string]interface{}{"ID": 1, "NAME": "默认仪表盘", "JSON_CONFIG": "{}"})
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
