package enum

import (
	"context"
	"fmt"
	"sync"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/logging"
)

// EnumService provides human-readable display names for enum values by loading
// from ClickHouse dictionaries (int_enum_map, string_enum_map) at startup.
// Falls back to built-in maps when ClickHouse is unavailable.
type EnumService struct {
	ch   *clickhouse.CHService
	mu   sync.RWMutex
	maps map[string]map[string]string
	once sync.Once
}

// NewEnumService creates an EnumService. Call Init() to load data.
func NewEnumService(ch *clickhouse.CHService) *EnumService {
	return &EnumService{ch: ch}
}

// Init loads enum display maps from ClickHouse dictionaries.
// Falls back to built-in maps if ClickHouse is unavailable.
func (s *EnumService) Init() {
	s.once.Do(func() {
		s.maps = loadFromCH(s.ch)
		if s.maps != nil {
			logging.Infof("Loaded %d enum types from ClickHouse dictionaries", len(s.maps))
		} else {
			s.maps = fallbackEnumMaps()
			logging.Warnf("Using built-in enum maps (%d types)", len(s.maps))
		}
	})
}

// GetDisplay resolves a raw enum value to its human-readable display name.
// Returns the raw value string if no mapping is found.
func (s *EnumService) GetDisplay(enumName string, rawValue interface{}) interface{} {
	strVal := fmt.Sprintf("%v", rawValue)
	s.mu.RLock()
	m, ok := s.maps[enumName]
	s.mu.RUnlock()
	if ok {
		if display, ok2 := m[strVal]; ok2 {
			return display
		}
	}
	return rawValue
}

// loadFromCH queries ClickHouse dictionaries for enum display mappings.
func loadFromCH(ch *clickhouse.CHService) map[string]map[string]string {
	if ch == nil || !ch.Enabled() {
		return nil
	}

	ctx := context.Background()
	result := make(map[string]map[string]string)

	// Load int_enum_map (tag_name, value, name_zh, name_en, …)
	rows, err := ch.Query(ctx,
		"SELECT tag_name, toString(value), name_zh FROM flow_tag.int_enum_map")
	if err == nil {
		if data, err2 := clickhouse.ScanRows(rows); err2 == nil {
			for _, row := range data {
				tag := fmt.Sprintf("%v", row["tag_name"])
				val := fmt.Sprintf("%v", row["toString(value)"])
				name := fmt.Sprintf("%v", row["name_zh"])
				insertEnumMap(result, tag, val, name)
			}
		}
	} else {
		logging.Errorf("int_enum_map load: %v", err)
	}

	// Load string_enum_map (tag_name, value, name_zh, name_en, …)
	rows, err = ch.Query(ctx,
		"SELECT tag_name, value, name_zh FROM flow_tag.string_enum_map")
	if err == nil {
		if data, err2 := clickhouse.ScanRows(rows); err2 == nil {
			for _, row := range data {
				tag := fmt.Sprintf("%v", row["tag_name"])
				val := fmt.Sprintf("%v", row["value"])
				name := fmt.Sprintf("%v", row["name_zh"])
				insertEnumMap(result, tag, val, name)
			}
		}
	} else {
		logging.Errorf("string_enum_map load: %v", err)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func insertEnumMap(m map[string]map[string]string, tag, val, name string) {
	sub, ok := m[tag]
	if !ok {
		sub = make(map[string]string)
		m[tag] = sub
	}
	sub[val] = name
}

// FallbackEnumMaps provides correct default values when ClickHouse is unavailable.
// All values are sourced from ClickHouse Dictionaries (int_enum_map, string_enum_map).
// Exported so other query paths (flowlog, showtagvalues) share one source of truth.
func FallbackEnumMaps() map[string]map[string]string { return fallbackEnumMaps() }

// fallbackEnumMaps provides correct default values when ClickHouse is unavailable.
// All values are sourced from ClickHouse Dictionaries (int_enum_map, string_enum_map).
func fallbackEnumMaps() map[string]map[string]string {
	return map[string]map[string]string{
		"response_status": {
			"0": "正常", "1": "异常", "2": "超时",
			"3": "服务端异常", "4": "客户端异常", "5": "取消",
		},
		"observation_point": {
			"c": "客户端网卡", "s": "服务端网卡",
			"c-p": "客户侧网络", "s-p": "服务侧网络",
			"c-app": "客户端应用", "s-app": "服务端应用",
			"app": "应用", "rest": "其他",
		},
		"l7_protocol": {
			"0": "N/A", "1": "",
			"20": "HTTP", "21": "HTTP2", "40": "Dubbo", "41": "gRPC",
			"43": "SofaRPC", "44": "FastCGI", "45": "bRPC", "46": "Tars",
			"47": "Some/IP", "48": "ISO-8583", "49": "Triple", "50": "NetSign",
			"60": "MySQL", "61": "PostgreSQL", "62": "Oracle", "63": "Dameng",
			"80": "Redis", "81": "MongoDB", "82": "Memcached",
			"100": "Kafka", "101": "MQTT", "102": "AMQP", "103": "OpenWire",
			"104": "NATS", "105": "Pulsar", "106": "ZMTP", "107": "RocketMQ",
			"108": "WebSphereMQ",
			"120": "DNS", "121": "TLS", "122": "Ping", "127": "Custom",
		},
		"protocol": {
			"0": "HOPOPT", "1": "ICMP", "2": "IGMP",
			"6": "TCP", "17": "UDP",
			"41": "IPv6", "47": "GRE", "50": "ESP", "51": "AH",
			"58": "IPv6-ICMP", "89": "OSPF", "103": "PIM", "132": "SCTP",
		},
		"is_tls":           {"0": "否", "1": "是"},
		"is_async":         {"0": "否", "1": "是"},
		"status":           {"0": "正常", "1": "异常", "2": "超时"},
		"role":             {"0": "客户端", "1": "服务端", "2": "内部"},
		"is_internet":      {"0": "内网", "1": "公网"},
		"signal_source":    {"0": "Packet", "3": "eBPF", "4": "OTel"},
		"l7_signal_source": {"0": "Packet", "3": "eBPF", "4": "OTel"},
		// auto_service_type semantics follow DeepFlow trident.proto
		// AutoServiceType + zerotrace-server RESOURCE_TYPE_TO_NODE_TYPE:
		// 1=CHOST, 12=REDIS_INSTANCE, 13=RDS_INSTANCE, 15=LOAD_BALANCE,
		// 16=NAT_GATEWAY, 101=POD_GROUP, 102=SERVICE, 103=POD_CLUSTER,
		// 104=CUSTOM_SERVICE (UI name biz_service, per api_cache),
		// 120=PROCESS, 130-135=POD_GROUP_*, 255=IP.
		"auto_service_type": {
			"0": "公网 IP", "1": "云主机",
			"10": "Pod", "11": "Pod Service", "12": "Redis", "13": "RDS",
			"14": "Pod 节点", "15": "负载均衡", "16": "NAT 网关",
			"101": "Pod 组", "102": "服务",
			"103": "Pod 集群", "104": "业务服务",
			"105": "ALB", "120": "进程",
			"130": "Pod 组", "131": "Pod 组", "132": "Pod 组",
			"133": "Pod 组", "134": "Pod 组", "135": "Pod 组",
			"255": "IP",
		},
		"auto_instance_type": {
			"0": "未知", "1": "虚拟机",
			"10": "Pod", "11": "Pod Group",
			"14": "命名空间", "255": "其他",
		},
		"close_type": {
			"0": "TCP 连接超时", "1": "TCP 连接重置", "2": "TCP 服务端断开",
			"3": "TCP 客户端断开", "4": "TCP 服务端 fin", "5": "周期性上报",
		},
		"event_type": {
			"0": "读", "1": "写", "2": "创建", "3": "删除",
			"4": "修改权限", "5": "修改属性", "6": "修改名称",
			"7": "打开", "8": "关闭", "9": "读目录",
		},
	}
}
