package clickhouse

import (
	"fmt"
	"strings"
)

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
func NodeTypeFor(t int) string {
	switch t {
	case 0:
		return "internet_ip"
	case 1:
		return "vm"
	case 10:
		return "pod"
	case 11:
		return "pod_service"
	case 14:
		return "pod_ns"
	case 15:
		return "rds"
	case 103:
		return "biz_service"
	case 104:
		return "biz_service_group"
	case 105:
		return "alb"
	case 120:
		return "nat_gateway"
	case 130:
		return "peering_connection"
	case 133:
		return "vpn_gateway"
	case 255:
		return "ip"
	default:
		return fmt.Sprintf("unknown_%d", t)
	}
}

// IconFor maps auto_service_type to icon_id.
func IconFor(t int) float64 {
	switch t {
	case 0:
		return -1
	case 1:
		return -1
	case 10:
		return -16
	case 11:
		return -16
	case 14:
		return -16
	case 103:
		return -45
	case 104:
		return -45
	case 105:
		return -7
	case 120:
		return -7
	case 130:
		return -14
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

