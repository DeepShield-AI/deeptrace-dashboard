package transport

import (
	"log"
	"net/http"
	"strings"
)

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// CORS wraps a handler with CORS headers and request logging.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		w = rec

		defer func() {
			if rec.status >= 400 {
				log.Printf("⛔ %d %s %s", rec.status, r.Method, r.URL.Path)
			}
		}()

		// Log all API requests and SPA routes.
		if strings.HasPrefix(r.URL.Path, "/api/") || !strings.Contains(r.URL.Path, ".") {
			log.Printf("→ %s %s%s", r.Method, r.URL.Path, func() string {
				if r.URL.RawQuery != "" {
					return "?" + r.URL.RawQuery
				}
				return ""
			}())
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "X-Org-Id, Crypto, X-Short-Refresh-Token-Key")

		// Critical: frontend reads X-Org-Id header to determine current org.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("X-Org-Id", "4")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}

		next.ServeHTTP(w, r)
	})
}
