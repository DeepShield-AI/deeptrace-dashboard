package transport

import (
	"net/http"

	"deeptrace-backend/query"
)

// RegisterQuerier registers all core querier List/Top/Profile endpoints.
func RegisterQuerier(mux *http.ServeMux, srv *query.QuerierService) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/List", handleList(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/Top", handleTop(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/Profile", handleProfile(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/Topo", handleTopo(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MergedMultiList", handleList(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MultiPromList", handleMultiPromList(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MultiTop", handleTop(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MultiHistogram", handleList(srv))

	// Resource list endpoints.
	for _, path := range []string{"ContainerList", "HostList", "ServiceList", "ResourceUsageList"} {
		mux.HandleFunc("/api/statistics/v1/stats/querier/"+path, handleList(srv))
	}

	// Alarm event history.
	mux.HandleFunc("/api/statistics/v1/stats/querier/AlarmEventHistory", handleList(srv))

	// Universal/generic endpoints.
	mux.HandleFunc("/api/statistics/v1/stats/querier/UniversalTop", handleTop(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/UniversalHistory", handleUniversalHistory(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/UniversalPromHistory", handleUniversalHistory(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/MultiUniversalPromHistory", handleUniversalHistory(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/UnsupportedTags", handleUnsupportedTags)
	mux.HandleFunc("/api/statistics/v1/stats/querier/L", handleList(srv))
	mux.HandleFunc("/api/statistics/v1/stats/querier/Npb", handleList(srv))
}

// --------------------------------------------------------------------------
// List
// --------------------------------------------------------------------------


