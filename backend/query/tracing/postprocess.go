package tracing

import (
	"encoding/json"
	"fmt"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
)

// L7FlowTracingRequestRaw accepts both cloud API format (lowercase) and
// algorithms service format (uppercase), normalizing to uppercase.
type L7FlowTracingRequestRaw struct {
	Region          string   `json:"REGION"`
	TimeStart       int64    `json:"TIME_START"`
	TimeEnd         int64    `json:"TIME_END"`
	Database        string   `json:"DATABASE"`
	Table           string   `json:"TABLE"`
	ID              string   `json:"_id"`
	TraceID         string   `json:"trace_id"`
	MaxIteration    int      `json:"MAX_ITERATION"`
	NetworkDelayUS  int      `json:"NETWORK_DELAY_US"`
	HostClockOffset int      `json:"HOST_CLOCK_OFFSET_US"`
	SignalSources   []string `json:"SIGNAL_SOURCES"`

	// Cloud API lowercase variants.
	TimeStartFloat float64  `json:"time_start"`
	TimeEndFloat   float64  `json:"time_end"`
	TracingSources []string `json:"tracing_sources"`
}

// Normalize returns an L7FlowTracingRequest with fields populated from
// both the uppercase (algorithms service) and lowercase (cloud API) formats.
func (r *L7FlowTracingRequestRaw) Normalize() *client.L7FlowTracingRequest {
	req := &client.L7FlowTracingRequest{
		// NOTE: REGION is NOT forwarded to the algorithms service — it causes
		// "PARTIAL_RESULT / 无法找到可用的查询节点". The service discovers
		// region from deepflow-server internally.
		Database:        r.Database,
		Table:           r.Table,
		ID:              r.ID,
		TraceID:         r.TraceID,
		MaxIteration:    r.MaxIteration,
		NetworkDelayUS:  r.NetworkDelayUS,
		HostClockOffset: r.HostClockOffset,
	}

	// TimeStart: uppercase first, fallback to lowercase.
	if r.TimeStart > 0 {
		req.TimeStart = r.TimeStart
	} else if r.TimeStartFloat > 0 {
		req.TimeStart = int64(r.TimeStartFloat)
	}

	// TimeEnd: uppercase first, fallback to lowercase.
	if r.TimeEnd > 0 {
		req.TimeEnd = r.TimeEnd
	} else if r.TimeEndFloat > 0 {
		req.TimeEnd = int64(r.TimeEndFloat)
	}

	// SignalSources: uppercase first, fallback to tracing_sources.
	if len(r.SignalSources) > 0 {
		req.SignalSources = r.SignalSources
	} else if len(r.TracingSources) > 0 {
		req.SignalSources = r.TracingSources
	}

	return req
}

// PostProcessResult ensures the algorithms service response matches the cloud API structure.
// It fills in missing fields: span_time_correction, paths, render timestamps, and defaults.
func PostProcessResult(result *client.L7FlowTracingResponse) map[string]interface{} {
	data, ok := result.Data.(map[string]interface{})
	if !ok {
		data = map[string]interface{}{}
	}

	// Ensure span_time_correction exists.
	if _, has := data["span_time_correction"]; !has {
		if hcc, ok := data["host_clock_correction"].(map[string]interface{}); ok && len(hcc) > 0 {
			data["span_time_correction"] = hcc
		} else {
			data["span_time_correction"] = map[string]interface{}{}
		}
	}

	// Ensure paths exists.
	if _, has := data["paths"]; !has {
		data["paths"] = computePaths(data)
	}

	// Add render timestamps and missing fields to each tracing item.
	if tracing, ok := data["tracing"].([]interface{}); ok {
		for _, item := range tracing {
			if m, ok := item.(map[string]interface{}); ok {
				if _, has := m["render_start_time_us"]; !has {
					if st, ok := m["start_time_us"]; ok {
						m["render_start_time_us"] = st
					}
				}
				if _, has := m["render_end_time_us"]; !has {
					if et, ok := m["end_time_us"]; ok {
						m["render_end_time_us"] = et
					}
				}
				if _, has := m["response_exception"]; !has {
					m["response_exception"] = ""
				}
				if _, has := m["biz_response_code"]; !has {
					m["biz_response_code"] = ""
				}
				if _, has := m["is_tls"]; !has {
					m["is_tls"] = 0
				}
				if _, has := m["is_async"]; !has {
					m["is_async"] = 0
				}
				if _, has := m["response_code"]; !has {
					m["response_code"] = nil
				}
			}
		}
	}

	return map[string]interface{}{
		"OPT_STATUS":  "SUCCESS",
		"TYPE":        result.Type,
		"DATA":        data,
		"DESCRIPTION": result.Description,
	}
}

// computePaths derives service-pair paths from tracing items, matching
// the cloud API's structure (client_uid/client_uname/server_uid/server_uname).
func computePaths(data map[string]interface{}) []interface{} {
	tracing, ok := data["tracing"].([]interface{})
	if !ok || len(tracing) == 0 {
		return []interface{}{}
	}

	type pair struct{ clientUID, clientUname, serverUID, serverUname string }
	edgeDurations := map[pair]int64{}
	totalDur := int64(0)

	for _, item := range tracing {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		dur, _ := clickhouse.ToInt64OK(m["duration"])
		suid := toString(m["service_uid"])
		sname := toString(m["service_uname"])

		totalDur += dur

		// If this span has a parent with a different service_uid, it's an edge.
		parentIdx, hasParent := m["parent_id"].(float64)
		if hasParent && int(parentIdx) >= 0 && int(parentIdx) < len(tracing) {
			parent := tracing[int(parentIdx)]
			if pm, ok := parent.(map[string]interface{}); ok {
				puid := toString(pm["service_uid"])
				pname := toString(pm["service_uname"])
				if puid != suid {
					edgeDurations[pair{puid, pname, suid, sname}] += dur
				}
			}
		}
	}

	if totalDur == 0 {
		return []interface{}{}
	}

	var paths []interface{}
	for p, dur := range edgeDurations {
		ratio := float64(dur) / float64(totalDur) * 100
		paths = append(paths, map[string]interface{}{
			"client_uid":     p.clientUID,
			"client_uname":   p.clientUname,
			"server_uid":     p.serverUID,
			"server_uname":   p.serverUname,
			"duration":       dur,
			"duration_ratio": formatRatio(ratio),
		})
	}
	if paths == nil {
		paths = []interface{}{}
	}
	return paths
}

func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return s.String()
	}
	return ""
}

func formatRatio(r float64) string {
	return fmt.Sprintf("%.2f", r)
}

// EmptyResult returns an empty tracing result matching the cloud API structure.
func EmptyResult() map[string]interface{} {
	return map[string]interface{}{
		"OPT_STATUS": "SUCCESS",
		"TYPE":       "Flow_Log_L7_Tracing",
		"DATA": map[string]interface{}{
			"tracing":               []interface{}{},
			"services":              []interface{}{},
			"span_time_correction":  map[string]interface{}{},
			"host_clock_correction": map[string]interface{}{},
			"paths":                 []interface{}{},
		},
		"DESCRIPTION": "",
	}
}
