package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RegisterShowTagValues registers the ShowTagValues endpoint.
func RegisterShowTagValues(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/ShowTagValues", handleShowTagValues(deps))
}

// svRequest is the request body for ShowTagValues.
type svRequest struct {
	Database string `json:"DATABASE"`
	Table    string `json:"TABLE"`
	Tag      string `json:"TAG"`
	Like     string `json:"LIKE"`
	Offset   int    `json:"OFFSET"`
	Limit    *int   `json:"LIMIT"`
}

func handleShowTagValues(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		var req svRequest
		if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		if req.Database == "" {
			req.Database = "flow_log"
		}
		if req.Table == "" {
			req.Table = "l7_flow_log"
		}

		// 1. Try ClickHouse direct query (system.columns.comment → flow_tag → DISTINCT).
		if data := chQueryShowTagValues(req); data != nil {
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS":  "SUCCESS",
				"DESCRIPTION": "",
				"DATA":        data,
				"TYPE":        "DBDescription",
				"SCHEMAS":     map[string]interface{}{},
			})
			return
		}

		// 3. Fallback: empty.
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS":  "SUCCESS",
			"DESCRIPTION": "",
			"DATA":        []interface{}{},
			"TYPE":        "DBDescription",
			"SCHEMAS":     map[string]interface{}{},
		})
	}
}

// ---------------------------------------------------------------------------
// ClickHouse direct query for ShowTagValues
// ---------------------------------------------------------------------------

// chQueryShowTagValues queries ClickHouse for tag values.
// Returns nil if CH is unreachable or error.
