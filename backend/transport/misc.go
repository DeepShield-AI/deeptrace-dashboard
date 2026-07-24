package transport

import (
	"io"
	"log"
	"net/http"
	"strings"
)

// RegisterMisc registers miscellaneous API routes (icons, config, alarm, composer, etc.).
func RegisterMisc(mux *http.ServeMux, deps *Dependencies) {
	// Icons.
	mux.HandleFunc("/api/df-web/v1/icons", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		data, err := deps.Aggregator.ReadDataFileJSON("icons.json")
		if err != nil {
			writeSuccess(w, []interface{}{})
			return
		}
		writeSuccess(w, data)
	})

	// Config.
	mux.HandleFunc("/api/df-web/v1/config/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "outerlinks") {
			writeSuccess(w, []interface{}{})
			return
		}
		writeSuccess(w, map[string]interface{}{
			"VERSION": "v7.1", "COMPANY": "DeepTrace",
			"SUPPORT_EMAIL": "admin@deeptrace.local",
			"SITE_TITLE":    "DeepTrace", "DEPLOY_MODE": "k8s",
			"BILLING_METHOD": "voucher", "SFLOW_MENU_ENABLED": "false",
			"NTP_SERVERS": "0.cn.pool.ntp.org",
		})
	})

	// Indicator template.
	mux.HandleFunc("/api/df-web/v1/indicator_template", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, []interface{}{})
	})

	// Logo info.
	mux.HandleFunc("/api/df-web/v1/logo_info", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{
			"LOGO_URL": "", "FAVICON_URL": "", "TITLE": "DeepTrace",
		})
	})

	// Alarm.
	mux.HandleFunc("/api/alarm/", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		writeSuccess(w, []interface{}{})
	})

	// Warrant / license.
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

	// Search histories.
	mux.HandleFunc("/api/df-web/v1/search-histories", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		writeSuccess(w, []interface{}{})
	})

	// Fast filter black list.
	mux.HandleFunc("/api/df-web/v1/fast_filter_black_lists", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{"BLACK_LIST": []interface{}{}})
	})

	// User config.
	mux.HandleFunc("/api/fuser/v1/user/", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}
		writeSuccess(w, map[string]interface{}{"params": []interface{}{}})
	})

	// -----------------------------------------------------------------------
	// Composer
	// -----------------------------------------------------------------------
	mux.HandleFunc("/api/df-web-composer/", handleComposer(deps))
}

func handleComposer(deps *Dependencies) http.HandlerFunc {
	agg := deps.Aggregator
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) {
			return
		}

		body, _ := io.ReadAll(r.Body)
		log.Printf("🎼 COMPOSER %s %s body=%d", r.Method, r.URL.Path[:min(len(r.URL.Path), 60)], len(body))
		path := r.URL.Path

		switch {
		case strings.Contains(path, "service_topo") && strings.Contains(path, "entry_path_overview"):
			data, err := agg.ReadDataFileJSON("service_overview.json")
			if err != nil {
				writeSuccess(w, map[string]interface{}{
					"overviewTrend": []interface{}{}, "overviewList": []interface{}{},
				})
				return
			}
			writeSuccess(w, data)

		case strings.Contains(path, "service_topo") && strings.Contains(path, "alert_event"):
			writeSuccess(w, map[string]interface{}{
				"alertLevelCount": map[string]int{}, "alertTrend": []interface{}{},
				"alertActiveLevelTrend": []interface{}{}, "alertActiveLevelIntervals": []interface{}{},
			})

		case strings.Contains(path, "service_topo") && strings.Contains(path, "flow_"):
			data, err := agg.ReadDataFileJSON("topo.json")
			if err != nil {
				writeSuccess(w, map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}})
				return
			}
			writeSuccess(w, data)

		case strings.Contains(path, "fast_list") || strings.Contains(path, "querier"):
			data, err := agg.ReadDataFileJSON("fast_list.json")
			if err != nil {
				writeSuccess(w, []interface{}{})
				return
			}
			writeSuccess(w, data)

		default:
			writeSuccess(w, []interface{}{})
		}
	}
}
