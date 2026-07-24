package engine

// ColumnMaps holds the canonical DeepFlow-DSL → ClickHouse-column mappings.
// Different database contexts use different subsets of columns.
type ColumnMaps struct {
	// Base maps shared across all contexts.
	Base map[string]string
	// FlowLog overrides/additions for flow_log databases.
	FlowLog map[string]string
	// FlowMetrics overrides/additions for flow_metrics databases.
	FlowMetrics map[string]string
	// Event overrides/additions for event databases.
	Event map[string]string
}

// CanonicalColumnMaps defines all known field mappings from the DeepFlow querier
// DSL to real ClickHouse column expressions. These are extracted from the 4+
// duplicate colMap definitions across the codebase.
var CanonicalColumnMaps = ColumnMaps{
	Base: map[string]string{
		"_id": "toString(_id)",
	},
	FlowLog: map[string]string{
		"protocol":           "l7_protocol",
		"event_desc":         "request_resource",
		"event_type":         "l7_protocol",
		"auto_service_id_0":  "auto_service_id_0",
		"auto_service_id_1":  "auto_service_id_1",
		"auto_instance_id_0": "auto_instance_id_0",
		"auto_instance_id_1": "auto_instance_id_1",
		// NOTE: auto_service_0/1 and auto_instance_0/1 are intentionally NOT mapped
		// for flow_log — they pass through as raw column names so deepflow-server
		// /v1/query/ can resolve them via ClickHouse dictionaries (device_map, etc.).
	},
	FlowMetrics: map[string]string{
		"auto_service":    "app_service",
		"auto_instance":   "app_instance",
		"auto_service_0":  "app_service",
		"auto_service_1":  "app_service",
		"auto_instance_0": "app_instance",
		"auto_instance_1": "app_instance",
	},
	Event: map[string]string{
		"event_type": "event_type",
	},
}

// GetColumnMap returns the merged column mapping for the given database name.
func GetColumnMap(db string) map[string]string {
	result := make(map[string]string, len(CanonicalColumnMaps.Base))
	for k, v := range CanonicalColumnMaps.Base {
		result[k] = v
	}
	switch db {
	case "flow_log", "l7_flow_log", "l4_flow_log":
		for k, v := range CanonicalColumnMaps.FlowLog {
			result[k] = v
		}
	case "flow_metrics":
		for k, v := range CanonicalColumnMaps.FlowMetrics {
			result[k] = v
		}
	case "event":
		for k, v := range CanonicalColumnMaps.Event {
			result[k] = v
		}
	}
	return result
}
