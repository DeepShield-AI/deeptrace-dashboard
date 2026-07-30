package topo

import (
	"fmt"
)

func TopoNodeTypeFor(t int) string {
	switch t {
	case 0:
		return "internet_ip"
	case 1:
		return "chost"
	case 11:
		return "pod_service"
	case 15:
		return "lb"
	case 103:
		return "pod_cluster"
	case 104:
		return "biz_service"
	case 120:
		return "gprocess"
	case 130, 133:
		return "pod_group"
	case 255:
		return "ip"
	default:
		return "other"
	}
}

// topoIconFor maps auto_service_type to icon_id (matches cloud behavior).

func TopoIconFor(t int) int {
	switch t {
	case 0:
		return -1
	case 1:
		return -23
	case 11:
		return -16
	case 15:
		return -12
	case 103:
		return -13
	case 104:
		return -45
	case 120:
		return -43
	case 130, 133:
		return -18
	case 255:
		return -10
	default:
		return -42
	}
}

// getIntVal safely extracts an int value from a map (returns 0 if missing/nil).

func GetIntVal(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int64:
			return int(n)
		case uint8:
			return int(n)
		case uint64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

// getStrVal safely extracts a string value from a map.

func GetStrVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok2 := v.(string); ok2 {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func BuildTopoUID(row map[string]interface{}, suffix, iconKey, nodeKey string) string {
	svcName := ""
	if v, ok := row["auto_service"+suffix]; ok && v != nil {
		svcName = fmt.Sprintf("%v", v)
	}
	svcID := ""
	if v, ok := row["auto_service_id"+suffix]; ok && v != nil {
		svcID = fmt.Sprintf("%v", v)
	}
	svcType := ""
	if v, ok := row["auto_service_type"+suffix]; ok && v != nil {
		svcType = fmt.Sprintf("%v", v)
	}
	iconID := ""
	if v, ok := row[iconKey]; ok && v != nil {
		iconID = fmt.Sprintf("%v", v)
	}
	nodeType := ""
	if v, ok := row[nodeKey]; ok && v != nil {
		nodeType = fmt.Sprintf("%v", v)
	}
	return fmt.Sprintf("auto_service=%s,auto_service_id=%s,auto_service_type=%s,icon_id=%s,is_internet=0,node_type=%s,rs_set_id=R1",
		svcName, svcID, svcType, iconID, nodeType)
}

// getStrVal safely extracts a string value from a map.
// topoNodeTypeFor maps auto_service_type to node_type string (matches cloud behavior).

func BuildTopoUIDFromMap(m map[string]interface{}, suffix, iconKey, nodeKey, svcName string) string {
	svcID := fmt.Sprintf("%v", m["auto_service_id"+suffix])
	svcType := fmt.Sprintf("%v", m["auto_service_type"+suffix])
	iconID := fmt.Sprintf("%v", m[iconKey])
	nodeType := fmt.Sprintf("%v", m[nodeKey])
	return fmt.Sprintf("auto_service=%s,auto_service_id=%s,auto_service_type=%s,icon_id=%s,is_internet=0,node_type=%s,rs_set_id=R1",
		svcName, svcID, svcType, iconID, nodeType)
}

// buildTopoUID builds a UID string matching cloud format:
// auto_service=<name>,auto_service_id=<id>,auto_service_type=<type>,icon_id=<icon>,is_internet=<n>,node_type=<nt>,rs_set_id=R1

