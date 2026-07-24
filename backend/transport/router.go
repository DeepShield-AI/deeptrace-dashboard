package transport

import (
	"net/http"

	"deeptrace-backend/cache"
	"deeptrace-backend/client"
	"deeptrace-backend/clickhouse"
	"deeptrace-backend/aggregator"
	"deeptrace-backend/query"
)

// Dependencies holds all optional services for transport handlers.
// Each field may be nil if the service is not available.
type Dependencies struct {
	Cache      *cache.Cache
	CH         *clickhouse.CHService
	Aggregator *aggregator.Aggregator
	Algorithms *client.AlgorithmsService
	Querier    *query.QuerierService
	StaticDir  string
}

// RegisterAll registers all API and static routes with the mux.
// Must be the only route registration call (replaces the previous
// per-package Register* pattern).
func RegisterAll(mux *http.ServeMux, deps *Dependencies) {
	RegisterAuth(mux)
	RegisterDashboard(mux, deps)
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
	RegisterTracing(mux, deps.Algorithms)

	// Fallback for unhandled /api/ paths.
	RegisterFallback(mux, deps)

	// Static file serving (must be last, catches "/").
	RegisterStatic(mux, deps.StaticDir)
}
