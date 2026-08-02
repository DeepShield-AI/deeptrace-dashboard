package dimension

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"deeptrace-backend/clickhouse"
)

type (
	SvcKey struct{ id, typ string }
	DimReq struct {
		Database  string `json:"DATABASE"`
		Table     string `json:"TABLE"`
		TimeStart int64  `json:"time_start"`
		TimeEnd   int64  `json:"time_end"`
		Region    string `json:"REGION"`
		Queries   []struct {
			Where  string `json:"WHERE"`
			Select string `json:"SELECT"`
			TOP    int    `json:"TOP"`
		} `json:"QUERIES"`
	}
)

type ResourceEntry struct {
	ID   interface{} `json:"ID"`
	Name string      `json:"NAME"`
}

// ipResourceDims maps the ip_resource_map dimension columns to their
// ATTRIBUTES keys (cloud display names for the resource dimensions).
var ipResourceDims = []struct{ idCol, nameCol, key string }{
	{"region_id", "region_name", "region"},
	{"az_id", "az_name", "az"},
	{"host_id", "host_name", "host"},
	{"chost_id", "chost_name", "chost"},
	{"l3_epc_id", "l3_epc_name", "vpc"},
	{"router_id", "router_name", "router"},
	{"dhcpgw_id", "dhcpgw_name", "dhcpgw"},
	{"lb_id", "lb_name", "lb"},
	{"natgw_id", "natgw_name", "natgw"},
	{"subnet_id", "subnet_name", "subnet"},
	{"redis_id", "redis_name", "redis"},
	{"rds_id", "rds_name", "rds"},
	{"pod_cluster_id", "pod_cluster_name", "pod_cluster"},
	{"pod_ns_id", "pod_ns_name", "pod_ns"},
	{"pod_node_id", "pod_node_name", "pod_node"},
	{"pod_group_id", "pod_group_name", "pod_group"},
	{"pod_service_id", "pod_service_name", "pod_service"},
	{"pod_id", "pod_name", "pod"},
}

// QueryIPResourceAttributes resolves the ATTRIBUTES dict for an IP through
// flow_tag.ip_resource_map (one row per IP with all its resource dimensions).
// Empty map table (ingester hasn't synced) yields an empty dict — the query
// itself is the contract; data fills in once the table is populated.
func QueryIPResourceAttributes(ch *clickhouse.CHService, ctx context.Context, ip string) map[string]interface{} {
	q := fmt.Sprintf("SELECT * FROM flow_tag.ip_resource_map WHERE ip = '%s' LIMIT 1", strings.ReplaceAll(ip, "'", "''"))
	rows, err := ch.Query(ctx, q)
	if err != nil {
		return nil
	}
	defer rows.Close()
	data, err := clickhouse.ScanRows(rows)
	if err != nil || len(data) == 0 {
		return nil
	}
	row := data[0]
	attrs := map[string]interface{}{}
	for _, d := range ipResourceDims {
		id, idOK := row[d.idCol]
		if !idOK || id == nil {
			continue
		}
		// Only include non-zero IDs (0 = dimension not set for this IP).
		switch v := id.(type) {
		case uint64:
			if v == 0 {
				continue
			}
		case float64:
			if v == 0 {
				continue
			}
		case int64:
			if v == 0 {
				continue
			}
		}
		name := GetStrDim(row, d.nameCol)
		if name == "" {
			name = fmt.Sprintf("%v", id)
		}
		attrs[d.key] = []map[string]interface{}{{"ID": id, "NAME": name}}
	}
	return attrs
}

// CloudResponse returns the dimension-resources response envelope verified
// against cloud.deepflow.yunshan.net: a synchronous task result with a
// DATA.ATTRIBUTES dict (empty when the queried service has no related
// resource attributes — the observed cloud behavior for resource-analysis).
func CloudResponse() map[string]interface{} {
	return map[string]interface{}{
		"OPT_STATUS":    "SUCCESS",
		"WAIT_CALLBACK": false,
		"TASK":          nil,
		"DESCRIPTION":   "",
		"TYPE":          "dict",
		"DATA": map[string]interface{}{
			"ATTRIBUTES": map[string]interface{}{},
			"debug":      map[string]interface{}{},
		},
	}
}

// dimEnumKey maps an ATTRIBUTES dimension key to the resource-enumeration
// response keys (verified against cloud.deepflow.yunshan.net: DATA is a
// resource enumeration dict — REGION_NUM/REGIONS/AZ_NUM/AZS/... — not an
// ATTRIBUTES wrapper).
var dimEnumKey = map[string][2]string{
	"region":      {"REGIONS", "REGION_NUM"},
	"az":          {"AZS", "AZ_NUM"},
	"host":        {"HOSTS", "HOST_NUM"},
	"chost":       {"CHOSTS", "CHOST_NUM"},
	"vpc":         {"VPCS", "VPC_NUM"},
	"router":      {"ROUTERS", "ROUTER_NUM"},
	"dhcpgw":      {"DHCPGWS", "DHCPGW_NUM"},
	"lb":          {"LBS", "LB_NUM"},
	"natgw":       {"NATGWS", "NATGW_NUM"},
	"subnet":      {"SUBNETS", "SUBNET_NUM"},
	"redis":       {"REDISES", "REDIS_NUM"},
	"rds":         {"RDSES", "RDS_NUM"},
	"pod_cluster": {"POD_CLUSTERS", "POD_CLUSTER_NUM"},
	"pod_ns":      {"POD_NSES", "POD_NS_NUM"},
	"pod_node":    {"POD_NODES", "POD_NODE_NUM"},
	"pod_group":   {"POD_GROUPS", "POD_GROUP_NUM"},
	"pod_service": {"POD_SERVICES", "POD_SERVICE_NUM"},
	"pod":         {"PODS", "POD_NUM"},
}

// QueryDimensionResources resolves the resource-enumeration dict for a
// dimension-resources request. Response contract (verified against
// cloud.deepflow.yunshan.net): DATA is a resource enumeration —
// {"REGION_NUM":1,"REGIONS":[{ID,NAME}],"AZ_NUM":...,"CHOSTS":[...],...}.
// IP conditions resolve through flow_tag.ip_resource_map; service
// conditions enumerate the flow_tag dictionaries.
func QueryDimensionResources(ch *clickhouse.CHService, req DimReq) map[string]interface{} {
	result := EmptyDimensionResult()
	if ch == nil || !ch.Enabled() {
		return result
	}

	ctx := context.Background()

	// IP condition → flow_tag.ip_resource_map row (per-IP dimensions).
	if ip := ExtractIPFromWhere(req.Queries); ip != "" {
		attrs := QueryIPResourceAttributes(ch, ctx, ip)
		for dim, entries := range attrs {
			if keys, ok := dimEnumKey[dim]; ok {
				result[keys[1]] = len(entries.([]map[string]interface{}))
				result[keys[0]] = entries
			}
		}
		return result
	}

	// Service condition: filter l7_flow_log with the request WHERE (the
	// query's own SQL conditions — auto_service_id_0=143 AND
	// auto_service_type_0=11), collect the matching resource IDs from the
	// per-side columns, then resolve each dimension through its dictionary.
	// This matches the cloud behavior: DATA lists the resources the queried
	// service is actually related to (not the full dictionary).
	svcIDs := ExtractServiceIDs(req.Queries)
	if len(svcIDs) == 0 {
		return result
	}
	where := strings.TrimSpace(req.Queries[0].Where)
	if where == "" {
		return result
	}
	// Side (_0/_1) comes from the WHERE columns (auto_service_id_0 → _0).
	side := "0"
	if m := sideRE.FindStringSubmatch(where); m != nil {
		side = m[1]
	}
	// Build the flow-log query with the request's own conditions.
	cleanWhere := clickhouse.CleanWhereClause(where, "flow_log", "l7_flow_log")
	cleanWhere = clickhouse.IPConditionToSide(cleanWhere)
	cols := []string{
		"region_id", "az_id", "l3_epc_id", "subnet_id", "l3_device_id",
		"pod_cluster_id", "pod_ns_id", "pod_node_id", "pod_group_id", "pod_id",
		"ip4", "ip6",
	}
	var selCols []string
	for _, c := range cols {
		selCols = append(selCols, c+"_"+side)
	}
	timeWhere := ""
	if req.TimeStart > 0 {
		timeWhere = fmt.Sprintf(" AND time >= %d", req.TimeStart)
	}
	if req.TimeEnd > 0 {
		timeWhere += fmt.Sprintf(" AND time <= %d", req.TimeEnd)
	}
	query := fmt.Sprintf("SELECT DISTINCT %s FROM `flow_log`.`l7_flow_log` WHERE %s%s LIMIT 500",
		strings.Join(selCols, ", "), cleanWhere, timeWhere)
	rows, err := ch.Query(ctx, query)
	if err != nil {
		return result
	}
	defer rows.Close()
	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		return result
	}
	// Collect resource IDs per dimension.
	idSets := map[string]map[interface{}]bool{}
	for _, dim := range []string{
		"region_id", "az_id", "l3_epc_id", "subnet_id", "l3_device_id",
		"pod_cluster_id", "pod_ns_id", "pod_node_id", "pod_group_id", "pod_id",
	} {
		idSets[dim] = map[interface{}]bool{}
	}
	var ips []string
	for _, row := range data {
		for _, dim := range []string{
			"region_id", "az_id", "l3_epc_id", "subnet_id", "l3_device_id",
			"pod_cluster_id", "pod_ns_id", "pod_node_id", "pod_group_id", "pod_id",
		} {
			if v, ok := row[dim+"_"+side]; ok && v != nil {
				if isNonZeroID(v) {
					idSets[dim][v] = true
				}
			}
		}
		for _, ipCol := range []string{"ip4_" + side, "ip6_" + side} {
			if v, ok := row[ipCol]; ok && v != nil {
				if s := fmt.Sprintf("%v", v); s != "" && s != "0" && !isZeroIP(s) {
					ips = append(ips, s)
				}
			}
		}
	}
	if len(ips) > 0 {
		result["IP_NUM"] = len(ips)
		result["IPS"] = ips
	}
	// Resolve each dimension through its dictionary with the collected IDs.
	dimMap := []struct{ dim, table, idCol, nameCol, listKey, numKey string }{
		{"region_id", "region_map", "id", "name", "REGIONS", "REGION_NUM"},
		{"az_id", "az_map", "id", "name", "AZS", "AZ_NUM"},
		{"l3_epc_id", "l3_epc_map", "id", "name", "VPCS", "VPC_NUM"},
		{"subnet_id", "subnet_map", "id", "name", "SUBNETS", "SUBNET_NUM"},
		{"l3_device_id", "chost_map", "id", "name", "CHOSTS", "CHOST_NUM"},
		{"pod_cluster_id", "pod_cluster_map", "id", "name", "POD_CLUSTERS", "POD_CLUSTER_NUM"},
		{"pod_ns_id", "pod_ns_map", "id", "name", "POD_NSES", "POD_NS_NUM"},
		{"pod_node_id", "pod_node_map", "id", "name", "POD_NODES", "POD_NODE_NUM"},
		{"pod_group_id", "pod_group_map", "id", "name", "POD_GROUPS", "POD_GROUP_NUM"},
		{"pod_id", "pod_map", "id", "name", "PODS", "POD_NUM"},
	}
	for _, m := range dimMap {
		ids := idSets[m.dim]
		if len(ids) == 0 {
			continue
		}
		var idList []string
		for id := range ids {
			idList = append(idList, fmt.Sprintf("%v", id))
		}
		q := fmt.Sprintf("SELECT %s, %s FROM flow_tag.%s WHERE %s IN (%s) ORDER BY %s",
			m.idCol, m.nameCol, m.table, m.idCol, strings.Join(idList, ","), m.idCol)
		rrows, rerr := ch.Query(ctx, q)
		if rerr != nil {
			continue
		}
		rdata, rerr := clickhouse.ScanRows(rrows)
		rrows.Close()
		if rerr != nil || len(rdata) == 0 {
			continue
		}
		entries := make([]ResourceEntry, 0, len(rdata))
		for _, r := range rdata {
			name := GetStrDim(r, m.nameCol)
			if name == "" {
				name = fmt.Sprintf("%v", r[m.idCol])
			}
			entries = append(entries, ResourceEntry{ID: r[m.idCol], Name: name})
		}
		result[m.numKey] = len(entries)
		result[m.listKey] = entries
	}
	return result
}

// sideRE extracts the _0/_1 side suffix from WHERE conditions.
var sideRE = regexp.MustCompile(`(?:_id|ip)_(0|1)`)

// isZeroIP reports whether an IP string is all zeros ("0.0.0.0", "::",
// "0000:0000:...") — ClickHouse stores zero IPs as expanded strings.
func isZeroIP(s string) bool {
	if s == "::" || s == "0.0.0.0" {
		return true
	}
	return strings.Trim(s, "0:") == ""
}

// isNonZeroID reports whether a scanned ID value is a non-zero number.
func isNonZeroID(v interface{}) bool {
	switch n := v.(type) {
	case uint64:
		return n != 0
	case int64:
		return n != 0
	case int:
		return n != 0
	case float64:
		return n != 0
	}
	return false
}

// queryMapTable queries a flow_tag mapping table for ID+NAME pairs.
func QueryMapTable(ch *clickhouse.CHService, ctx context.Context, table, idCol, nameCol string) []ResourceEntry {
	q := fmt.Sprintf("SELECT %s, %s FROM flow_tag.%s WHERE %s != 0 ORDER BY %s LIMIT 20", idCol, nameCol, table, idCol, idCol)
	rows, err := ch.Query(ctx, q)
	if err != nil {
		return nil
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil || len(data) == 0 {
		return nil
	}

	result := make([]ResourceEntry, 0, len(data))
	for _, row := range data {
		id := row[idCol]
		name := GetStrDim(row, nameCol)
		if name == "" {
			name = fmt.Sprintf("%v", id)
		}
		result = append(result, ResourceEntry{ID: id, Name: name})
	}
	return result
}

// queryServiceIPs queries distinct IPs from the data table for the given services.
func QueryServiceIPs(ch *clickhouse.CHService, ctx context.Context, svcKeys []SvcKey, db, tbl string) []string {
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	// Build OR conditions for the service filter.
	var orClauses []string
	for _, sk := range svcKeys {
		orClauses = append(orClauses,
			fmt.Sprintf("(auto_service_id_0=%s AND auto_service_type_0=%s)", sk.id, sk.typ))
		orClauses = append(orClauses,
			fmt.Sprintf("(auto_service_id_1=%s AND auto_service_type_1=%s)", sk.id, sk.typ))
	}
	where := strings.Join(orClauses, " OR ")

	q := fmt.Sprintf("SELECT DISTINCT ip4 FROM `%s`.`%s` WHERE %s AND ip4 != 0 LIMIT 20", db, tbl, where)
	rows, err := ch.Query(ctx, q)
	if err != nil {
		return nil
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil || len(data) == 0 {
		return nil
	}

	result := make([]string, 0, len(data))
	for _, row := range data {
		if v := fmt.Sprintf("%v", row["ip4"]); v != "" && v != "0" {
			result = append(result, v)
		}
	}
	return result
}

// extractServiceIDs extracts "id:type" pairs from the WHERE clause.
func ExtractServiceIDs(queries []struct {
	Where  string `json:"WHERE"`
	Select string `json:"SELECT"`
	TOP    int    `json:"TOP"`
},
) []string {
	for _, q := range queries {
		w := q.Where

		// Pattern 1: auto_service_id_N=X AND auto_service_type_N=Y (with role suffix)
		// Pattern 2: auto_service_id=X (without role suffix, e.g. from resource-analysis drawer)
		idStart := strings.Index(w, "auto_service_id")
		if idStart < 0 {
			continue
		}
		rest := w[idStart:]
		var results []string

		// Check which pattern we have.
		hasRoleSuffix := strings.HasPrefix(rest, "auto_service_id_")

		// Parse the id value.
		eqIdx := strings.Index(rest, "=")
		if eqIdx < 0 {
			continue
		}
		endIdx := strings.IndexAny(rest[eqIdx:], " )`")
		idVal := ""
		if endIdx < 0 {
			idVal = rest[eqIdx+1:]
		} else {
			idVal = rest[eqIdx+1 : eqIdx+endIdx]
		}
		// Clean: remove backticks and trailing chars.
		idVal = strings.Trim(idVal, "`' ")

		var typeVal string
		if hasRoleSuffix {
			// Find the corresponding auto_service_type_N=Y.
			typeIdx := strings.Index(rest, "auto_service_type_")
			if typeIdx < 0 {
				// No type specified — use type from the flow_log table context if available.
				// Fall back to "255" (IP) as a reasonable default.
				typeVal = "0"
			} else {
				typeRest := rest[typeIdx:]
				eqIdx2 := strings.Index(typeRest, "=")
				if eqIdx2 >= 0 {
					endIdx2 := strings.IndexAny(typeRest[eqIdx2:], " )`")
					if endIdx2 < 0 {
						typeVal = typeRest[eqIdx2+1:]
					} else {
						typeVal = typeRest[eqIdx2+1 : eqIdx2+endIdx2]
					}
					typeVal = strings.Trim(typeVal, "`' ")
				}
			}
		} else {
			// Pattern 2: auto_service_id=X without role suffix.
			// Without a type, we can't meaningfully resolve resources.
			// But we should still return the id so the caller can try.
			typeVal = "0"
		}

		// Only add if we got a valid parse.
		if idVal != "" {
			results = append(results, idVal+":"+typeVal)
		}

		return results
	}
	return nil
}

// getStrDim safely extracts a string from a map.
// extractIPFromWhere extracts an IP address from WHERE clause conditions
// like ip_0='1.2.3.4' or `ip_1`='1.2.3.4' (backtick-quoted variants appear
// in real requests — the plain "ip_1='" match missed them).
func ExtractIPFromWhere(queries []struct {
	Where  string `json:"WHERE"`
	Select string `json:"SELECT"`
	TOP    int    `json:"TOP"`
},
) string {
	ipRE := regexp.MustCompile("(?:`?)ip_[01](?:`?)=\\s*'([^']+)'")
	for _, q := range queries {
		if m := ipRE.FindStringSubmatch(q.Where); m != nil {
			return m[1]
		}
	}
	return ""
}

func GetStrDim(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}

// emptyDimensionResult returns a zero-filled dimension result.
func EmptyDimensionResult() map[string]interface{} {
	return map[string]interface{}{
		"REGION_NUM": 0, "REGIONS": []ResourceEntry{},
		"AZ_NUM": 0, "AZS": []ResourceEntry{},
		"VPC_NUM": 0, "VPCS": []ResourceEntry{},
		"ROUTER_NUM": 0, "ROUTERS": []ResourceEntry{},
		"DHCPGW_NUM": 0, "DHCPGWS": []ResourceEntry{},
		"SUBNET_NUM": 0, "SUBNETS": []ResourceEntry{},
		"IP_NUM": 0, "IPS": []string{},
		"HOST_NUM": 0, "HOSTS": []ResourceEntry{},
		"CHOST_NUM": 0, "CHOSTS": []ResourceEntry{},
		"LB_NUM": 0, "LBS": []ResourceEntry{},
		"NATGW_NUM": 0, "NATGWS": []ResourceEntry{},
		"REDIS_NUM": 0, "REDISES": []ResourceEntry{},
		"RDS_NUM": 0, "RDSES": []ResourceEntry{},
		"POD_CLUSTER_NUM": 0, "POD_CLUSTERS": []ResourceEntry{},
		"POD_NS_NUM": 0, "POD_NSES": []ResourceEntry{},
		"POD_NODE_NUM": 0, "POD_NODES": []ResourceEntry{},
		"POD_SERVICE_NUM": 0, "POD_SERVICES": []ResourceEntry{},
		"POD_GROUP_NUM": 0, "POD_GROUPS": []ResourceEntry{},
		"POD_NUM": 0, "PODS": []ResourceEntry{},
		"debug": map[string]interface{}{},
	}
}
