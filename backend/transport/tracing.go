package transport

import (
	"encoding/json"
	"net/http"

	"deeptrace-backend/client"
	"deeptrace-backend/logging"
	"deeptrace-backend/query/tracing"
)

// RegisterTracing registers tracing algorithm endpoints.
func RegisterTracing(mux *http.ServeMux, algo *client.AlgorithmsService) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/L4FlowTracing", handleL7FlowTracing(algo))
	mux.HandleFunc("/api/statistics/v1/stats/querier/L7FlowTracing", handleL7FlowTracing(algo))
	mux.HandleFunc("/api/deepflow-app/v1/stats/querier/L7FlowTracing", handleL7FlowTracing(algo))
	mux.HandleFunc("/api/statistics/v1/stats/querier/tracing-completion-by-external-app-spans", handleTracingCompletion(algo))
	mux.HandleFunc("/api/statistics/v1/stats/querier/TracingAlgoParams", handleTracingAlgoParams(algo))
}

func handleL7FlowTracing(algo *client.AlgorithmsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readRawBody(r)
		if err != nil {
			writeError(w, "cannot read body")
			return
		}

		if algo != nil && algo.Available() {
			var raw tracing.L7FlowTracingRequestRaw
			if uerr := json.Unmarshal(body, &raw); uerr != nil {
				logging.Errorf("L7FlowTracing unmarshal error: %v", uerr)
			} else {
				req := raw.Normalize()
				result, err := algo.L7FlowTracing(req)
				if err != nil {
					logging.Errorf("L7FlowTracing algorithm error: %v", err)
				} else if result != nil {
					env := tracing.PostProcessResult(result)
					writeJSON(w, env)
					return
				}
			}
		}

		// Fallback: matching the cloud API response structure.
		logging.Warnf("L7FlowTracing: algorithms unavailable, returning empty")
		writeJSON(w, tracing.EmptyResult())
	}
}

// ---------------------------------------------------------------------------
// Handlers below are unchanged — they proxy to the algorithms service.
// ---------------------------------------------------------------------------

func handleTracingCompletion(algo *client.AlgorithmsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[client.TracingCompletionRequest](r)
		if err != nil {
			writeSuccess(w, []interface{}{})
			return
		}
		req := *parsed
		if algo != nil && algo.Available() {
			result, err := algo.TracingCompletion(&req)
			if err == nil && result != nil {
				writeJSON(w, map[string]interface{}{
					"OPT_STATUS": "SUCCESS", "TYPE": result.Type,
					"DATA": result.Data, "DESCRIPTION": result.Description,
				})
				return
			}
		}
		writeSuccess(w, []interface{}{})
	}
}

func handleTracingAlgoParams(algo *client.AlgorithmsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var params map[string]interface{}
		if algo != nil && algo.Available() {
			p, err := algo.TracingAlgoParams()
			if err == nil && p != nil {
				params = map[string]interface{}{
					"network_delay_us":     p.NetworkDelayUS,
					"host_clock_offset_us": p.HostClockOffset,
					"max_iteration":        p.MaxIteration,
				}
			}
		}
		if params == nil {
			params = map[string]interface{}{
				"network_delay_us": 50000, "host_clock_offset_us": 10000, "max_iteration": 30,
			}
		}
		// The algorithms service / cloud API include TYPE: "TracingAlgoParams".
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS":  "SUCCESS",
			"DESCRIPTION": "",
			"DATA":        params,
			"TYPE":        "TracingAlgoParams",
		})
	}
}
