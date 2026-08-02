package clickhouse

import (
	"fmt"
	"strings"
)

// QuerierRegion is the _querier_region value attached to every querier result row.
// Defaults to "本地"; the region service (query/region) overwrites it with the
// real default region name at startup when deepflow-server is reachable.
var QuerierRegion = "本地"

// IconIDDefault returns a default icon_id for a given field name based on naming convention.
func IconIDDefault(fieldName string) float64 {
	switch {
	case strings.Contains(fieldName, "client_auto_service"):
		return -13
	case strings.Contains(fieldName, "server_client_auto_service"):
		return -13
	case strings.Contains(fieldName, "server_auto_service"):
		return -16
	case strings.Contains(fieldName, "client_server_auto_service"):
		return -16
	case strings.Contains(fieldName, "client_icon_id"):
		return -15
	case strings.Contains(fieldName, "server_client_icon_id"):
		return -15
	case strings.Contains(fieldName, "server_icon_id"):
		return -17
	case strings.Contains(fieldName, "client_server_icon_id"):
		return -17
	default:
		return -1
	}
}

// NodeTypeFor maps auto_service_type to node_type string.
// Ground truth (three sources, all consistent):
//   - DeepFlow trident.proto AutoServiceType numeric values
//   - zerotrace-server controller/tagrecorder RESOURCE_TYPE_TO_NODE_TYPE
//   - api_cache TraceMap node_data (1114 nodes): 1→chost, 11→pod_service,
//     103→pod_cluster, 104→biz_service, 130→pod_group, 255→ip
//
// Keep in sync with topo.TopoNodeTypeFor.
func NodeTypeFor(t int) string {
	switch t {
	case 0:
		return "internet_ip"
	case 1:
		return "chost"
	case 5:
		return "router"
	case 6:
		return "host"
	case 9:
		return "dhcpgw"
	case 10:
		return "pod"
	case 11:
		return "pod_service"
	case 12:
		return "redis"
	case 13:
		return "rds"
	case 14:
		return "pod_node"
	case 15:
		return "lb"
	case 16:
		return "natgw"
	case 101:
		return "pod_group"
	case 102:
		return "service"
	case 103:
		return "pod_cluster"
	case 104:
		return "biz_service"
	case 105:
		return "alb"
	case 120:
		return "gprocess"
	case 130, 131, 132, 133, 134, 135:
		return "pod_group"
	case 255:
		return "ip"
	default:
		return fmt.Sprintf("unknown_%d", t)
	}
}

// IconFor maps auto_service_type to icon_id.
// Verified against api_cache TraceMap node_data: 1→-23, 103→-13,
// 104→-45, 130→-18.
func IconFor(t int) float64 {
	switch t {
	case 0:
		return -1
	case 1:
		return -23
	case 10:
		return -16
	case 11:
		return -16
	case 14:
		return -16
	case 103:
		return -13
	case 104:
		return -45
	case 105:
		return -7
	case 120:
		return -7
	case 130:
		return -18
	case 133:
		return -14
	case 255:
		return -10
	default:
		return -1
	}
}

// BuildSchemas creates a SCHEMAS map from a result row.
func BuildSchemas(row map[string]interface{}) map[string]interface{} {
	schemas := map[string]interface{}{}
	for k, v := range row {
		vt, tp := "String", 0
		switch v.(type) {
		case float64, float32:
			vt, tp = "Float64", 1
		case int, int64, uint64:
			vt, tp = "UInt64", 1
		}
		schemas[k] = map[string]interface{}{
			"label_type": "", "pre_as": "", "type": tp,
			"unit": "", "value_type": vt,
		}
	}
	return schemas
}

// MetricExpr holds a parsed metric expression.
type MetricExpr struct {
	Key string
	SQL string
}

// ValidateTableName rejects identifiers that could break out of a
// backtick-quoted SQL identifier (backticks, quotes, whitespace, semicolons).
// Database and table names are spliced into SQL in several builders; this
// keeps them to a safe character set. An empty name passes (callers default
// it first).
func ValidateTableName(name string) error {
	if name == "" {
		return nil
	}
	// "." is allowed as a separator (e.g. flow_metrics.1m), but ".." or
	// leading/trailing dots would be ambiguous / path-like.
	if strings.Contains(name, "..") || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("%w: invalid table/database name %q", ErrUnsupportedColumn, name)
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') && r != '_' && r != '.' && r != '-' {
			return fmt.Errorf("%w: invalid table/database name %q", ErrUnsupportedColumn, name)
		}
	}
	return nil
}
