package dbdesc

import "deeptrace-backend/clickhouse"

// ShowMetricsFunctions returns the hardcoded metric function list
// matching the cloud ShowMetricsFunctions API response.
func ShowMetricsFunctions() []map[string]interface{} {
	return []map[string]interface{}{
		{"name": "Avg", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "AAvg", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Sum", "type": 1, "support_metric_types": []int{1}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Max", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Min", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Percentile", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 1, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "PercentileExact", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 1, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Spread", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Rspread", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Stddev", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Apdex", "type": 1, "support_metric_types": []int{3}, "unit_overwrite": "%", "additional_param_count": 1, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Uniq", "type": 1, "support_metric_types": []int{6}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": false, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "UniqExact", "type": 1, "support_metric_types": []int{6}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": false, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Percentage", "type": 3, "unit_overwrite": "%", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "PerSecond", "type": 3, "unit_overwrite": "$unit/s", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Histogram", "type": 3, "unit_overwrite": "", "additional_param_count": 1, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Last", "type": 1, "support_metric_types": []int{1, 2, 3, 4, 5, 9}, "unit_overwrite": "", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Count", "type": 1, "support_metric_types": []int{8}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": true, "value_type": "Number", "_querier_region": clickhouse.QuerierRegion},
		{"name": "TopK", "type": 1, "support_metric_types": []int{6}, "unit_overwrite": "$unit", "additional_param_count": 1, "is_support_other_operators": false, "value_type": "String", "_querier_region": clickhouse.QuerierRegion},
		{"name": "Any", "type": 1, "support_metric_types": []int{6}, "unit_overwrite": "$unit", "additional_param_count": 0, "is_support_other_operators": false, "value_type": "String", "_querier_region": clickhouse.QuerierRegion},
	}
}

// FallbackDatabases returns the hardcoded ShowDatabases data used when
// ClickHouse is unavailable.
func FallbackDatabases() []map[string]interface{} {
	return []map[string]interface{}{
		{"name": "flow_metrics", "datasources": []string{"1m", "1s"}},
		{"name": "flow_log", "datasources": []string{"l4_flow_log", "l7_flow_log"}},
		{"name": "event", "datasources": []string{"perf_event", "alarm_event"}},
	}
}

// FallbackTables returns the hardcoded ShowTables data used when
// ClickHouse is unavailable.
func FallbackTables() []map[string]interface{} {
	return []map[string]interface{}{
		{"name": "vtap_app_port", "datasources": []string{"1m", "1s"}},
		{"name": "vtap_flow_port", "datasources": []string{"1m", "1s"}},
		{"name": "application_map", "datasources": []string{"1m"}},
		{"name": "network_map", "datasources": []string{"1m"}},
	}
}

// FallbackShowTag returns the hardcoded ShowTag data used when
// ClickHouse is unavailable.
func FallbackShowTag() []map[string]interface{} {
	return []map[string]interface{}{
		{"name": "auto_service", "display_name": "服务", "type": "resource"},
		{"name": "auto_instance", "display_name": "实例", "type": "resource"},
		{"name": "ip", "display_name": "IP地址", "type": "resource"},
		{"name": "protocol", "display_name": "协议", "type": "int_enum"},
		{"name": "is_internet", "display_name": "网络类型", "type": "int_enum"},
		{
			"name": "role", "client_name": "role", "server_name": "role",
			"display_name": "角色", "display_name_zh": "角色", "display_name_en": "Role",
			"type": "int_enum", "category": "Capture Info",
			"operators":   []string{"=", "!=", "IN", "NOT IN", ">=", "<="},
			"permissions": []bool{true, true, true},
			"description": "", "description_zh": "", "description_en": "",
			"related_tag": "", "deprecated": false,
			"not_supported_operators": []string{}, "table": "",
		},
		{"name": "response_status", "display_name": "响应状态", "type": "int_enum"},
		{"name": "observation_point", "display_name": "观测点", "type": "resource"},
		{"name": "server_port", "display_name": "服务端端口", "type": "resource"},
	}
}
