package transport

import (
	"net/http"

	"deeptrace-backend/cache"
	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/query"
	"deeptrace-backend/query/region"
)

// Dependencies holds all optional services for transport handlers.
// Each field may be nil if the service is not available.
type Dependencies struct {
	Cache      *cache.Cache
	CH         *clickhouse.CHService
	Algorithms *client.AlgorithmsService
	Querier    *query.QuerierService
	Region     *region.Service
	StaticDir  string
}

// RegisterAll registers all API and static routes with the mux.
// Must be the only route registration call (replaces the previous
// per-package Register* pattern).
func RegisterAll(mux *http.ServeMux, deps *Dependencies) {
	RegisterAuth(mux)
	RegisterDashboard(mux, deps)
	RegisterBiz(mux, deps)
	RegisterResource(mux, deps)
	RegisterMisc(mux, deps)

	// Querier & related endpoints.
	srv := deps.Querier
	RegisterQuerier(mux, srv)
	RegisterFlowLog(mux, srv)
	RegisterTraceMap(mux, srv)
	RegisterHistogram(mux, srv)
	RegisterDBDescription(mux, deps)
	RegisterShowMetrics(mux, deps)
	RegisterShowTagValues(mux, deps)
	RegisterSQLQuery(mux, deps)

	// Composer endpoints (real frontend requests, verified in api_cache).
	RegisterComposer(mux, deps)
	RegisterTracing(mux, deps.Algorithms)
	RegisterDurationDetail(mux, deps)

	// Fallback for unhandled /api/ paths.
	RegisterDimensionResources(mux, deps)
	RegisterFallback(mux, deps)

	// Static file serving (must be last, catches "/").
	RegisterStatic(mux, deps.StaticDir)
}
