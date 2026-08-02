package transport

import (
	"net/http"
	"strings"

	"deeptrace-backend/query/biz"
)

// RegisterBiz adds the df-web biz (business) API routes.
func RegisterBiz(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/df-web/v1/biz", handleBizList(deps))
	mux.HandleFunc("/api/df-web/v1/biz/", handleBizRoutes(deps))
	mux.HandleFunc("/api/df-web/v1/biz_groups", handleBizGroups)
}

// handleBizGroups serves GET /api/df-web/v1/biz_groups (business groups).
func handleBizGroups(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, biz.QueryBizGroups())
}

func handleBizList(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Real data first: the business list is derived from actual
		// ClickHouse statistics (service/path counts).
		rows := biz.QueryBizList(deps.CH, r.Context())
		writeSuccess(w, rows)
	}
}

// handleBizRoutes dispatches /api/df-web/v1/biz/... sub-routes.
// Business logic lives in query/biz — this handler only parses the path and
// writes the response.
func handleBizRoutes(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// /api/df-web/v1/biz/{biz_id}/biz_entry_path
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// ["api","df-web","v1","biz","<biz_id>","biz_entry_path"]
		if len(parts) == 6 && parts[3] == "biz" && parts[5] == "biz_entry_path" {
			// Real data first (M4 semantics) — the query reads ClickHouse
			// service pairs; no cache involvement for this endpoint.
			rows := biz.QueryEntryPaths(deps.CH, r.Context(), parts[4])
			writeSuccess(w, rows)
			return
		}
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		writeSuccess(w, map[string]interface{}{"ID": 1, "NAME": "默认业务", "JSON_CONFIG": "{}"})
	}
}
