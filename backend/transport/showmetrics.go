package transport

import (
	"deeptrace-backend/query"
	"deeptrace-backend/query/showmetrics"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// RegisterShowMetrics registers the ShowMetrics endpoint.
func RegisterShowMetrics(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/ShowMetrics", handleShowMetrics(deps))
}

func handleShowMetrics(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		// Parse request.
		var req struct {
			Database string `json:"DATABASE"`
			Table    string `json:"TABLE"`
		}
		db, tbl := "flow_log", "l7_flow_log"
		if json.Unmarshal([]byte(bodyStr), &req) == nil {
			if req.Database != "" {
				db = req.Database
			}
			if req.Table != "" {
				tbl = req.Table
			}
		}

	// ShowMetrics writes helper: include TYPE field.
	writeShowMetrics := func(w http.ResponseWriter, data interface{}) {
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS":  "SUCCESS",
			"DESCRIPTION": "",
			"DATA":        data,
			"TYPE":        "DBDescription",
			"SCHEMAS":     map[string]interface{}{},
		})
	}

		// Collect metrics from multiple sources and merge for best coverage.
		var mergedMetrics []interface{}
		seen := map[string]bool{}

		// 1. Try deepflow-server (ZT) — returns virtual tags and metrics.
		if deps.Querier != nil && deps.Querier.Zerotrace != nil {
			result, err := query.QueryShowMetrics(deps.Querier.Zerotrace, bodyStr)
			if err == nil && result != nil {
				for _, m := range result.Data {
				key := fmt.Sprintf("%s.%s.%s", db, tbl, m["name"])
				if !seen[key] {
					seen[key] = true
					mergedMetrics = append(mergedMetrics, m)
				}
			}
		}
		}

		// 2. Try ClickHouse HTTP for column metadata (fills gaps ZT misses).
		if chMetrics := showmetrics.QueryShowMetricsCH(db, tbl); chMetrics != nil {
			for _, m := range chMetrics {
				if mm, ok := m.(map[string]interface{}); ok {
					key := fmt.Sprintf("%s.%s.%s", db, tbl, mm["name"])
					if !seen[key] {
						seen[key] = true
						mergedMetrics = append(mergedMetrics, mm)
					}
				}
			}
		}

		if len(mergedMetrics) > 0 {
			writeShowMetrics(w, mergedMetrics)
			return
		}

		// 3. Fallback: hardcoded minimal list.
		writeShowMetrics(w, showmetrics.FallbackShowMetrics(tbl))
	}
}

// ---------------------------------------------------------------------------
// ClickHouse HTTP query (port 8123)
// ---------------------------------------------------------------------------
