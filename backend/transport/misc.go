package transport

import (
	"log"
	"net/http"
	"strings"
)

func RegisterMisc(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/df-web/v1/icons", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		writeSuccess(w, []interface{}{})
	})
	mux.HandleFunc("/api/df-web/v1/config/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "outerlinks") { writeSuccess(w, []interface{}{}); return }
		writeSuccess(w, map[string]interface{}{
			"VERSION": "v7.1", "COMPANY": "DeepTrace",
			"SUPPORT_EMAIL": "admin@deeptrace.local", "SITE_TITLE": "DeepTrace",
			"DEPLOY_MODE": "k8s", "BILLING_METHOD": "voucher",
			"SFLOW_MENU_ENABLED": "false", "NTP_SERVERS": "0.cn.pool.ntp.org",
		})
	})
	mux.HandleFunc("/api/df-web/v1/indicator_template", func(w http.ResponseWriter, r *http.Request) {
		tableName := r.URL.Query().Get("table_name")
		dbName := r.URL.Query().Get("db_name")
		if tableName == "" {
			tableName = "application"
		}
		if dbName == "" {
			dbName = "flow_metrics"
		}
		writeSuccess(w, []interface{}{
			map[string]interface{}{
				"ID":              1,
				"LCUUID":          "00000000-0000-0000-0000-000000000001",
				"TEMPLATE_NAME":   "常用模板",
				"TABLE_NAME":      tableName,
				"DB_NAME":         dbName,
				"DEF_TEMP":        0,
				"USER_ID":         1,
				"CREATED_AT":      "2025-01-01 00:00:00",
				"UPDATED_AT":      "2025-01-01 00:00:00",
				"TEAM_INFO":       map[string]interface{}{"current_role": 4, "name": "Default", "team_id": 1},
				"TEAM_ID":         1,
				"OWNER_USER_INFO": map[string]interface{}{
					"ID": 1, "USERNAME": "admin", "COMPANY": "admin",
					"PHONE_NUM": "", "EMAIL": "admin@deepflow.local",
					"STATE": 1, "USER_TYPE": 1, "AUTH_TYPE": 1,
					"USERUUID": "1", "DEPARTMENT": "", "SUB_DEPARTMENT": "",
				},
				"ACCESS_ACTIONS":  []string{"read"},
			},
		})
	})
	mux.HandleFunc("/api/df-web/v1/logo_info", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{"LOGO_URL": "", "FAVICON_URL": "", "TITLE": "DeepTrace"})
	})
	mux.HandleFunc("/api/alarm/", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		writeSuccess(w, []interface{}{})
	})
	mux.HandleFunc("/api/warrant/", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{
			"LICENSE_DATA": true, "CHECK_HOST": true, "CHECK_IP": true,
			"LICENSE_FUNCTION": []string{
				"application_observation", "network_observation",
				"infrastructure_observation", "network_tracing",
				"system_tracing", "application_tracing",
				"call_log", "flow_log", "profile",
			},
		})
	})
	mux.HandleFunc("/api/df-web/v1/search-histories", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, []interface{}{})
	})
}

func handleComposerFallback(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		log.Printf("🎼 COMPOSER fallback %s %s", r.Method, r.URL.Path)
		writeSuccess(w, []interface{}{})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// toFloat safely converts a value to float64.
func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// toInt safely converts a value to int.
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}
