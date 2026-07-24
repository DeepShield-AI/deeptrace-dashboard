package engine

import (
	"fmt"
	"strings"
	"time"
)

// GetStr safely extracts a string from a map.
func GetStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// GetF64 safely extracts a float64 from a map.
func GetF64(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok && v != nil {
		switch val := v.(type) {
		case float64:
			return val
		case uint64:
			return float64(val)
		case int64:
			return float64(val)
		case float32:
			return float64(val)
		default:
			return 0
		}
	}
	return 0
}

// FormatTimestamp converts a ClickHouse timestamp (microseconds) to ISO 8601 string.
func FormatTimestamp(val interface{}) string {
	switch v := val.(type) {
	case float64:
		return time.UnixMicro(int64(v)).Format("2006-01-02T15:04:05.000000-07:00")
	case uint64:
		return time.UnixMicro(int64(v)).Format("2006-01-02T15:04:05.000000-07:00")
	case int64:
		return time.UnixMicro(v).Format("2006-01-02T15:04:05.000000-07:00")
	case string:
		return v
	default:
		return fmt.Sprintf("%v", val)
	}
}

// IconIDDefault returns a default icon_id for a given field name.
// The icon ID depends on which side of the connection the field represents:
//
//	                    | auto_service_0/1        | auto_instance_0/1
//	Client-side  (-13) | client_auto_service_…   | client_icon_id / server_client_icon_id → -15
//	Server-side  (-16) | server_auto_service_…   | server_icon_id / client_server_icon_id → -17
//
// Naming convention:
//   - client_*          = client side  → auto_service_0 / auto_instance_0
//   - server_client_*   = client side  (server looking at client)
//   - server_*          = server side  → auto_service_1 / auto_instance_1
//   - client_server_*   = server side  (client looking at server)
func IconIDDefault(fieldName string) float64 {
	// Determine side: isServerSide = server_* (but NOT server_client_*) OR client_server_*
	isServerSide := (strings.HasPrefix(fieldName, "server_") && !strings.HasPrefix(fieldName, "server_client_")) ||
		strings.HasPrefix(fieldName, "client_server_")
	// Determine if this is a service icon or instance icon.
	isService := strings.Contains(fieldName, "auto_service")
	if isServerSide {
		if isService {
			return -16 // auto_service_1 (server-side service)
		}
		return -17 // auto_instance_1 (server-side instance)
	}
	// Client-side
	if isService {
		return -13 // auto_service_0 (client-side service)
	}
	return -15 // auto_instance_0 (client-side instance)
}
