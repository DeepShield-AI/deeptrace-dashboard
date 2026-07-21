package main

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	port      = "8888"
	staticDir = "../cloud.deepflow.yunshan.net"
	dataDir   = "./data"
	cacheDir  = "../api_cache"
)

// apiCache maps cache keys to response bodies
// Key format: "METHOD /path" or "METHOD /path body_hash" for POST with body
var apiCache = map[string][]byte{}
var apiCacheWithBody = map[string]map[string][]byte{} // path -> {body_substring -> response}

func loadAPICache() {
	files, err := os.ReadDir(cacheDir)
	if err != nil {
		log.Printf("⚠️  No api_cache dir: %v", err)
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") || f.Name() == "_index.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, f.Name()))
		if err != nil {
			continue
		}
		var entry struct {
			Method       string `json:"method"`
			Path         string `json:"path"`
			ResponseBody string `json:"responseBody"`
			IsBase64     bool   `json:"responseIsBase64"`
		}
		if json.Unmarshal(data, &entry) != nil {
			continue
		}
		var respBody []byte
		if entry.IsBase64 {
			respBody, _ = base64.StdEncoding.DecodeString(entry.ResponseBody)
		} else {
			respBody = []byte(entry.ResponseBody)
		}
		if len(respBody) == 0 {
			continue
		}
		basePath := strings.Split(entry.Path, "?")[0]
		key := entry.Method + " " + basePath

		// For POST requests with body, store with body-awareness
		reqBody := ""
		if raw, ok2 := func() (string, bool) {
			var full map[string]interface{}
			json.Unmarshal(data, &full)
			if rb, ok3 := full["requestBody"]; ok3 {
				if s, ok4 := rb.(string); ok4 {
					return s, true
				}
			}
			return "", false
		}(); ok2 && raw != "" && raw != "{}" {
			reqBody = raw
		}

		if reqBody != "" {
			if apiCacheWithBody[key] == nil {
				apiCacheWithBody[key] = map[string][]byte{}
			}
			apiCacheWithBody[key][reqBody] = respBody
		}
		if _, exists := apiCache[key]; !exists {
			apiCache[key] = respBody
		}
	}
	log.Printf("📦 Loaded %d API cache entries", len(apiCache))
}

func findCachedResponse(method, urlPath string) []byte {
	basePath := strings.Split(urlPath, "?")[0]
	key := method + " " + basePath
	if resp, ok := apiCache[key]; ok {
		return resp
	}
	return nil
}

func findCachedResponseWithBody(method, urlPath, body string) []byte {
	basePath := strings.Split(urlPath, "?")[0]
	key := method + " " + basePath
	if bodyMap, ok := apiCacheWithBody[key]; ok && body != "" {
		// 1. Exact body match
		if resp, ok2 := bodyMap[body]; ok2 {
			return resp
		}
		// 2. Structured match on DATABASE/TABLE/TAG fields (handles key-order differences)
		var reqMap map[string]interface{}
		if json.Unmarshal([]byte(body), &reqMap) == nil && len(reqMap) > 0 {
			reqDB := fmt.Sprintf("%v", reqMap["DATABASE"])
			reqTable := fmt.Sprintf("%v", reqMap["TABLE"])
			reqTag := fmt.Sprintf("%v", reqMap["TAG"])
			var bestResp []byte
			bestScore := 0
			for cachedBody, resp := range bodyMap {
				var cMap map[string]interface{}
				if json.Unmarshal([]byte(cachedBody), &cMap) != nil {
					continue
				}
				score := 0
				if fmt.Sprintf("%v", cMap["DATABASE"]) == reqDB && reqDB != "<nil>" {
					score++
				}
				if fmt.Sprintf("%v", cMap["TABLE"]) == reqTable && reqTable != "<nil>" {
					score += 2
				}
				if fmt.Sprintf("%v", cMap["TAG"]) == reqTag && reqTag != "<nil>" {
					score += 4
				}
				if score > bestScore {
					bestScore = score
					bestResp = resp
				}
			}
			if bestResp != nil {
				return bestResp
			}
		}
	}
	// Fallback to simple cache (path-only match)
	if resp, ok := apiCache[key]; ok {
		return resp
	}
	return nil
}

func main() {
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	loadAPICache()

	mux := http.NewServeMux()

	// ==================== Auth APIs (hardcoded) ====================
	mux.HandleFunc("/api/fauths/login", handleLogin)
	mux.HandleFunc("/api/fauths/login_list", handleLoginList)
	mux.HandleFunc("/api/fuser/v1/users/current", handleCurrentUser)
	mux.HandleFunc("/api/fpermit/v1/orgs", handleOrgs)
	mux.HandleFunc("/api/fpermit/v1/org/", handleOrgRoutes)

	// ==================== Data APIs (from files) ====================
	mux.HandleFunc("/api/statistics/v1/stats/querier/Topo", handleTopo)
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/", handleDBDescription)
	mux.HandleFunc("/api/querier/v1/query/", handleQuery)
	mux.HandleFunc("/api/df-web-composer/", handleComposer)
	mux.HandleFunc("/api/df-web/v1/dashboards", handleDashboards)
	mux.HandleFunc("/api/df-web/v1/biz/", handleBiz)
	mux.HandleFunc("/api/df-web/v1/icons", handleIcons)
	mux.HandleFunc("/api/df-web/v1/config/", handleConfig)
	mux.HandleFunc("/api/df-web/v1/indicator_template", handleIndicatorTemplate)
	mux.HandleFunc("/api/df-web/v1/logo_info", handleLogoInfo)
	mux.HandleFunc("/api/deepflow-server/", handleDeepflowServer)
	mux.HandleFunc("/api/alarm/", handleAlarm)

	mux.HandleFunc("/api/warrant/", handleWarrant)
	mux.HandleFunc("/api/df-web/v1/search-histories", handleSearchHistories)
	mux.HandleFunc("/api/df-web/v1/fast_filter_black_lists", handleFastFilter)
	mux.HandleFunc("/api/fuser/v1/user/", handleUserConf)

	// Catch-all for other /api/ paths
	mux.HandleFunc("/api/", handleAPIFallback)

	// ==================== Static files + SPA fallback ====================
	mux.HandleFunc("/", handleStatic)

	// Wrap with CORS
	handler := corsMiddleware(mux)

	log.Printf("🚀 DeepTrace Backend starting on :%s", port)
	log.Printf("   Static: %s", staticDir)
	log.Printf("   Data:   %s", dataDir)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

// ============================================================
// Middleware
// ============================================================

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		w = rec
		defer func() {
			if rec.status >= 400 {
				log.Printf("⛔ %d %s %s", rec.status, r.Method, r.URL.Path)
			}
		}()
		// Log all requests with full URL and status
		if strings.HasPrefix(r.URL.Path, "/api/") || !strings.Contains(r.URL.Path, ".") {
			log.Printf("→ %s %s%s", r.Method, r.URL.Path, func() string {
				if r.URL.RawQuery != "" {
					return "?" + r.URL.RawQuery
				}
				return ""
			}())
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "X-Org-Id, Crypto, X-Short-Refresh-Token-Key")
		// Critical: frontend reads X-Org-Id header to determine current org
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("X-Org-Id", "4")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ============================================================
// Helpers
// ============================================================

func ok(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DATA":        data,
		"DESCRIPTION": "",
	})
}

func readDataFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dataDir, name))
}

func readDataFileJSON(name string) (interface{}, error) {
	data, err := readDataFile(name)
	if err != nil {
		return nil, err
	}
	var v interface{}
	err = json.Unmarshal(data, &v)
	return v, err
}

// ============================================================
// Auth Handlers (hardcoded - no DB needed)
// ============================================================

// JWT token with org_id=4, team_id=1 embedded in payload
const fakeAccessToken = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTksImlhdCI6MTcyMTUwMDAwMCwiaXNzIjoiYXV0aDp0b2tlbiIsImRhdGEiOnsiaWQiOjEsInVzZXJuYW1lIjoiYWRtaW4iLCJlbWFpbCI6ImFkbWluQGRlZXB0cmFjZS5sb2NhbCIsImxvZ2luX3RpbWUiOjE3MjE1MDAwMDAsInJlZnJlc2hfdGltZSI6MTcyMTUwMDAwMCwidG9rZW5fa2V5IjoiZmFrZS1rZXkiLCJvcmdfaWQiOjQsInRlYW1faWQiOjF9fQ.ZmFrZS1zaWduYXR1cmU"
const fakeRefreshToken = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTksImlhdCI6MTcyMTUwMDAwMH0.ZmFrZS1zaWduYXR1cmU"

func handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
		"DATA": map[string]interface{}{
			"access_token":  fakeAccessToken,
			"refresh_token": fakeRefreshToken,
		},
	})
}

func handleLoginList(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"1": map[string]interface{}{
			"name": "DeepFlow", "platform": "deepflow", "type": "deepflow",
			"format": "email", "login_account_mode": []string{"email", "phone"}, "raw_input": true,
		},
	})
}

func handleCurrentUser(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"ID": 1, "USERNAME": "admin", "EMAIL": "admin@deeptrace.local",
		"PHONE_NUM": "", "USER_TYPE": 5, "REAL_USER_TYPE": 5,
		"USERUUID": "admin-uuid-001", "ORG_ID": 4, "COMPANY": "DeepTrace",
		"ACCESS_TOKEN": fakeAccessToken,
		"ACCOUNT_RULE": map[string]interface{}{
			"account_allowed_login_time_period": false,
			"account_allowed_login_min_time":    0,
			"account_allowed_login_max_time":    0,
			"account_not_login_lock_time":       0,
			"account_not_change_pwd_lock_time":  0,
			"account_first_login_change_pwd":    false,
			"account_second_check":              false,
			"account_login_failed_count":        0,
			"account_login_failed_locked_time":  60,
			"account_allow_login_white_list_ip": "*",
			"verifycode_use":                    false,
			"user_limit":                        100,
			"user_tenant_limit":                 200,
			"use_ungrouped_type":                false,
			"read_only_admin":                   false,
			"radius_account_switch":             false,
		},
		"PWD_RULE": map[string]interface{}{
			"pwd_min_len": 8, "pwd_max_len": 16,
			"pwd_include_number": false, "pwd_include_string": false,
			"pwd_include_case": false, "pwd_include_special_chars": false,
		},
		"SESSION_RULE": map[string]interface{}{
			"session_inactive_close": false, "session_inactive_close_interval": 0,
			"session_single": false, "session_max_online": 0, "session_one_client": false,
		},
		"SSO_RULE":          map[string]interface{}{"sso_open": false, "sso_link": []interface{}{}},
		"FILE_STORAGE_RULE": map[string]interface{}{"file_storage_extension": "rpm,iso,gz,zip,tar", "file_storage_size": 3072},
		"SEARCH_RULE":       nil,
		"TENANT_ORG_CONFIG": map[string]interface{}{"org_create_enable": false, "org_create_max_num": 2},
	})
}

func handleOrgs(w http.ResponseWriter, r *http.Request) {
	ok(w, []map[string]interface{}{
		{
			"ID": 4, "LCUUID": "org-uuid-001", "ORG_ID": 4, "NAME": "DeepTrace",
			"DESC": "", "STATUS": 0, "OWNER_USER_ID": 1,
			"DISABLED_DELETE": false, "USER_NUM": 1,
			"OWNER_USER_INFO": map[string]interface{}{"ID": 1, "EMAIL": "admin@deeptrace.local", "USER_TYPE": 5},
		},
	})
}

func handleOrgRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.Contains(path, "select") {
		// org select - just acknowledge, do NOT return tokens (avoids reload loop)
		http.SetCookie(w, &http.Cookie{
			Name: "X-Org-Id", Value: "4", Path: "/", MaxAge: 86400 * 365,
		})
		ok(w, nil)
	} else if strings.Contains(path, "page_scopes") {
		// team-level page_scopes returns array with SCOPE field
		if strings.Contains(path, "team") {
			ok(w, []map[string]interface{}{
				{"ID": 1, "LCUUID": "scope-001", "SCOPE": "[]", "TEAM_ID": 1},
			})
		} else {
			ok(w, map[string]interface{}{"pages": []interface{}{}})
		}
	} else if strings.Contains(path, "role_teams") {
		ok(w, []map[string]interface{}{
			{"ID": 1, "NAME": "默认团队", "ROLE": "owner", "SHORT_LCUUID": "team-001", "ORG_ID": 4},
		})
	} else if strings.Contains(path, "teams") {
		ok(w, []map[string]interface{}{
			{"ID": 1, "NAME": "默认团队", "SHORT_LCUUID": "team-001", "ORG_ID": 4},
		})
	} else {
		ok(w, []interface{}{})
	}
}

// ============================================================
// Topo Handler - 全景拓扑（服务地图）
// ============================================================

func handleTopo(w http.ResponseWriter, r *http.Request) {
	data, err := readDataFileJSON("topo.json")
	if err != nil {
		log.Printf("⚠️  topo.json not found, returning empty")
		ok(w, map[string]interface{}{
			"instance_data": []interface{}{},
			"peers_data":    []interface{}{},
		})
		return
	}
	ok(w, data)
}

// ============================================================
// Querier - SQL 查询引擎
// ============================================================

func handleQuery(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]string
	json.Unmarshal(body, &req)

	db := req["db"]
	sql := strings.ToLower(req["sql"])
	log.Printf("📊 QUERY db=%s sql=%s", db, sql[:min(len(sql), 80)])

	// Route to different data files based on query content
	var dataFile string
	switch {
	case strings.Contains(sql, "l7_flow_log") || strings.Contains(sql, "trace"):
		dataFile = "traces.json"
	case strings.Contains(sql, "flow_metrics") || strings.Contains(sql, "vtap_app_port"):
		dataFile = "metrics.json"
	case strings.Contains(sql, "event"):
		dataFile = "events.json"
	default:
		dataFile = "query_default.json"
	}

	data, err := readDataFileJSON(dataFile)
	if err != nil {
		ok(w, []interface{}{})
		return
	}
	ok(w, data)
}

func handleDBDescription(w http.ResponseWriter, r *http.Request) {
	// Try cached response with body matching
	body, _ := io.ReadAll(r.Body)
	bodyStr := string(body)
	if cached := findCachedResponseWithBody(r.Method, r.URL.Path, bodyStr); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	path := r.URL.Path
	switch {
	case strings.Contains(path, "ShowDatabases"):
		ok(w, []map[string]interface{}{
			{"name": "flow_metrics", "datasources": []string{"1m", "1s"}},
			{"name": "flow_log", "datasources": []string{"l4_flow_log", "l7_flow_log"}},
			{"name": "event", "datasources": []string{"perf_event", "alarm_event"}},
		})
	case strings.Contains(path, "ShowTables"):
		data, err := readDataFileJSON("tables.json")
		if err != nil {
			ok(w, []map[string]interface{}{
				{"name": "vtap_app_port", "datasources": []string{"1m", "1s"}},
				{"name": "vtap_flow_port", "datasources": []string{"1m", "1s"}},
				{"name": "application_map", "datasources": []string{"1m"}},
				{"name": "network_map", "datasources": []string{"1m"}},
			})
			return
		}
		ok(w, data)
	case strings.Contains(path, "ShowTag"):
		data, err := readDataFileJSON("tags.json")
		if err != nil {
			ok(w, []map[string]interface{}{
				{"name": "auto_service", "display_name": "服务", "type": "resource"},
				{"name": "auto_instance", "display_name": "实例", "type": "resource"},
				{"name": "ip", "display_name": "IP地址", "type": "resource"},
				{"name": "protocol", "display_name": "协议", "type": "int_enum"},
			})
			return
		}
		ok(w, data)
	case strings.Contains(path, "ShowMetrics"):
		data, err := readDataFileJSON("metrics.json")
		if err != nil {
			ok(w, []map[string]interface{}{
				{"name": "byte", "display_name": "字节", "is_agg": true, "unit": "bytes"},
				{"name": "request", "display_name": "请求数", "is_agg": true, "unit": "count"},
				{"name": "response_delay", "display_name": "响应时延", "is_agg": true, "unit": "us"},
			})
			return
		}
		ok(w, data)
	default:
		ok(w, []interface{}{})
	}
}

// ============================================================
// Composer - 服务拓扑详情
// ============================================================

func handleComposer(w http.ResponseWriter, r *http.Request) {
	// Try cached response first
	if cached := findCachedResponse(r.Method, r.URL.Path); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	path := r.URL.Path
	body, _ := io.ReadAll(r.Body)

	log.Printf("🎼 COMPOSER %s %s body=%d", r.Method, path[:min(len(path), 60)], len(body))

	switch {
	case strings.Contains(path, "service_topo") && strings.Contains(path, "entry_path_overview"):
		data, err := readDataFileJSON("service_overview.json")
		if err != nil {
			ok(w, map[string]interface{}{
				"overviewTrend": []interface{}{},
				"overviewList":  []interface{}{},
			})
			return
		}
		ok(w, data)
	case strings.Contains(path, "service_topo") && strings.Contains(path, "alert_event"):
		ok(w, map[string]interface{}{
			"alertLevelCount":           map[string]int{},
			"alertTrend":                []interface{}{},
			"alertActiveLevelTrend":     []interface{}{},
			"alertActiveLevelIntervals": []interface{}{},
		})
	case strings.Contains(path, "service_topo") && strings.Contains(path, "flow_"):
		data, err := readDataFileJSON("topo.json")
		if err != nil {
			ok(w, map[string]interface{}{"instance_data": []interface{}{}, "peers_data": []interface{}{}})
			return
		}
		ok(w, data)
	case strings.Contains(path, "fast_list") || strings.Contains(path, "querier"):
		data, err := readDataFileJSON("fast_list.json")
		if err != nil {
			ok(w, []interface{}{})
			return
		}
		ok(w, data)
	default:
		ok(w, []interface{}{})
	}
}

// ============================================================
// Dashboard
// ============================================================

func handleDashboards(w http.ResponseWriter, r *http.Request) {
	if cached := findCachedResponse(r.Method, r.URL.Path); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	data, err := readDataFileJSON("dashboards.json")
	if err != nil {
		ok(w, []interface{}{})
		return
	}
	ok(w, data)
}

func handleBiz(w http.ResponseWriter, r *http.Request) {
	if cached := findCachedResponse(r.Method, r.URL.Path); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	// Extract UUID from path: /api/df-web/v1/biz/{uuid}
	parts := strings.Split(r.URL.Path, "/")
	uuid := ""
	for i, p := range parts {
		if p == "biz" && i+1 < len(parts) {
			uuid = parts[i+1]
			break
		}
	}

	filename := fmt.Sprintf("dashboard_%s.json", uuid)
	data, err := readDataFileJSON(filename)
	if err != nil {
		// Try generic dashboard
		data, err = readDataFileJSON("dashboard_default.json")
		if err != nil {
			ok(w, map[string]interface{}{
				"ID": 1, "NAME": "默认仪表盘", "LCUUID": uuid,
				"JSON_CONFIG": "{}",
			})
			return
		}
	}
	ok(w, data)
}

// ============================================================
// Resources - 采集器/域/VPC/Pod etc
// ============================================================

func handleDeepflowServer(w http.ResponseWriter, r *http.Request) {
	// Try cached response first
	if cached := findCachedResponse(r.Method, r.URL.Path); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	path := r.URL.Path
	log.Printf("🔧 RESOURCE %s", path[:min(len(path), 60)])

	switch {
	case strings.Contains(path, "vtaps"):
		data, _ := readDataFileJSON("agents.json")
		if data == nil {
			data = []interface{}{}
		}
		ok(w, data)
	case strings.Contains(path, "domains"):
		data, _ := readDataFileJSON("domains.json")
		if data == nil {
			data = []interface{}{}
		}
		ok(w, data)
	case strings.Contains(path, "vpcs"):
		data, _ := readDataFileJSON("vpcs.json")
		if data == nil {
			data = []interface{}{}
		}
		ok(w, data)
	case strings.Contains(path, "pod-services"):
		data, _ := readDataFileJSON("services.json")
		if data == nil {
			data = []interface{}{}
		}
		ok(w, data)
	case strings.Contains(path, "pod-groups"):
		data, _ := readDataFileJSON("pod_groups.json")
		if data == nil {
			data = []interface{}{}
		}
		ok(w, data)
	case strings.Contains(path, "pods"):
		data, _ := readDataFileJSON("pods.json")
		if data == nil {
			data = []interface{}{}
		}
		ok(w, data)
	case strings.Contains(path, "subnets"):
		data, _ := readDataFileJSON("subnets.json")
		if data == nil {
			data = []interface{}{}
		}
		ok(w, data)
	case strings.Contains(path, "vms"):
		data, _ := readDataFileJSON("vms.json")
		if data == nil {
			data = []interface{}{}
		}
		ok(w, data)
	case strings.Contains(path, "data-sources"):
		ok(w, []map[string]interface{}{
			{"ID": 1, "NAME": "1m", "INTERVAL": 60, "RETENTION_TIME": 7},
			{"ID": 2, "NAME": "1s", "INTERVAL": 1, "RETENTION_TIME": 1},
		})
	case strings.Contains(path, "biz-decode"):
		ok(w, []interface{}{})
	default:
		ok(w, []interface{}{})
	}
}

// ============================================================
// Others
// ============================================================

func handleAlarm(w http.ResponseWriter, r *http.Request) {
	if cached := findCachedResponse(r.Method, r.URL.Path); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	ok(w, []interface{}{})
}

func handleIcons(w http.ResponseWriter, r *http.Request) {
	if cached := findCachedResponse(r.Method, r.URL.Path); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	data, err := readDataFileJSON("icons.json")
	if err != nil {
		ok(w, []interface{}{})
		return
	}
	ok(w, data)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "outerlinks") {
		ok(w, []interface{}{})
		return
	}
	ok(w, map[string]interface{}{
		"VERSION": "v7.1", "COMPANY": "DeepTrace",
		"SUPPORT_EMAIL": "admin@deeptrace.local",
		"SITE_TITLE":    "DeepTrace", "DEPLOY_MODE": "k8s",
		"BILLING_METHOD": "voucher", "SFLOW_MENU_ENABLED": "false",
		"NTP_SERVERS": "0.cn.pool.ntp.org",
	})
}

func handleIndicatorTemplate(w http.ResponseWriter, r *http.Request) {
	ok(w, []interface{}{})
}

func handleLogoInfo(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"LOGO_URL": "", "FAVICON_URL": "", "TITLE": "DeepTrace",
	})
}

func handleWarrant(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"LICENSE_DATA": true,
		"CHECK_HOST":   true,
		"CHECK_IP":     true,
		"LICENSE_FUNCTION": []string{
			"application_observation",
			"network_observation",
			"infrastructure_observation",
			"network_tracing",
			"system_tracing",
			"application_tracing",
			"call_log",
			"flow_log",
			"profile",
		},
	})
}

func handleSearchHistories(w http.ResponseWriter, r *http.Request) {
	if cached := findCachedResponse(r.Method, r.URL.Path); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	ok(w, []interface{}{})
}

func handleFastFilter(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"BLACK_LIST": []interface{}{}})
}

func handleUserConf(w http.ResponseWriter, r *http.Request) {
	if cached := findCachedResponse(r.Method, r.URL.Path); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	ok(w, map[string]interface{}{"params": []interface{}{}})
}

func handleAPIFallback(w http.ResponseWriter, r *http.Request) {
	// Try cached response (body-aware for POST)
	var bodyStr string
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		body, _ := io.ReadAll(r.Body)
		bodyStr = string(body)
	}
	if cached := findCachedResponseWithBody(r.Method, r.URL.Path, bodyStr); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}
	log.Printf("❓ UNHANDLED %s %s", r.Method, r.URL.Path)
	ok(w, []interface{}{})
}

// ============================================================
// Static file serving + SPA fallback
// ============================================================

func handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	filePath := filepath.Join(staticDir, path)
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		// No cache for HTML files
		if strings.HasSuffix(path, ".html") || path == "/" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
		}
		// Gzip compress JS/CSS files
		ext := filepath.Ext(path)
		if (ext == ".js" || ext == ".css") && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", map[string]string{".js": "application/javascript", ".css": "text/css"}[ext])
			w.Header().Set("Vary", "Accept-Encoding")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			data, _ := os.ReadFile(filePath)
			w.WriteHeader(200)
			gz.Write(data)
			return
		}
		http.ServeFile(w, r, filePath)
		return
	}

	// SPA fallback
	spaExt := filepath.Ext(path)
	if spaExt != "" && spaExt != ".html" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
}

// ============================================================
// Utility
// ============================================================

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func init() {
	_ = time.Now()
}
