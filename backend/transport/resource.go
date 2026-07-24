package transport

import (
	"log"
	"net/http"
	"strings"
)

// RegisterResource adds resource API routes (deepflow-server proxy).
func RegisterResource(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/deepflow-server/", handleResource(deps))
}

func handleResource(deps *Dependencies) http.HandlerFunc {
	agg := deps.Aggregator
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}

		path := r.URL.Path
		if len(path) > 60 {
			log.Printf("🔧 RESOURCE %s", path[:60])
		} else {
			log.Printf("🔧 RESOURCE %s", path)
		}

		var data interface{}
		switch {
		case strings.Contains(path, "vtaps"):
			data, _ = agg.ReadDataFileJSON("agents.json")
		case strings.Contains(path, "domains"):
			data, _ = agg.ReadDataFileJSON("domains.json")
		case strings.Contains(path, "vpcs"):
			data, _ = agg.ReadDataFileJSON("vpcs.json")
		case strings.Contains(path, "pod-services"):
			data, _ = agg.ReadDataFileJSON("services.json")
		case strings.Contains(path, "pod-groups"):
			data, _ = agg.ReadDataFileJSON("pod_groups.json")
		case strings.Contains(path, "pods"):
			data, _ = agg.ReadDataFileJSON("pods.json")
		case strings.Contains(path, "subnets"):
			data, _ = agg.ReadDataFileJSON("subnets.json")
		case strings.Contains(path, "vms"):
			data, _ = agg.ReadDataFileJSON("vms.json")
		case strings.Contains(path, "data-sources"):
			writeSuccess(w, []map[string]interface{}{
				{"ID": 1, "NAME": "1m", "INTERVAL": 60, "RETENTION_TIME": 7},
				{"ID": 2, "NAME": "1s", "INTERVAL": 1, "RETENTION_TIME": 1},
			})
			return
		case strings.Contains(path, "biz-decode"):
			writeSuccess(w, []interface{}{})
			return
		default:
			writeSuccess(w, []interface{}{})
			return
		}
		if data == nil {
			data = []interface{}{}
		}
		writeSuccess(w, data)
	}
}
