package transport

import (
	"net/http"

	"deeptrace-backend/query/region"
	"deeptrace-backend/query/resource"
)

func RegisterResource(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/deepflow-server/v2/regions", handleRegions(deps))
	mux.HandleFunc("/api/deepflow-server/v1/regions", handleRegions(deps))
	mux.HandleFunc("/api/deepflow-server/v1/data-sources/", handleDataSources(deps))
	mux.HandleFunc("/api/deepflow-server/v1/data_sources/", handleDataSources(deps))
	mux.HandleFunc("/api/deepflow-server/", handleResourceFallback)
}

// ---------------------------------------------------------------------------
// Regions
// ---------------------------------------------------------------------------

// handleRegions serves the region list from the RegionService (loaded from
// deepflow-server's region dictionary), falling back to hardcoded data when
// the service is unavailable.
func handleRegions(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Region != nil {
			if regions := deps.Region.Regions(); regions != nil {
				writeSuccess(w, regions)
				return
			}
		}
		writeSuccess(w, region.FallbackRegions())
	}
}

// ---------------------------------------------------------------------------
// Data Sources
// ---------------------------------------------------------------------------

func handleDataSources(_ *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, resource.QueryDataSources())
	}
}

// ---------------------------------------------------------------------------
// Fallback for other deepflow-server resource paths
// ---------------------------------------------------------------------------

func handleResourceFallback(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, []interface{}{})
}
