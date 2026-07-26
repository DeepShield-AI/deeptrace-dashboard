package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"deeptrace-backend/aggregator"
	"deeptrace-backend/query/flowlog"
)

func RegisterMisc(mux *http.ServeMux, deps *Dependencies) {
	agg := deps.Aggregator

	mux.HandleFunc("/api/df-web/v1/icons", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		data, err := agg.ReadDataFileJSON("icons.json")
		if err != nil { writeSuccess(w, []interface{}{}); return }
		writeSuccess(w, data)
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
		writeSuccess(w, []interface{}{})
	})

	mux.HandleFunc("/api/df-web/v1/logo_info", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{
			"LOGO_URL": "", "FAVICON_URL": "", "TITLE": "DeepTrace",
		})
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
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		writeSuccess(w, []interface{}{})
	})

	mux.HandleFunc("/api/df-web/v1/fast_filter_black_lists", func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, map[string]interface{}{"BLACK_LIST": []interface{}{}})
	})

	mux.HandleFunc("/api/fuser/v1/user/", func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		writeSuccess(w, map[string]interface{}{"params": []interface{}{}})
	})

	// -----------------------------------------------------------------------
	// Composer routes — each registered with a specific pattern
	// -----------------------------------------------------------------------
	mux.HandleFunc("/api/df-web-composer/api/querier/fast_list/", handleFastList(deps))
	mux.HandleFunc("/api/df-web-composer/api/querier/fast_list", handleFastList(deps))
	mux.HandleFunc("/api/df-web-composer/api/service_topo/entry_path_overview", handleServiceOverview(agg))
	mux.HandleFunc("/api/df-web-composer/api/service_topo/", handleServiceTopo(agg))
	mux.HandleFunc("/api/df-web-composer/", handleComposerFallback(deps))
}

// ---------------------------------------------------------------------------
// fast_list — ZT-based tag value options
// ---------------------------------------------------------------------------

type fastListRequest struct {
	DB        string `json:"db"`
	Table     string `json:"table"`
	TimeStart int64  `json:"time_start"`
	TimeEnd   int64  `json:"time_end"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
	Where     *struct {
		ResourceSets []struct {
			ID        string `json:"id"`
			Condition []struct {
				Key string      `json:"key"`
				Op  string      `json:"op"`
				Val interface{} `json:"val"`
			} `json:"condition"`
		} `json:"resourceSets"`
	} `json:"where"`
}

func handleFastList(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		log.Printf("🎼 fast_list %s body=%d", r.URL.Path[:60], len(body))
		if data := queryFastList(deps, r, body); data != nil {
			writeJSON(w, map[string]interface{}{"OPT_STATUS": "SUCCESS", "DATA": data})
			return
		}
		writeSuccess(w, []interface{}{})
	}
}

func queryFastList(deps *Dependencies, r *http.Request, body []byte) []interface{} {
	zt := deps.Querier.Zerotrace
	if zt == nil || !zt.Available() { return nil }
	var req fastListRequest
	if err := json.Unmarshal(body, &req); err != nil { return nil }
	db := req.DB; if db == "" { db = "flow_log" }
	tbl := req.Table; if tbl == "" { tbl = "l7_flow_log" }

	selStart := strings.Index(r.URL.Path, "fast_list/")
	if selStart < 0 { return nil }
	selStr := r.URL.Path[selStart+len("fast_list/"):]
	if idx := strings.IndexByte(selStr, '?'); idx >= 0 { selStr = selStr[:idx] }
	if selStr == "" { return nil }

	var clauses []string
	if req.TimeStart > 0 { clauses = append(clauses, fmt.Sprintf("time >= %d", req.TimeStart)) }
	if req.TimeEnd > 0 { clauses = append(clauses, fmt.Sprintf("time <= %d", req.TimeEnd)) }
	if req.Where != nil {
		for _, rs := range req.Where.ResourceSets {
			for _, c := range rs.Condition {
				op := c.Op; if op == "" { op = "=" }
				clauses = append(clauses, fmt.Sprintf("`%s` %s %v", c.Key, op, c.Val))
			}
		}
	}
	whereClause := ""
	if len(clauses) > 0 { whereClause = " WHERE " + strings.Join(clauses, " AND ") }

	limit := req.Limit; if limit <= 0 { limit = 100 }
	sql := fmt.Sprintf("SELECT %s, Count(row) AS count_row FROM `%s`%s GROUP BY %s ORDER BY count_row DESC LIMIT %d",
		selStr, tbl, whereClause, selStr, limit)
	log.Printf("🔍 ZT fast_list: db=%s sql=%s", db, sql)

	rows, err := zt.QueryRaw(db, sql)
	if err != nil { log.Printf("⚠️  fast_list error: %v", err); return nil }

	data := make([]interface{}, 0, len(rows.Values))
	for _, row := range rows.Values {
		r := make(map[string]interface{})
		for i, col := range rows.Columns {
			if i >= len(row) { continue }
			val := row[i]
			if strings.HasPrefix(col, "Enum(") {
				if s, ok := val.(string); ok {
					if cn := flowlog.EnumZHCN(s); cn != "" { val = cn }
				}
			}
			r[col] = val
		}
		r["_querier_region"] = "本地"
		data = append(data, r)
	}
	return data
}

// ---------------------------------------------------------------------------
// service_topo
// ---------------------------------------------------------------------------

func handleServiceOverview(agg *aggregator.Aggregator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := agg.ReadDataFileJSON("service_overview.json")
		if err != nil {
			writeSuccess(w, map[string]interface{}{
				"overviewTrend": []interface{}{}, "overviewList": []interface{}{},
			})
			return
		}
		writeSuccess(w, data)
	}
}

func handleServiceTopo(agg *aggregator.Aggregator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "alert_event") {
			writeSuccess(w, map[string]interface{}{
				"alertLevelCount": map[string]int{}, "alertTrend": []interface{}{},
				"alertActiveLevelTrend": []interface{}{}, "alertActiveLevelIntervals": []interface{}{},
			})
			return
		}
		data, err := agg.ReadDataFileJSON("topo.json")
		if err != nil {
			writeSuccess(w, map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}})
			return
		}
		writeSuccess(w, data)
	}
}

// ---------------------------------------------------------------------------
// Fallback
// ---------------------------------------------------------------------------

func handleComposerFallback(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checkCache(w, deps, r.Method, r.URL.RequestURI()) { return }
		log.Printf("🎼 COMPOSER fallback %s %s", r.Method, r.URL.Path)
		writeSuccess(w, []interface{}{})
	}
}
