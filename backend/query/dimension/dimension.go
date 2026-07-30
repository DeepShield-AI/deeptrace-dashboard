package dimension

import (
	"context"
	"fmt"
	"strings"

	"deeptrace-backend/clickhouse"
)

type svcKey struct{ id, typ string }

// dimReq mirrors the dimension-resources request body.
type dimReq struct {
	Database  string
	Table     string
	TimeStart int64
	TimeEnd   int64
	Region    string
	Queries   []struct {
		Where  string `json:"WHERE"`
		Select string `json:"SELECT"`
		TOP    int    `json:"TOP"`
	}
}

// resourceEntry holds a resource ID + NAME pair.
type resourceEntry struct {
	ID   interface{}
	Name string
}

func queryDimensionResources(ch *clickhouse.CHService, req dimReq) map[string]interface{} {
	result := emptyDimensionResult()

	if ch == nil || !ch.Enabled() {
		return result
	}

	// Extract service ID and type from WHERE clause.
	// WHERE format: "(auto_service_id_0=X AND auto_service_type_0=Y) OR (...)"
	svcIDs := extractServiceIDs(req.Queries)

	// If no service IDs found, try extracting IP from WHERE clause.
	// WHERE format: "(subnet_id_0=0 AND ip_0='1.2.3.4') OR (subnet_id_1=0 AND ip_1='1.2.3.4')"
	if len(svcIDs) == 0 {
		ip := extractIPFromWhere(req.Queries)
		if ip != "" {
			result["IP_NUM"] = 1
			result["IPS"] = []string{ip}
			svcIDs = []string{"0:0"} // placeholder to continue resource queries
		}
	}
	if len(svcIDs) == 0 {
		return result
	}

	ctx := context.Background()

	// Build a set of (service_id, service_type) pairs.
	var svcKeys []svcKey
	for _, s := range svcIDs {
		parts := strings.Split(s, ":")
		if len(parts) == 2 {
			svcKeys = append(svcKeys, svcKey{parts[0], parts[1]})
		}
	}
	if len(svcKeys) == 0 {
		return result
	}

	// Query flow_tag mapping tables for each resource type.
	// Use the service ID to filter related resources via service_id columns.
	if rows := queryMapTable(ch, ctx, "region_map", "id", "name"); len(rows) > 0 {
		result["REGION_NUM"] = len(rows)
		result["REGIONS"] = rows
	}
	if rows := queryMapTable(ch, ctx, "az_map", "id", "name"); len(rows) > 0 {
		result["AZ_NUM"] = len(rows)
		result["AZS"] = rows
	}
	if rows := queryMapTable(ch, ctx, "l3_epc_map", "id", "name"); len(rows) > 0 {
		result["VPC_NUM"] = len(rows)
		result["VPCS"] = rows
	}
	if rows := queryMapTable(ch, ctx, "subnet_map", "id", "name"); len(rows) > 0 {
		result["SUBNET_NUM"] = len(rows)
		result["SUBNETS"] = rows
	}
	if rows := queryMapTable(ch, ctx, "chost_map", "id", "name"); len(rows) > 0 {
		result["CHOST_NUM"] = len(rows)
		result["CHOSTS"] = rows
	}
	if rows := queryMapTable(ch, ctx, "pod_cluster_map", "id", "name"); len(rows) > 0 {
		result["POD_CLUSTER_NUM"] = len(rows)
		result["POD_CLUSTERS"] = rows
	}
	if rows := queryMapTable(ch, ctx, "pod_ns_map", "id", "name"); len(rows) > 0 {
		result["POD_NS_NUM"] = len(rows)
		result["POD_NSES"] = rows
	}
	if rows := queryMapTable(ch, ctx, "pod_node_map", "id", "name"); len(rows) > 0 {
		result["POD_NODE_NUM"] = len(rows)
		result["POD_NODES"] = rows
	}
	if rows := queryMapTable(ch, ctx, "pod_service_map", "id", "name"); len(rows) > 0 {
		result["POD_SERVICE_NUM"] = len(rows)
		result["POD_SERVICES"] = rows
	}
	if rows := queryMapTable(ch, ctx, "pod_group_map", "id", "name"); len(rows) > 0 {
		result["POD_GROUP_NUM"] = len(rows)
		result["POD_GROUPS"] = rows
	}
	if rows := queryMapTable(ch, ctx, "pod_map", "id", "name"); len(rows) > 0 {
		result["POD_NUM"] = len(rows)
		result["PODS"] = rows
	}

	// For IPs, query distinct IPs from the data table related to the service.
	if ips := queryServiceIPs(ch, ctx, svcKeys, req.Database, req.Table); len(ips) > 0 {
		result["IP_NUM"] = len(ips)
		result["IPS"] = ips
	}

	return result
}

// queryMapTable queries a flow_tag mapping table for ID+NAME pairs.
func queryMapTable(ch *clickhouse.CHService, ctx context.Context, table, idCol, nameCol string) []resourceEntry {
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

	result := make([]resourceEntry, 0, len(data))
	for _, row := range data {
		id := row[idCol]
		name := getStrDim(row, nameCol)
		if name == "" {
			name = fmt.Sprintf("%v", id)
		}
		result = append(result, resourceEntry{ID: id, Name: name})
	}
	return result
}

// queryServiceIPs queries distinct IPs from the data table for the given services.
func queryServiceIPs(ch *clickhouse.CHService, ctx context.Context, svcKeys []svcKey, db, tbl string) []string {
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
func extractServiceIDs(queries []struct {
	Where  string `json:"WHERE"`
	Select string `json:"SELECT"`
	TOP    int    `json:"TOP"`
}) []string {
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
// extractIPFromWhere extracts an IP address from WHERE clause conditions like ip_0='1.2.3.4'.
func extractIPFromWhere(queries []struct {
	Where  string `json:"WHERE"`
	Select string `json:"SELECT"`
	TOP    int    `json:"TOP"`
}) string {
	for _, q := range queries {
		w := q.Where
		// Look for ip_0='...' or ip_1='...'
		for _, prefix := range []string{"ip_0='", "ip_1='"} {
			idx := strings.Index(w, prefix)
			if idx < 0 {
				continue
			}
			rest := w[idx+len(prefix):]
			end := strings.Index(rest, "'")
			if end > 0 {
				return rest[:end]
			}
		}
	}
	return ""
}

func getStrDim(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}

// emptyDimensionResult returns a zero-filled dimension result.
func emptyDimensionResult() map[string]interface{} {
	return map[string]interface{}{
		"REGION_NUM": 0, "REGIONS": []resourceEntry{},
		"AZ_NUM": 0, "AZS": []resourceEntry{},
		"VPC_NUM": 0, "VPCS": []resourceEntry{},
		"ROUTER_NUM": 0, "ROUTERS": []resourceEntry{},
		"DHCPGW_NUM": 0, "DHCPGWS": []resourceEntry{},
		"SUBNET_NUM": 0, "SUBNETS": []resourceEntry{},
		"IP_NUM": 0, "IPS": []string{},
		"HOST_NUM": 0, "HOSTS": []resourceEntry{},
		"CHOST_NUM": 0, "CHOSTS": []resourceEntry{},
		"LB_NUM": 0, "LBS": []resourceEntry{},
		"NATGW_NUM": 0, "NATGWS": []resourceEntry{},
		"REDIS_NUM": 0, "REDISES": []resourceEntry{},
		"RDS_NUM": 0, "RDSES": []resourceEntry{},
		"POD_CLUSTER_NUM": 0, "POD_CLUSTERS": []resourceEntry{},
		"POD_NS_NUM": 0, "POD_NSES": []resourceEntry{},
		"POD_NODE_NUM": 0, "POD_NODES": []resourceEntry{},
		"POD_SERVICE_NUM": 0, "POD_SERVICES": []resourceEntry{},
		"POD_GROUP_NUM": 0, "POD_GROUPS": []resourceEntry{},
		"POD_NUM": 0, "PODS": []resourceEntry{},
		"debug": map[string]interface{}{},
	}
}

