package main

import (
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
)

func main() {
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
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

func handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"DESCRIPTION": "",
		"DATA": map[string]interface{}{
			"TOKEN":  "fake-token-deeptrace-2024",
			"SECRET": "fake-secret",
		},
	})
}

func handleLoginList(w http.ResponseWriter, r *http.Request) {
	ok(w, []map[string]interface{}{
		{"TYPE": "password", "ENABLED": true},
	})
}

func handleCurrentUser(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"ID": 1, "USERNAME": "admin", "EMAIL": "admin@deeptrace.local",
		"PHONE": "", "ROLE": "admin", "STATE": 1, "TYPE": 1,
		"DISPLAY_NAME": "管理员",
	})
}

func handleOrgs(w http.ResponseWriter, r *http.Request) {
	ok(w, []map[string]interface{}{
		{"ID": 4, "NAME": "DeepTrace", "LCUUID": "org-001", "TYPE": 1},
	})
}

func handleOrgRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.Contains(path, "page_scopes") {
		data, err := readDataFileJSON("page_scopes.json")
		if err != nil {
			// Return all pages enabled
			ok(w, map[string]interface{}{"pages": []string{
				"network", "application", "universal-map", "resources",
				"tracing", "log", "event", "alert", "dashboard",
			}})
			return
		}
		ok(w, data)
	} else if strings.Contains(path, "role_teams") {
		ok(w, []map[string]interface{}{
			{"ID": 1, "NAME": "默认团队", "ROLE": "admin"},
		})
	} else if strings.Contains(path, "teams") {
		ok(w, []map[string]interface{}{
			{"ID": 1, "NAME": "默认团队", "SHORT_LCUUID": "team-001"},
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
	data, err := readDataFileJSON("dashboards.json")
	if err != nil {
		ok(w, []interface{}{})
		return
	}
	ok(w, data)
}

func handleBiz(w http.ResponseWriter, r *http.Request) {
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
	ok(w, []interface{}{})
}

func handleIcons(w http.ResponseWriter, r *http.Request) {
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
		"THEME": "dark", "LANGUAGE": "zh-CN",
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
	ok(w, map[string]interface{}{"LICENSE": "valid", "EXPIRED": false})
}

func handleSearchHistories(w http.ResponseWriter, r *http.Request) {
	ok(w, []interface{}{})
}

func handleFastFilter(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"BLACK_LIST": []interface{}{}})
}

func handleUserConf(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{"params": []interface{}{}})
}

func handleAPIFallback(w http.ResponseWriter, r *http.Request) {
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
		http.ServeFile(w, r, filePath)
		return
	}

	// SPA fallback
	ext := filepath.Ext(path)
	if ext != "" && ext != ".html" {
		http.NotFound(w, r)
		return
	}

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
