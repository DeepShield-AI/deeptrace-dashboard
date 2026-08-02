package transport

import (
	"errors"
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query/dimension"
)

// RegisterDimensionResources adds the dimension-resources endpoint.
func RegisterDimensionResources(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/dimension-resources", handleDimensionResources(deps))
}

func handleDimensionResources(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[dimension.DimReq](r)
		if err != nil {
			if errors.Is(err, ErrBodyRead) {
				writeError(w, "cannot read body")
				return
			}
			logging.Errorf("dimension-resources unmarshal error: %v", err)
			writeJSON(w, dimension.CloudResponse())
			return
		}
		req := *parsed

		// Cloud contract (verified against cloud.deepflow.yunshan.net): a
		// synchronous task envelope with DATA.ATTRIBUTES. IP conditions
		// resolve through flow_tag.ip_resource_map; service conditions return
		// the same empty ATTRIBUTES the cloud returns.
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS":    "SUCCESS",
			"WAIT_CALLBACK": false,
			"TASK":          nil,
			"DESCRIPTION":   "",
			"TYPE":          "dict",
			"DATA":          dimension.QueryDimensionResources(deps.CH, req),
		})
	}
}

// dimension.QueryDimensionResources queries ClickHouse for resource dimension data
// related to the specified service filter.
