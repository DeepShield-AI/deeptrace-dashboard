package transport

import (
	"context"
	"net/http"
	"strings"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"
)

// Verification protocol headers (see tools/replay_from_cache.py).
const (
	sourceHeader      = "X-DeepTrace-Source"
	forceSourceHeader = "X-DeepTrace-Force-Source"
	noFallbackHeader  = "X-DeepTrace-No-Fallback"
)

// SourceControlMiddleware attaches a source policy from the verification
// request headers. Only active when verify is true (VERIFY_SOURCE_CONTROL=1,
// local verification only) — otherwise the headers are ignored so public
// deployments can't select internal data sources.
func SourceControlMiddleware(verify bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if verify {
				if forced := r.Header.Get(forceSourceHeader); forced != "" && query.IsKnownSource(forced) {
					policy := query.SourcePolicy{
						ForcedSource: forced,
						NoFallback:   r.Header.Get(noFallbackHeader) == "true",
					}
					logging.Infof("verify: forced source %q (no-fallback=%v)", policy.ForcedSource, policy.NoFallback)
					r = r.WithContext(query.WithSourcePolicy(r.Context(), policy))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// queryWithProvenance runs a chain query with provenance tracking: it records
// which source served the request and sets the X-DeepTrace-Source header, and
// turns chain errors into HTTP 502 under a forced no-fallback policy.
// Returns (result, true) on success — (nil, false) after an error was written.
func queryWithProvenance[T any](w http.ResponseWriter, r *http.Request, run func(ctx context.Context) (*T, error)) (*T, bool) {
	prov := &query.Provenance{}
	ctx := query.WithProvenance(r.Context(), prov)
	result, err := run(ctx)
	if err != nil {
		logging.Errorf("chain query error: %v", err)
		writeSourceError(w, r, "query failed: "+err.Error())
		return nil, false
	}
	if prov.Source != "" {
		w.Header().Set(sourceHeader, prov.Source)
	}
	return result, true
}

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
				logging.Warnf("%d %s %s", rec.status, r.Method, r.URL.Path)
			}
		}()

		// Log all API requests and SPA routes.
		if strings.HasPrefix(r.URL.Path, "/api/") || !strings.Contains(r.URL.Path, ".") {
			logging.Infof("Request %s %s%s", r.Method, r.URL.Path, func() string {
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
		w.Header().Set("Access-Control-Expose-Headers", "X-Org-Id, Crypto, X-Short-Refresh-Token-Key, X-DeepTrace-Source")

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
