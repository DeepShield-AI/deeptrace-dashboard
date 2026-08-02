package transport

import (
	"net/http"
)

// RegisterDashboard adds dashboard and biz API routes.
func RegisterDashboard(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/df-web/v1/dashboards", handleDashboards(deps))
}

func handleDashboards(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		writeSuccess(w, []interface{}{})
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
