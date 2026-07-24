package enum

import (
	"context"
	"fmt"
	"log"
	"sync"

	"deeptrace-backend/clickhouse"
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
			log.Printf("✅ Loaded %d enum types from ClickHouse dictionaries", len(s.maps))
		} else {
			s.maps = fallbackEnumMaps()
			log.Printf("📋 Using built-in enum maps (%d types)", len(s.maps))
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
		log.Printf("⚠️  int_enum_map load: %v", err)
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
		log.Printf("⚠️  string_enum_map load: %v", err)
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
			"60": "MySQL", "61": "PostgreSQL", "68": "",
			"80": "Redis", "100": "Kafka", "101": "MQTT",
			"120": "DNS", "121": "TLS",
		},
		"protocol": {
			"0": "HOPOPT", "1": "ICMP", "2": "IGMP",
			"6": "TCP", "17": "UDP",
			"41": "IPv6", "47": "GRE", "50": "ESP", "51": "AH",
			"58": "IPv6-ICMP", "89": "OSPF", "103": "PIM", "132": "SCTP",
		},
		"is_tls":              {"0": "否", "1": "是"},
		"is_async":            {"0": "否", "1": "是"},
		"status":              {"0": "正常", "1": "异常", "2": "超时"},
		"role":                {"0": "客户端", "1": "服务端", "2": "内部"},
		"is_internet":         {"0": "内网", "1": "公网"},
		"signal_source":       {"0": "OTel", "3": "eBPF", "4": "Prometheus"},
		"auto_service_type": {
			"0": "未知", "1": "虚拟机",
			"10": "Pod", "11": "Pod Service", "15": "RDS 实例",
			"103": "业务服务", "104": "业务服务组",
			"105": "ALB", "120": "NAT 网关",
			"130": "对等连接", "133": "VPN 网关", "255": "其他",
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
