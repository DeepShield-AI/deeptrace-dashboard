package transport

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"deeptrace-backend/query/dimension"
)


// RegisterDimensionResources adds the dimension-resources endpoint.
func RegisterDimensionResources(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/dimension-resources", handleDimensionResources(deps))
}

func handleDimensionResources(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}
		var req dimension.DimReq
		if err := json.Unmarshal(body, &req); err != nil {
			log.Printf("⚠️  dimension-resources unmarshal error: %v", err)
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS": "SUCCESS", "TYPE": "dict",
				"DATA": dimension.EmptyDimensionResult(),
			})
			return
		}

		data := dimension.QueryDimensionResources(deps.CH, req)
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS": "SUCCESS", "TYPE": "dict",
			"DATA": data,
		})
	}
}

// dimension.QueryDimensionResources queries ClickHouse for resource dimension data
// related to the specified service filter.
