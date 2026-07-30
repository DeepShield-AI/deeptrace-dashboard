package clickhouse

// CanonicalColumnMap defines the authoritative mapping from ZT tag names to
// ClickHouse column names. All query paths (Top, List, fast_list) reference
// this single source of truth.
//
// Two modes:
//   Grouped = true  → wraps in any() for GROUP BY queries (Top queries)
//   Grouped = false → bare column reference (List queries, WHERE conditions)

// ColumnExpr returns the ClickHouse expression for a given ZT tag name.
// When grouped is true, non-aggregate columns are wrapped in any().
func ColumnExpr(tag string, grouped bool) string {
	return canonicalMap[tag]
}

// IDColumn returns the raw ClickHouse ID column for a virtual tag name.
// Used by fast_list WHERE conditions and similar non-GROUP BY contexts.
func IDColumn(tag string) string {
	if col, ok := idColumnMap[tag]; ok {
		return col
	}
	return tag
}

// canonicalMap maps ZT tag names to full ClickHouse SQL expressions.
// These replace the duplicated flowLogColMap and colMap from top.go/builder.go.
var canonicalMap = map[string]string{
	// --- auto_service / auto_instance (name resolution) ---
	"auto_service_0":  `if(auto_service_type_0 IN (0, 255), if($any(is_ipv4) = 1, IPv4NumToString($any(ip4_0)), IPv6NumToString($any(ip6_0))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_0), toUInt64($any(auto_service_id_0))), ''))`,
	"auto_service_1":  `if(auto_service_type_1 IN (0, 255), if($any(is_ipv4) = 1, IPv4NumToString($any(ip4_1)), IPv6NumToString($any(ip6_1))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_1), toUInt64($any(auto_service_id_1))), ''))`,
	"auto_instance_0": `if(auto_instance_type_0 IN (0, 255), if($any(is_ipv4) = 1, IPv4NumToString($any(ip4_0)), IPv6NumToString($any(ip6_0))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_instance_type_0), toUInt64($any(auto_instance_id_0))), toString(auto_instance_id_0)))`,
	"auto_instance_1": `if(auto_instance_type_1 IN (0, 255), if($any(is_ipv4) = 1, IPv4NumToString($any(ip4_1)), IPv6NumToString($any(ip6_1))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_instance_type_1), toUInt64($any(auto_instance_id_1))), toString(auto_instance_id_1)))`,

	// --- Resource tags: dictGet name resolution ---
	"region_0": `$any(dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id_0), ''))`,
	"region_1": `$any(dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id_1), ''))`,
	"az_0":     `$any(dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id_0), ''))`,
	"az_1":     `$any(dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id_1), ''))`,
	"chost_0":  `$any(dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id_0), ''))`,
	"chost_1":  `$any(dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id_1), ''))`,
	"vpc_0":    `$any(dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(epc_id_0), ''))`,
	"vpc_1":    `$any(dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(epc_id_1), ''))`,
	"pod_ns_0":      `$any(dictGetOrDefault('flow_tag.pod_ns_map', 'name', toUInt64(pod_ns_id_0), ''))`,
	"pod_ns_1":      `$any(dictGetOrDefault('flow_tag.pod_ns_map', 'name', toUInt64(pod_ns_id_1), ''))`,
	"pod_cluster_0": `$any(dictGetOrDefault('flow_tag.pod_cluster_map', 'name', toUInt64(pod_cluster_id_0), ''))`,
	"pod_cluster_1": `$any(dictGetOrDefault('flow_tag.pod_cluster_map', 'name', toUInt64(pod_cluster_id_1), ''))`,
	"pod_service_0": `$any(dictGetOrDefault('flow_tag.pod_service_map', 'name', toUInt64(pod_service_id_0), ''))`,
	"pod_service_1": `$any(dictGetOrDefault('flow_tag.pod_service_map', 'name', toUInt64(pod_service_id_1), ''))`,
	"pod_group_0":   `$any(dictGetOrDefault('flow_tag.pod_group_map', 'name', toUInt64(pod_group_id_0), ''))`,
	"pod_group_1":   `$any(dictGetOrDefault('flow_tag.pod_group_map', 'name', toUInt64(pod_group_id_1), ''))`,
	"pod_node_0":    `$any(dictGetOrDefault('flow_tag.pod_node_map', 'name', toUInt64(pod_node_id_0), ''))`,
	"pod_node_1":    `$any(dictGetOrDefault('flow_tag.pod_node_map', 'name', toUInt64(pod_node_id_1), ''))`,
	"service_0":     `$any(dictGetOrDefault('flow_tag.biz_service_map', 'name', toUInt64(biz_service_id_0), ''))`,
	"service_1":     `$any(dictGetOrDefault('flow_tag.biz_service_map', 'name', toUInt64(biz_service_id_1), ''))`,

	// --- k8s / cloud / os tags ---
	"k8s.label_0": `$any(dictGetOrDefault('flow_tag.pod_k8s_labels_map', 'labels', toUInt64(pod_id_0), ''))`,
	"k8s.label_1": `$any(dictGetOrDefault('flow_tag.pod_k8s_labels_map', 'labels', toUInt64(pod_id_1), ''))`,
	"cloud.tag_0": `$any(dictGetOrDefault('flow_tag.chost_cloud_tags_map', 'cloud_tags', toUInt64(l3_device_id_0), ''))`,
	"cloud.tag_1": `$any(dictGetOrDefault('flow_tag.chost_cloud_tags_map', 'cloud_tags', toUInt64(l3_device_id_1), ''))`,
	"os.app_0":    `$any(dictGetOrDefault('flow_tag.os_app_tags_map', 'os_app_tags', toUInt64(gprocess_id_0), ''))`,
	"os.app_1":    `$any(dictGetOrDefault('flow_tag.os_app_tags_map', 'os_app_tags', toUInt64(gprocess_id_1), ''))`,

	// --- Raw ID columns (no dictGet) ---
	"chost_id_0":     "l3_device_id_0", "chost_id_1": "l3_device_id_1",
	"vpc_id_0":       "epc_id_0", "vpc_id_1": "epc_id_1",
	"subnet_0":       "subnet_id_0", "subnet_1": "subnet_id_1",
	"router_0":       "router_id_0", "router_1": "router_id_1",
	"l2_vpc_0":       "epc_id_0", "l2_vpc_1": "epc_id_1",
	"lb_0":           "lb_id_0", "lb_1": "lb_id_1",
	"lb_listener_0":  "lb_listener_id_0", "lb_listener_1": "lb_listener_id_1",
	"pod_ingress_0":  "pod_ingress_id_0", "pod_ingress_1": "pod_ingress_id_1",
	"pod_0":          "pod_id_0", "pod_1": "pod_id_1",
	"gprocess_0":     "gprocess_id_0", "gprocess_1": "gprocess_id_1",
	"process_0":      "process_id_0", "process_1": "process_id_1",
	"x_request_0":    "x_request_id_0", "x_request_1": "x_request_id_1",
	"tap_port":       "tap_port", "vtap": "vtap_id", "agent": "agent_id",
	"role":           "0",

	// --- Computed virtual columns ---
	"client_node_type": "auto_service_type_0",
	"server_node_type": "auto_service_type_1",
	"event_type":       "l7_protocol",
	"event_desc":       "request_resource",
	"auto_instance":    "if(empty(app_instance), toString(auto_instance_id_0), app_instance)",
	"app_service":      "app_service",
}

// idColumnMap maps virtual tag names to raw ClickHouse ID columns.
// Used by fast_list WHERE conditions.
var idColumnMap = map[string]string{
	"auto_service":      "auto_service_id",
	"auto_instance":     "auto_instance_id",
	"auto_service_type": "auto_service_type",
	"chost":             "l3_device_id",
	"host":              "l3_device_id",
	"vpc":               "epc_id",
	"pod_service":       "pod_service_id",
	"pod_group":         "pod_group_id",
	"pod_cluster":       "pod_cluster_id",
	"pod_ns":            "pod_ns_id",
	"pod_node":          "pod_node_id",
	"subnet":            "subnet_id",
	"router":            "router_id",
	"region":            "region_id",
	"az":                "az_id",
	"lb":                "lb_id",
	"service":           "biz_service_id",
}
