package transport

import (
	"net/http"
	"strings"
)

func RegisterResource(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/deepflow-server/", handleResource(deps))
}

func handleResource(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/regions") {
			writeSuccess(w, []map[string]interface{}{
				{"ID": 1, "NAME": "本地", "LCUUID": "region-local", "SHORT_LCUUID": "local",
					"AZ_COUNT": 1, "EPC_COUNT": 0, "STATUS": 1, "CREATE_METHOD": 0,
					"CREATED_AT": "2024-01-01 00:00:00", "UPDATED_AT": "",
					"DOMAIN_LCUUIDS": []interface{}{}, "DELETED_AT": nil,
					"LATITUDE": nil, "ICON_ID": -4, "LABEL": "",
					"label": "本地", "value": "本地",
				},
			})
			return
		}
		if strings.Contains(path, "/data-sources") || strings.Contains(path, "/data_sources") {
			writeSuccess(w, []map[string]interface{}{
				{"ID": 1, "NAME": "1s", "DISPLAY_NAME": "秒级",
					"DATA_TABLE_COLLECTION": "flow_metrics.*", "STATE": 1,
					"BASE_DATA_SOURCE_ID": 0, "BASE_DATA_SOURCE_NAME": "",
					"INTERVAL": 1, "RETENTION_TIME": 168},
				{"ID": 2, "NAME": "1m", "DISPLAY_NAME": "分钟级",
					"DATA_TABLE_COLLECTION": "flow_metrics.*", "STATE": 1,
					"BASE_DATA_SOURCE_ID": 1, "BASE_DATA_SOURCE_NAME": "秒级",
					"INTERVAL": 60, "RETENTION_TIME": 720},
				{"ID": 3, "NAME": "flow_log.l7_flow_log", "DISPLAY_NAME": "流日志",
					"DATA_TABLE_COLLECTION": "flow_log.l7_flow_log", "STATE": 1,
					"BASE_DATA_SOURCE_ID": 0, "BASE_DATA_SOURCE_NAME": "",
					"INTERVAL": 0, "RETENTION_TIME": 168},
			})
			return
		}
		writeSuccess(w, []interface{}{})
	}
}
