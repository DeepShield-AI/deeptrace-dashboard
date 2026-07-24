package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"deeptrace-backend/client"
)

// RegisterTracing registers tracing algorithm endpoints.
func RegisterTracing(mux *http.ServeMux, algo *client.AlgorithmsService) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/L4FlowTracing", handleL7FlowTracing(algo))
	mux.HandleFunc("/api/statistics/v1/stats/querier/L7FlowTracing", handleL7FlowTracing(algo))
	mux.HandleFunc("/api/deepflow-app/v1/stats/querier/L7FlowTracing", handleL7FlowTracing(algo))
	mux.HandleFunc("/api/statistics/v1/stats/querier/tracing-completion-by-external-app-spans", handleTracingCompletion(algo))
	mux.HandleFunc("/api/statistics/v1/stats/querier/TracingAlgoParams", handleTracingAlgoParams(algo))
}

// l7FlowTracingRequestRaw accepts both cloud API format (lowercase) and
// algorithms service format (uppercase), normalizing to uppercase.
type l7FlowTracingRequestRaw struct {
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
	TimeStartFloat  float64  `json:"time_start"`
	TimeEndFloat    float64  `json:"time_end"`
	TracingSources  []string `json:"tracing_sources"`
}

// normalize returns an L7FlowTracingRequest with fields populated from
// both the uppercase (algorithms service) and lowercase (cloud API) formats.
func (r *l7FlowTracingRequestRaw) normalize() *client.L7FlowTracingRequest {
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

	// TimeStart: uppercase first, fallback to lowercase
	if r.TimeStart > 0 {
		req.TimeStart = r.TimeStart
	} else if r.TimeStartFloat > 0 {
		req.TimeStart = int64(r.TimeStartFloat)
	}

	// TimeEnd: uppercase first, fallback to lowercase
	if r.TimeEnd > 0 {
		req.TimeEnd = r.TimeEnd
	} else if r.TimeEndFloat > 0 {
		req.TimeEnd = int64(r.TimeEndFloat)
	}

	// SignalSources: uppercase first, fallback to tracing_sources
	if len(r.SignalSources) > 0 {
		req.SignalSources = r.SignalSources
	} else if len(r.TracingSources) > 0 {
		req.SignalSources = r.TracingSources
	}

	return req
}

func handleL7FlowTracing(algo *client.AlgorithmsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, "cannot read body", 400)
			return
		}

		if algo != nil && algo.Available() {
			var raw l7FlowTracingRequestRaw
			if err := json.Unmarshal(body, &raw); err != nil {
				log.Printf("⚠️  L7FlowTracing unmarshal error: %v", err)
			} else {
				req := raw.normalize()
				result, err := algo.L7FlowTracing(req)
				if err != nil {
					log.Printf("⚠️  L7FlowTracing algorithm error: %v", err)
				} else if result != nil {
					env := postProcessTracingResult(result)
					writeJSON(w, env)
					return
				}
			}
		}

		// Fallback: matching the cloud API response structure.
		log.Printf("ℹ️  L7FlowTracing: algorithms unavailable, returning empty")
		writeJSON(w, map[string]interface{}{
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
		})
	}
}

// postProcessTracingResult ensures the response matches the cloud API structure.
// The algorithms service may omit span_time_correction, paths, and render timestamps.
func postProcessTracingResult(result *client.L7FlowTracingResponse) map[string]interface{} {
	data, ok := result.Data.(map[string]interface{})
	if !ok {
		data = map[string]interface{}{}
	}

	// Ensure span_time_correction exists
	if _, has := data["span_time_correction"]; !has {
		// Derive from host_clock_correction if available
		if hcc, ok := data["host_clock_correction"].(map[string]interface{}); ok && len(hcc) > 0 {
			data["span_time_correction"] = hcc
		} else {
			data["span_time_correction"] = map[string]interface{}{}
		}
	}

	// Ensure paths exists
	if _, has := data["paths"]; !has {
		data["paths"] = computePaths(data)
	}

	// Add render timestamps and missing fields to each tracing item
	if tracing, ok := data["tracing"].([]interface{}); ok {
		for _, item := range tracing {
			if m, ok := item.(map[string]interface{}); ok {
				// render_start_time_us / render_end_time_us
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
				// response_exception default
				if _, has := m["response_exception"]; !has {
					m["response_exception"] = ""
				}
				// biz_response_code default
				if _, has := m["biz_response_code"]; !has {
					m["biz_response_code"] = ""
				}
				// is_tls / is_async / response_code (cloud API always includes them)
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
	rootDurations := map[string]int64{}
	totalDur := int64(0)

	for _, item := range tracing {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		dur, _ := toInt64(m["duration"])
		suid, _ := toString(m["service_uid"])
		sname, _ := toString(m["service_uname"])

		rootDurations[suid] += dur
		totalDur += dur

		// If this span has a parent with a different service_uid, it's an edge
		parentIdx, hasParent := m["parent_id"].(float64)
		if hasParent && int(parentIdx) >= 0 && int(parentIdx) < len(tracing) {
			parent := tracing[int(parentIdx)]
			if pm, ok := parent.(map[string]interface{}); ok {
				puid, _ := toString(pm["service_uid"])
				pname, _ := toString(pm["service_uname"])
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
			"client_uid":    p.clientUID,
			"client_uname":  p.clientUname,
			"server_uid":    p.serverUID,
			"server_uname":  p.serverUname,
			"duration":      dur,
			"duration_ratio": formatRatio(ratio),
		})
	}
	if paths == nil {
		paths = []interface{}{}
	}
	return paths
}

func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, true
		}
	}
	return 0, false
}

func toString(v interface{}) (string, bool) {
	switch s := v.(type) {
	case string:
		return s, true
	case json.Number:
		return s.String(), true
	}
	return "", false
}

func formatRatio(r float64) string {
	return fmt.Sprintf("%.2f", r)
}

// ---------------------------------------------------------------------------
// Handlers below are unchanged — they proxy to the algorithms service.
// ---------------------------------------------------------------------------

func handleTracingCompletion(algo *client.AlgorithmsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if algo != nil && algo.Available() {
			var req client.TracingCompletionRequest
			if json.Unmarshal(body, &req) == nil {
				result, err := algo.TracingCompletion(&req)
				if err == nil && result != nil {
					writeJSON(w, map[string]interface{}{
						"OPT_STATUS": "SUCCESS", "TYPE": result.Type,
						"DATA": result.Data, "DESCRIPTION": result.Description,
					})
					return
				}
			}
		}
		writeSuccess(w, []interface{}{})
	}
}

func handleTracingAlgoParams(algo *client.AlgorithmsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if algo != nil && algo.Available() {
			params, err := algo.TracingAlgoParams()
			if err == nil && params != nil {
				writeSuccess(w, params)
				return
			}
		}
		writeSuccess(w, map[string]interface{}{
			"network_delay_us": 50000, "host_clock_offset_us": 10000, "max_iteration": 30,
		})
	}
}
