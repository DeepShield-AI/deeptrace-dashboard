package clickhouse

import "strings"

// CanonicalColumnMap defines the authoritative mapping from ZT tag names to
// ClickHouse column names. All query paths (Top, List, fast_list) reference
// this single source of truth.
//
// Two modes:
//   Grouped = true  → wraps in any() for GROUP BY queries (Top queries)
//   Grouped = false → bare column reference (List queries, WHERE conditions)

// ColumnExpr returns the ClickHouse expression for a given ZT tag name.
// When grouped is true, $any(...) placeholders become any(...) for GROUP BY
// queries (Top); when grouped is false they are stripped (bare columns).
// Returns "" for unknown tags.
func ColumnExpr(tag string, grouped bool) string {
	expr, ok := canonicalMap[tag]
	if !ok {
		return ""
	}
	return ExpandAny(expr, grouped)
}

// ExpandAny expands $any(...) placeholders in an expression:
// grouped=true  → any(<inner>) for the SELECT side of a GROUP BY query;
// grouped=false → <inner> bare for GROUP BY keys / WHERE conditions.
// Shared by canonicalMap (flow_log) and flowMetricsSideMap expressions.
func ExpandAny(expr string, grouped bool) string {
	var b strings.Builder
	i := 0
	for i < len(expr) {
		idx := strings.Index(expr[i:], "$any(")
		if idx < 0 {
			b.WriteString(expr[i:])
			break
		}
		idx += i
		b.WriteString(expr[i:idx])
		// Find the matching closing paren (supports nesting).
		depth := 1
		j := idx + len("$any(")
		for j < len(expr) && depth > 0 {
			switch expr[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
			j++
		}
		inner := expr[idx+len("$any(") : j-1]
		if grouped {
			b.WriteString("any(")
			b.WriteString(inner)
			b.WriteString(")")
		} else {
			b.WriteString(inner)
		}
		i = j
	}
	return b.String()
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
	"region_0":      `$any(dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id_0), ''))`,
	"region_1":      `$any(dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id_1), ''))`,
	"az_0":          `$any(dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id_0), ''))`,
	"az_1":          `$any(dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id_1), ''))`,
	"chost_0":       `$any(dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id_0), ''))`,
	"chost_1":       `$any(dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id_1), ''))`,
	"vpc_0":         `$any(dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(epc_id_0), ''))`,
	"vpc_1":         `$any(dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(epc_id_1), ''))`,
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

	// --- k8s annotations/envs (builder.go flow_log path) ---
	"k8s.annotation_0": "dictGetOrDefault('flow_tag.pod_service_k8s_annotations_map', 'annotations', toUInt64(pod_service_id_0), '')",
	"k8s.annotation_1": "dictGetOrDefault('flow_tag.pod_service_k8s_annotations_map', 'annotations', toUInt64(pod_service_id_1), '')",
	"k8s.env_0":        "dictGetOrDefault('flow_tag.pod_k8s_envs_map', 'envs', toUInt64(pod_id_0), '')",
	"k8s.env_1":        "dictGetOrDefault('flow_tag.pod_k8s_envs_map', 'envs', toUInt64(pod_id_1), '')",

	// --- Raw ID columns (no dictGet) ---
	"chost_id_0": "l3_device_id_0", "chost_id_1": "l3_device_id_1",
	"vpc_id_0": "epc_id_0", "vpc_id_1": "epc_id_1",
	"subnet_0": "subnet_id_0", "subnet_1": "subnet_id_1",
	"router_0": "router_id_0", "router_1": "router_id_1",
	"l2_vpc_0": "epc_id_0", "l2_vpc_1": "epc_id_1",
	"lb_0": "lb_id_0", "lb_1": "lb_id_1",
	"lb_listener_0": "lb_listener_id_0", "lb_listener_1": "lb_listener_id_1",
	"pod_ingress_0": "pod_ingress_id_0", "pod_ingress_1": "pod_ingress_id_1",
	"pod_0": "pod_id_0", "pod_1": "pod_id_1",
	"gprocess_0": "gprocess_id_0", "gprocess_1": "gprocess_id_1",
	"process_0": "process_id_0", "process_1": "process_id_1",
	"x_request_0": "x_request_id_0", "x_request_1": "x_request_id_1",
	"tap_port": "tap_port", "vtap": "vtap_id", "agent": "agent_id",
	"role": "0",

	// --- Computed virtual columns ---
	"client_node_type": "auto_service_type_0",
	"server_node_type": "auto_service_type_1",
	"event_type":       "l7_protocol",
	"event_desc":       "request_resource",
	"auto_instance":    "if(empty(app_instance), toString(auto_instance_id_0), app_instance)",
	"app_service":      "app_service",
	"service_id_0":     "auto_service_id_0",
	"service_id_1":     "auto_service_id_1",
	"instance_id_0":    "auto_instance_id_0",
	"instance_id_1":    "auto_instance_id_1",
	// Private ranges per RFC 1918 + loopback + CGNAT (100.64/10) +
	// link-local (169.254/16). isIPAddressInRange does proper CIDR math —
	// the old startsWith('172.1') prefixes misclassified 172.10.0.0–172.15.255.255
	// (public) as private.
	"is_internet_0": `$any(if(is_ipv4 = 1 AND (isIPAddressInRange(IPv4NumToString(ip4_0), '10.0.0.0/8') OR isIPAddressInRange(IPv4NumToString(ip4_0), '172.16.0.0/12') OR isIPAddressInRange(IPv4NumToString(ip4_0), '192.168.0.0/16') OR isIPAddressInRange(IPv4NumToString(ip4_0), '127.0.0.0/8') OR isIPAddressInRange(IPv4NumToString(ip4_0), '100.64.0.0/10') OR isIPAddressInRange(IPv4NumToString(ip4_0), '169.254.0.0/16')), 0, 1))`,
	"is_internet_1": `$any(if(is_ipv4 = 1 AND (isIPAddressInRange(IPv4NumToString(ip4_1), '10.0.0.0/8') OR isIPAddressInRange(IPv4NumToString(ip4_1), '172.16.0.0/12') OR isIPAddressInRange(IPv4NumToString(ip4_1), '192.168.0.0/16') OR isIPAddressInRange(IPv4NumToString(ip4_1), '127.0.0.0/8') OR isIPAddressInRange(IPv4NumToString(ip4_1), '100.64.0.0/10') OR isIPAddressInRange(IPv4NumToString(ip4_1), '169.254.0.0/16')), 0, 1))`,
}

// idColumnMap maps virtual tag names to raw ClickHouse ID columns.
// Used by fast_list WHERE conditions and SELECT mapping.
// NOTE: _0/_1 entries map to per-side ID columns (flow_log); service uses
// service_id_* per the fast_list behavior (not biz_service_id).
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

	// Per-side ID columns (flow_log fast_list / GROUP BY).
	"region_0": "region_id_0", "region_1": "region_id_1",
	"az_0": "az_id_0", "az_1": "az_id_1",
	"chost_0": "l3_device_id_0", "chost_1": "l3_device_id_1",
	"chost_id_0": "l3_device_id_0", "chost_id_1": "l3_device_id_1",
	"vpc_0": "epc_id_0", "vpc_1": "epc_id_1",
	"subnet_0": "subnet_id_0", "subnet_1": "subnet_id_1",
	"router_0": "router_id_0", "router_1": "router_id_1",
	"lb_0": "lb_id_0", "lb_1": "lb_id_1",
	"pod_0": "pod_id_0", "pod_1": "pod_id_1",
	"pod_ns_0": "pod_ns_id_0", "pod_ns_1": "pod_ns_id_1",
	"pod_node_0": "pod_node_id_0", "pod_node_1": "pod_node_id_1",
	"pod_cluster_0": "pod_cluster_id_0", "pod_cluster_1": "pod_cluster_id_1",
	"pod_service_0": "pod_service_id_0", "pod_service_1": "pod_service_id_1",
	"pod_group_0": "pod_group_id_0", "pod_group_1": "pod_group_id_1",
	"gprocess_0": "gprocess_id_0", "gprocess_1": "gprocess_id_1",
	"service_0": "service_id_0", "service_1": "service_id_1",
	"auto_service_0": "auto_service_id_0", "auto_service_1": "auto_service_id_1",
	"auto_instance_0": "auto_instance_id_0", "auto_instance_1": "auto_instance_id_1",
	"k8s.label_0": "pod_id_0", "k8s.label_1": "pod_id_1",
	"cloud.tag_0": "l3_device_id_0", "cloud.tag_1": "l3_device_id_1",
	"os.app_0": "gprocess_id_0", "os.app_1": "gprocess_id_1",
	"l2_vpc_0": "epc_id_0", "l2_vpc_1": "epc_id_1",
	"lb_listener_0": "lb_listener_id_0", "lb_listener_1": "lb_listener_id_1",
	"pod_ingress_0": "pod_ingress_id_0", "pod_ingress_1": "pod_ingress_id_1",
	"process_0": "process_id_0", "process_1": "process_id_1",
	"x_request_0": "x_request_id_0", "x_request_1": "x_request_id_1",
	"tap_port": "tap_port", "vtap": "vtap_id", "agent": "agent_id",
}

// FastListSkipCols lists virtual columns to skip in WHERE conditions.
var FastListSkipCols = map[string]struct{}{
	"role":          {},
	"is_internet":   {},
	"is_internet_0": {},
	"is_internet_1": {},
}
