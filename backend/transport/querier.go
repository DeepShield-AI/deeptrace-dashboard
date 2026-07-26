package transport

import (
	"encoding/json"
	"io"
	"log"
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

func handleList(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		result, err := srv.QueryList(r.Context(), &req)
		if err != nil {
			log.Printf("⚠️  QueryList error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}

// --------------------------------------------------------------------------
// Top
// --------------------------------------------------------------------------

func handleTop(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return
		}
		result, err := srv.QueryTop(r.Context(), &req)
		if err != nil {
			log.Printf("⚠️  QueryTop error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}

// --------------------------------------------------------------------------
// Profile
// --------------------------------------------------------------------------

func handleProfile(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		result, err := srv.QueryTopForProfile(r.Context(), &req)
		if err != nil {
			log.Printf("⚠️  Profile error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}


func handleTopo(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{
			"instance_data": []interface{}{}, "peers_data": []interface{}{},
		})
	}
}

func handleUniversalHistory(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req query.QuerierListRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		result, err := srv.QueryTop(r.Context(), &req)
		if err != nil {
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		writeResult(w, result)
	}
}

func handleUnsupportedTags(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, []interface{}{})
}
