package transport

import (
	"io"
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"
)

// RegisterFallback adds the catch-all /api/ handler (for unregistered API paths).
func RegisterFallback(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/", handleAPIFallback(deps))
}

func handleAPIFallback(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var bodyStr string
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			body, _ := io.ReadAll(r.Body)
			bodyStr = string(body)
		}

		// Under a forced no-fallback policy the cache is not an acceptable
		// answer — the request reached the catch-all untouched, so fail.
		if policy := query.SourcePolicyFromContext(r.Context()); policy.NoFallback {
			writeSourceError(w, r, "no data source served the query")
			return
		}

		// Cache first.
		if deps.Cache != nil {
			if cached := deps.Cache.FindWithBody(r.Method, r.URL.RequestURI(), bodyStr); cached != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Write(cached)
				return
			}
		}

		logging.Warnf("UNHANDLED %s %s", r.Method, r.URL.Path)
		writeSuccess(w, []interface{}{})
	}
}
