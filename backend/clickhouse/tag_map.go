package clickhouse

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Unified tag → ClickHouse expression mapping, routed by database.
//
// flow_log tables use the canonical map (per-side _0/_1 columns with
// dictionary name resolution, see column_map.go).
// flow_metrics tables use single-sided physical columns; virtual tags
// (chost/vpc/pod_*/region/az/auto_service) are resolved through the
// flow_tag dictionaries — each entry below was verified against the live
// ClickHouse before being added.
// ---------------------------------------------------------------------------

// flowMetricsTagMap maps virtual flow_metrics tags to name-resolving
// expressions. Entries are only added for tags whose dictionary exists
// (flow_tag.<x>_map) and whose referenced physical column is present in
// the flow_metrics tables.
var flowMetricsTagMap = map[string]string{
	"chost":         `dictGetOrDefault('flow_tag.chost_map', 'name', toUInt64(l3_device_id), '')`,
	"vpc":           `dictGetOrDefault('flow_tag.l3_epc_map', 'name', toUInt64(l3_epc_id), '')`,
	"pod_group":     `dictGetOrDefault('flow_tag.pod_group_map', 'name', toUInt64(pod_group_id), '')`,
	"pod_cluster":   `dictGetOrDefault('flow_tag.pod_cluster_map', 'name', toUInt64(pod_cluster_id), '')`,
	"pod_ns":        `dictGetOrDefault('flow_tag.pod_ns_map', 'name', toUInt64(pod_ns_id), '')`,
	"pod_node":      `dictGetOrDefault('flow_tag.pod_node_map', 'name', toUInt64(pod_node_id), '')`,
	"region":        `dictGetOrDefault('flow_tag.region_map', 'name', toUInt64(region_id), '')`,
	"az":            `dictGetOrDefault('flow_tag.az_map', 'name', toUInt64(az_id), '')`,
	"auto_instance": "app_instance", // application.* carries a string column
}

// profileTagMap maps profile.in_process virtual tags to name-resolving
// expressions. The table carries the same physical ID columns as the
// flow_metrics family (pod_id/pod_node_id/l3_epc_id/...), so the flow_metrics
// maps above already cover most tags — these two entries resolve the tags
// whose dictionaries differ for profile data (pod_map, gprocess_map).
// Both dictionaries verified present in flow_tag.
var profileTagMap = map[string]string{
	"pod":      `dictGetOrDefault('flow_tag.pod_map', 'name', toUInt64(pod_id), '')`,
	"gprocess": `dictGetOrDefault('flow_tag.gprocess_map', 'name', toUInt64(gprocess_id), '')`,
}

// flowMetricsIDMap maps virtual tags to their physical ID column (used for
// GROUP BY and WHERE). Bare physical columns only — no expressions.
var flowMetricsIDMap = map[string]string{
	"chost":            "l3_device_id",
	"chost_id":         "l3_device_id",
	"vpc":              "l3_epc_id",
	"vpc_id":           "l3_epc_id",
	"pod_group":        "pod_group_id",
	"pod_group_id":     "pod_group_id",
	"pod_cluster":      "pod_cluster_id",
	"pod_cluster_id":   "pod_cluster_id",
	"pod_ns":           "pod_ns_id",
	"pod_ns_id":        "pod_ns_id",
	"pod_node":         "pod_node_id",
	"pod_node_id":      "pod_node_id",
	"pod":              "pod_id",
	"pod_id":           "pod_id",
	"region":           "region_id",
	"region_id":        "region_id",
	"az":               "az_id",
	"az_id":            "az_id",
	"auto_service":     "auto_service_id",
	"auto_service_id":  "auto_service_id",
	"auto_instance":    "auto_instance_id",
	"auto_instance_id": "auto_instance_id",
	"gprocess":         "gprocess_id",
	"gprocess_id":      "gprocess_id",
	"host":             "host_id",
	"host_id":          "host_id",
	"service":          "service_id",
	"service_id":       "service_id",
	"subnet":           "subnet_id",
	"subnet_id":        "subnet_id",
}

// flowMetricsSideMap maps _0/_1-suffixed virtual tags (Top client/server
// view) to the shared single-sided flow_metrics column. flow_metrics tables
// have no per-side columns — both sides collapse onto the same column.
// Entries referencing columns that don't exist in the tables (pod_service_id,
// biz_service_id) are intentionally absent: the column pre-check sends those
// queries to the cache fallback.
var flowMetricsSideMap = map[string]string{
	"auto_service_0":  "", // table-family dependent (see sideServiceExpr)
	"auto_service_1":  "",
	"auto_instance_0": "app_instance",
	"auto_instance_1": "app_instance",
	"chost_0":         "l3_device_id",
	"chost_1":         "l3_device_id",
	"region_0":        "region_id",
	"region_1":        "region_id",
	"az_0":            "az_id",
	"az_1":            "az_id",
	"subnet_0":        "subnet_id",
	"subnet_1":        "subnet_id",
	"vpc_0":           "l3_epc_id",
	"vpc_1":           "l3_epc_id",
	"pod_ns_0":        "pod_ns_id",
	"pod_ns_1":        "pod_ns_id",
	"pod_cluster_0":   "pod_cluster_id",
	"pod_cluster_1":   "pod_cluster_id",
	"pod_group_0":     "pod_group_id",
	"pod_group_1":     "pod_group_id",
	"pod_node_0":      "pod_node_id",
	"pod_node_1":      "pod_node_id",
	"service_0":       "service_id",
	"service_1":       "service_id",
	// is_internet_0/1: computed from the IP columns (the flow_metrics tables
	// carry ip4_0/ip6_0/is_ipv4 but no is_internet_* column). Same CIDR logic
	// as the flow_log canonical map. $any() wraps the non-grouped columns:
	// ExpandAny(true) → any(...) for SELECT, ExpandAny(false) → bare for
	// GROUP BY.
	"is_internet_0": `$any(if(is_ipv4 = 1 AND (isIPAddressInRange(IPv4NumToString(ip4_0), '10.0.0.0/8') OR isIPAddressInRange(IPv4NumToString(ip4_0), '172.16.0.0/12') OR isIPAddressInRange(IPv4NumToString(ip4_0), '192.168.0.0/16') OR isIPAddressInRange(IPv4NumToString(ip4_0), '127.0.0.0/8') OR isIPAddressInRange(IPv4NumToString(ip4_0), '100.64.0.0/10') OR isIPAddressInRange(IPv4NumToString(ip4_0), '169.254.0.0/16')), 0, 1))`,
	"is_internet_1": `$any(if(is_ipv4 = 1 AND (isIPAddressInRange(IPv4NumToString(ip4_1), '10.0.0.0/8') OR isIPAddressInRange(IPv4NumToString(ip4_1), '172.16.0.0/12') OR isIPAddressInRange(IPv4NumToString(ip4_1), '192.168.0.0/16') OR isIPAddressInRange(IPv4NumToString(ip4_1), '127.0.0.0/8') OR isIPAddressInRange(IPv4NumToString(ip4_1), '100.64.0.0/10') OR isIPAddressInRange(IPv4NumToString(ip4_1), '169.254.0.0/16')), 0, 1))`,
}

// sideServiceExpr resolves auto_service_0/1 for a flow_metrics table.
// side is "0" or "1": network-family tables carry per-side ID columns
// (auto_service_type_0 / auto_service_id_0 — verified against system.columns);
// the application family has a single app_service string column.
// Types 0 (internet) and 255 (IP) have no dictionary entry — display the
// peer IP instead of the raw id (matches the flow_log canonical map).
func sideServiceExpr(table, side string) string {
	if flowMetricsTableFamily(table) == "application" {
		return "app_service"
	}
	return fmt.Sprintf("if(auto_service_type_%s IN (0, 255), if($any(is_ipv4) = 1, IPv4NumToString($any(ip4_%s)), IPv6NumToString($any(ip6_%s))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_%s), toUInt64(auto_service_id_%s)), toString(auto_service_id_%s)))", side, side, side, side, side, side)
}

// TagSideExpr resolves a _0/_1-suffixed tag for Top client/server views.
// Returns the SELECT-side expression: $any(...) placeholders become any(...)
// so non-grouped columns (is_ipv4, ip4_0) aggregate correctly. "" for
// unknown or unsupported tags (pre-check falls through).
func TagSideExpr(db, table, tag string) string {
	if db != "flow_metrics" {
		return ""
	}
	var expr string
	if tag == "auto_service_0" || tag == "auto_service_1" {
		expr = sideServiceExpr(table, strings.TrimPrefix(tag, "auto_service_"))
	} else {
		expr = flowMetricsSideMap[tag]
	}
	if expr == "" {
		return ""
	}
	return ExpandAny(expr, true)
}

// TagSideGroupExpr resolves the GROUP BY key for a _0/_1-suffixed tag:
// the bare (non-aggregated) form of the expression. "" when the tag groups
// on a plain column (callers fall back to TagIDExpr / TagGroupExpr).
func TagSideGroupExpr(db, table, tag string) string {
	if db != "flow_metrics" {
		return ""
	}
	var expr string
	if tag == "auto_service_0" || tag == "auto_service_1" {
		expr = sideServiceExpr(table, strings.TrimPrefix(tag, "auto_service_"))
	} else {
		expr = flowMetricsSideMap[tag]
	}
	if expr == "" || !strings.Contains(expr, "$any(") {
		return ""
	}
	return ExpandAny(expr, false)
}

// flowMetricsTableFamily extracts "application" from "application.1m".
func flowMetricsTableFamily(table string) string {
	if i := strings.Index(table, "."); i >= 0 {
		return table[:i]
	}
	return table
}

// autoServiceExpr resolves the auto_service name expression for a
// flow_metrics table: application.* carries a string app_service column,
// network.* (and others) must resolve via the device_map dictionary
// (composite key on type+id; grouping via TagGroupExpr).
// Types 0 (internet) and 255 (IP) have no device_map entry — display the
// peer IP instead of the raw id (same semantics as the per-side
// sideServiceExpr; $any() wraps the non-grouped IP columns, expanded by
// the builder's ExpandAny on the SELECT/GROUP BY paths).
func autoServiceExpr(table string) string {
	if flowMetricsTableFamily(table) == "application" {
		// application.* carries an app_service string column, but traffic
		// without service attribution (type 0/255) leaves it empty — fall
		// back to the peer IP like the network family (verified: type=255
		// rows carry real IPs in ip4/ip6).
		return "if(auto_service_type IN (0, 255), if($any(is_ipv4) = 1, IPv4NumToString($any(ip4)), IPv6NumToString($any(ip6))), app_service)"
	}
	return "if(auto_service_type IN (0, 255), if($any(is_ipv4) = 1, IPv4NumToString($any(ip4)), IPv6NumToString($any(ip6))), dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type), toUInt64(auto_service_id)), toString(auto_service_id)))"
}

// TagExpr resolves a DeepFlow tag name to a ClickHouse name-resolving
// expression for the given database/table. Returns "" for unknown tags
// (the caller falls back to a bare column reference).
func TagExpr(db, table, tag string) string {
	switch db {
	case "flow_metrics":
		if tag == "auto_service" {
			return autoServiceExpr(table)
		}
		if expr, ok := flowMetricsTagMap[tag]; ok {
			return expr
		}
		// ID-only tags (chost_id, vpc_id, ...) select their physical column.
		if col, ok := flowMetricsIDMap[tag]; ok {
			return col
		}
		return ""
	case "profile":
		// profile.in_process carries the flow_metrics physical ID columns
		// (pod_id/pod_node_id/l3_epc_id/...); virtual tags resolve through
		// the flow_tag dictionaries, with profile-specific entries first.
		if expr, ok := profileTagMap[tag]; ok {
			return expr
		}
		if expr, ok := flowMetricsTagMap[tag]; ok {
			return expr
		}
		if col, ok := flowMetricsIDMap[tag]; ok {
			return col
		}
		return ""
	default:
		// flow_log: canonical map with $any placeholders replaced.
		return ColumnExpr(tag, false)
	}
}

// TagGroupExpr returns the GROUP BY column list required by a tag's
// name-resolving expression. The device_map dictionary uses a composite
// key (type, id), so network auto_service must group on both columns
// (per-side columns for _0/_1 tags — the network-family tables carry
// auto_service_id_0/auto_service_type_0, no unsuffixed columns).
// Returns "" when the tag groups on its plain ID column only.
func TagGroupExpr(db, table, tag string) string {
	if db != "flow_metrics" && db != "profile" {
		return ""
	}
	if flowMetricsTableFamily(table) == "application" {
		return ""
	}
	switch tag {
	case "auto_service":
		return "auto_service_id, auto_service_type"
	case "auto_service_0":
		return "auto_service_id_0, auto_service_type_0"
	case "auto_service_1":
		return "auto_service_id_1, auto_service_type_1"
	}
	return ""
}

// nodeTypeExpr resolves a node_type_map dictionary lookup and renames the
// internet type "internet_ip" to "ip". The cloud frontend's knowledge-graph
// query builder (Line-*.js o()) only handles node types "ip"/"auto_service"
// — for "internet_ip" it falls back to an empty condition array and crashes
// on c[0].key. Renaming routes the node into the ip branch (whose condition
// carries an ip key). Verified: type 0 rows previously crashed the graph tab;
// after the rename the graph opens.
func nodeTypeExpr(side string) string {
	expr := fmt.Sprintf("dictGetOrDefault('flow_tag.node_type_map', 'node_type', toUInt64(auto_service_type%s), '')", side)
	return fmt.Sprintf("replaceRegexpOne(%s, 'internet_ip', 'ip')", expr)
}

// selectHasColumn reports whether an aliased column already appears in a
// SELECT part list (used by the auto-add logic to avoid duplicates).
func selectHasColumn(parts []string, col string) bool {
	for _, p := range parts {
		if strings.Contains(p, "`"+col+"`") {
			return true
		}
	}
	return false
}

// autoServiceSide returns the physical column suffix ("", "_0", "_1") for an
// auto_service resolution target (node_type/icon_id dictionaries), and whether
// the tag is an auto_service variant at all.
func autoServiceSide(tag string) (string, bool) {
	switch tag {
	case "auto_service":
		return "", true
	case "auto_service_0", "auto_service_1":
		return tag[len(tag)-2:], true
	}
	return "", false
}

// TagIDExpr resolves the physical ID column for a tag in the given
// database/table. Used for GROUP BY, WHERE and fast_list conditions.
// Returns the tag itself when no mapping exists.
func TagIDExpr(db, table, tag string) string {
	if db != "flow_metrics" && db != "profile" {
		return IDColumn(tag)
	}
	if tag == "auto_service" {
		// application.* carries a string app_service column — grouping must
		// use it directly; other families resolve via auto_service_id.
		if flowMetricsTableFamily(table) == "application" {
			return "app_service"
		}
		return "auto_service_id"
	}
	if tag == "auto_service_0" || tag == "auto_service_1" {
		if flowMetricsTableFamily(table) == "application" {
			return "app_service"
		}
		return "auto_service_id_" + tag[len(tag)-1:]
	}
	if col, ok := flowMetricsIDMap[tag]; ok {
		return col
	}
	return tag
}
