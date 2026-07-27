package transport

import (
	"fmt"
	"net/http"
)

// DataSource matches the cloud API field order exactly.
type DataSource struct {
	ID                          int    `json:"ID"`
	Name                        string `json:"NAME"`
	DisplayName                 string `json:"DISPLAY_NAME"`
	DataTableCollection         string `json:"DATA_TABLE_COLLECTION"`
	State                       int    `json:"STATE"`
	BaseDataSourceID            int    `json:"BASE_DATA_SOURCE_ID"`
	BaseDataSourceName          string `json:"BASE_DATA_SOURCE_NAME"`
	Interval                    int    `json:"INTERVAL"`
	RetentionTime               int    `json:"RETENTION_TIME"`
	QueryTime                   int    `json:"QUERY_TIME"`
	SummableMetricsOperator     string `json:"SUMMABLE_METRICS_OPERATOR"`
	UnsummableMetricsOperator   string `json:"UNSUMMABLE_METRICS_OPERATOR"`
	IsDefault                   bool   `json:"IS_DEFAULT"`
	UpdatedAt                   string `json:"UPDATED_AT"`
	LCUUID                      string `json:"LCUUID"`
}

func RegisterResource(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/deepflow-server/v2/regions", handleRegions)
	mux.HandleFunc("/api/deepflow-server/v1/regions", handleRegions)
	mux.HandleFunc("/api/deepflow-server/v1/data-sources/", handleDataSources(deps))
	mux.HandleFunc("/api/deepflow-server/v1/data_sources/", handleDataSources(deps))
	mux.HandleFunc("/api/deepflow-server/", handleResourceFallback)
}

// ---------------------------------------------------------------------------
// Regions
// ---------------------------------------------------------------------------

func handleRegions(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, []map[string]interface{}{
		{"ID": 1, "NAME": "本地", "LCUUID": "region-local", "SHORT_LCUUID": "local",
			"AZ_COUNT": 1, "EPC_COUNT": 0, "STATUS": 1, "CREATE_METHOD": 0,
			"CREATED_AT": "2024-01-01 00:00:00", "UPDATED_AT": "",
			"DOMAIN_LCUUIDS": []interface{}{}, "DELETED_AT": nil,
			"LATITUDE": nil, "ICON_ID": -4, "LABEL": "",
			"label": "本地", "value": "本地",
		},
	})
}

// ---------------------------------------------------------------------------
// Data Sources
// ---------------------------------------------------------------------------

func handleDataSources(_ *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeSuccess(w, queryDataSources())
	}
}

// queryDataSources returns the data source list matching the cloud API exactly.
func queryDataSources() []DataSource {
	// All cloud data sources with exact IDs, names, display names, collections and metadata.
	// IDs and LCUUIDs match the cloud API exactly for known entries.
	ds := []DataSource{
		// ID  NAME                       DISPLAY_NAME              COLLECTION                    STATE  BASE_ID BASE_NAME    INTERVAL RETENTION QTIME SUMMABLE UNSMMABLE DEF UPDATED_AT
		{1, "1s", "网络-指标（秒级）", "flow_metrics.network*", 1, 0, "", 1, 24, 0, "", "", true, "2025-09-14 16:02:47", dataSourceLCUUID(1)},
		{3, "1m", "网络-指标（分钟级）", "flow_metrics.network*", 1, 1, "网络-指标（秒级）", 60, 48, 0, "Sum", "Avg", true, "2025-09-14 16:02:56", dataSourceLCUUID(3)},
		{6, "flow_log.l4_flow_log", "网络-流日志", "flow_log.l4_flow_log", 1, 0, "", 0, 48, 180, "", "", true, "2025-09-14 16:03:03", dataSourceLCUUID(6)},
		{7, "1s", "应用-指标（秒级）", "flow_metrics.application*", 1, 0, "", 1, 24, 0, "Sum", "Avg", true, "2025-09-14 16:03:20", dataSourceLCUUID(7)},
		{8, "1m", "应用-指标（分钟级）", "flow_metrics.application*", 1, 7, "应用-指标（秒级）", 60, 48, 0, "Sum", "Avg", true, "2025-09-14 16:03:27", dataSourceLCUUID(8)},
		{9, "flow_log.l7_flow_log", "应用-调用日志", "flow_log.l7_flow_log", 1, 0, "", 0, 48, 1440, "", "", true, "2025-09-14 16:03:35", dataSourceLCUUID(9)},
		{10, "flow_log.l4_packet", "网络-TCP 时序数据", "flow_log.l4_packet", 1, 0, "", 0, 72, 0, "", "", true, "2025-09-14 16:03:29", dataSourceLCUUID(10)},
		{11, "flow_log.l7_packet", "网络-PCAP 数据", "flow_log.l7_packet", 1, 0, "", 0, 4, 0, "", "", true, "2026-04-14 14:25:57", dataSourceLCUUID(11)},
		{12, "deepflow_tenant", "租户侧监控数据", "deepflow_tenant.*", 1, 0, "", 10, 48, 0, "", "", true, "2025-09-14 16:03:47", dataSourceLCUUID(12)},
		{13, "ext_metrics", "外部指标数据", "ext_metrics.*", 1, 0, "", 10, 48, 0, "", "", true, "2025-09-14 16:03:53", dataSourceLCUUID(13)},
		{14, "prometheus", "Prometheus 数据", "prometheus.*", 1, 0, "", 10, 48, 0, "", "", true, "2025-09-14 16:04:00", dataSourceLCUUID(14)},
		{15, "event.event", "事件-资源变更事件", "event.event", 1, 0, "", 0, 48, 4320, "", "", true, "2025-09-14 16:04:07", dataSourceLCUUID(15)},
		{16, "event.file_event", "事件-文件读写事件", "event.file_event", 1, 0, "", 0, 168, 0, "", "", true, "2025-10-21 19:04:38", dataSourceLCUUID(16)},
		{17, "event.alert_event", "事件-告警事件", "event.alert_event", 1, 0, "", 0, 48, 0, "", "", true, "2026-04-14 10:27:35", dataSourceLCUUID(17)},
		{18, "profile.in_process", "应用-性能剖析", "profile.in_process", 1, 0, "", 0, 48, 360, "", "", true, "2025-09-14 16:04:28", dataSourceLCUUID(18)},
		{19, "1m", "网络-网络策略", "flow_metrics.traffic_policy", 1, 0, "", 60, 48, 0, "Sum", "Avg", true, "2025-09-14 16:04:35", dataSourceLCUUID(19)},
		{20, "1s", "日志-日志数据", "application_log.log", 1, 0, "", 1, 48, 0, "Sum", "Avg", true, "2025-09-14 16:04:40", dataSourceLCUUID(20)},
		{25, "1h", "网络-指标（小时级）", "flow_metrics.network*", 1, 3, "网络-指标（分钟级）", 3600, 720, 0, "Sum", "Avg", true, "2025-09-14 16:04:47", dataSourceLCUUID(25)},
		{26, "1d", "网络-指标-天级", "flow_metrics.network*", 1, 3, "网络-指标（分钟级）", 86400, 720, 0, "Sum", "Avg", true, "2025-09-14 16:04:54", dataSourceLCUUID(26)},
		{27, "1h", "应用-指标（小时级）", "flow_metrics.application*", 1, 8, "应用-指标（分钟级）", 3600, 720, 0, "Sum", "Avg", true, "2025-09-14 16:05:01", dataSourceLCUUID(27)},
		{28, "1d", "应用-指标（天级）", "flow_metrics.application*", 1, 27, "应用-指标（小时级）", 86400, 720, 0, "Sum", "Avg", true, "2025-09-14 16:05:07", dataSourceLCUUID(28)},
		{29, "1s", "事件-文件读写指标", "event.file_event_metrics", 1, 0, "", 1, 48, 0, "Sum", "Avg", true, "2025-09-14 16:05:14", dataSourceLCUUID(29)},
		{30, "1s", "应用-性能剖析指标", "profile.in_process_metrics", 1, 0, "", 1, 48, 0, "Sum", "Avg", true, "2025-09-14 16:05:20", dataSourceLCUUID(30)},
		{31, "event.file_agg_event", "事件-文件读写聚合事件", "event.file_agg_event", 1, 0, "", 0, 168, 0, "", "", true, "2026-05-12 18:40:53", dataSourceLCUUID(31)},
		{32, "event.file_mgmt_event", "事件-文件管理事件", "event.file_mgmt_event", 1, 0, "", 0, 168, 0, "", "", true, "2026-05-12 18:40:53", dataSourceLCUUID(32)},
		{33, "event.proc_perm_event", "事件-进程权限事件", "event.proc_perm_event", 1, 0, "", 0, 168, 0, "", "", true, "2026-05-12 18:40:53", dataSourceLCUUID(33)},
		{34, "event.proc_ops_event", "事件-进程操作事件", "event.proc_ops_event", 1, 0, "", 0, 168, 0, "", "", true, "2026-05-12 18:40:53", dataSourceLCUUID(34)},
		{35, "event.proc_block_event", "事件-进程阻断事件", "event.proc_block_event", 1, 0, "", 0, 168, 0, "", "", true, "2026-07-03 11:05:19", dataSourceLCUUID(35)},
	}

	return ds
}

// dataSourceLCUUID generates a deterministic UUID v4 for a data source.
func dataSourceLCUUID(id int) string {
	// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	// Use the ID as a seed for deterministic output.
	h := uint64(id) * 0x9e3779b97f4a7c15
	h = h ^ (h >> 30) * 0xbf58476d1ce4e5b9
	h = h ^ (h >> 27) * 0x94d049bb133111eb
	h = h ^ (h >> 31)
	return fmt.Sprintf("%08x-%04x-4%03x-%04x-%012x",
		h&0xFFFFFFFF, (h>>32)&0xFFFF,
		(h>>48)&0x0FFF,
		0x8000|((h>>40)&0x3FFF),
		(h>>16)&0xFFFFFFFFFFFF,
	)
}

// ---------------------------------------------------------------------------
// Fallback for other deepflow-server resource paths
// ---------------------------------------------------------------------------

func handleResourceFallback(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, []interface{}{})
}

func getStrField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}
