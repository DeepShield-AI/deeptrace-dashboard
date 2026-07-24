package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// AlgorithmsService is an HTTP client for zerotrace-algorithms (port 20418).
type AlgorithmsService struct {
	baseURL    string
	httpClient *http.Client
}

// NewAlgorithms creates a client for zerotrace-algorithms.
// addr should be "host:port", e.g. "localhost:20418".
func NewAlgorithms(addr string) *AlgorithmsService {
	if addr == "" {
		return nil
	}
	return &AlgorithmsService{
		baseURL: fmt.Sprintf("http://%s", addr),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Available returns true if the service was configured.
func (a *AlgorithmsService) Available() bool {
	return a != nil && a.baseURL != ""
}

// ---------------------------------------------------------------------------
// L7FlowTracingRequest matches the Python API input for L7FlowTracing.
// ---------------------------------------------------------------------------

type L7FlowTracingRequest struct {
	Region           string   `json:"REGION,omitempty"`
	TimeStart        int64    `json:"TIME_START"`
	TimeEnd          int64    `json:"TIME_END"`
	Database         string   `json:"DATABASE"`
	Table            string   `json:"TABLE"`
	ID               string   `json:"_id,omitempty"`
	TraceID          string   `json:"trace_id,omitempty"`
	Debug            bool     `json:"DEBUG,omitempty"`
	MaxIteration     int      `json:"MAX_ITERATION,omitempty"`
	NetworkDelayUS   int      `json:"NETWORK_DELAY_US,omitempty"`
	HostClockOffset  int      `json:"HOST_CLOCK_OFFSET_US,omitempty"`
	SignalSources    []string `json:"SIGNAL_SOURCES,omitempty"`
	HasAttributes    int      `json:"has_attributes,omitempty"`
}

// L7FlowTracingResponse is the top-level response from tracing API.
type L7FlowTracingResponse struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Status      string      `json:"status"`
	Data        interface{} `json:"data"`
	TsdbInfo    interface{} `json:"tsdb_info,omitempty"`
}

// ---------------------------------------------------------------------------
// TracingCompletionRequest
// ---------------------------------------------------------------------------

type AppSpan struct {
	StartTimeUS   int64  `json:"start_time_us"`
	EndTimeUS     int64  `json:"end_time_us"`
	SpanKind      int    `json:"span_kind"`
	TraceID       string `json:"trace_id"`
	SpanID        string `json:"span_id"`
	ParentSpanID  string `json:"parent_span_id"`
}

type TracingCompletionRequest struct {
	AppSpans         []AppSpan `json:"APP_SPANS"`
	MaxIteration     int       `json:"MAX_ITERATION,omitempty"`
	NetworkDelayUS   int       `json:"NETWORK_DELAY_US,omitempty"`
	Debug            bool      `json:"DEBUG,omitempty"`
	SignalSources    []string  `json:"SIGNAL_SOURCES,omitempty"`
}

type TracingCompletionResponse struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Status      string      `json:"status"`
	Data        interface{} `json:"data"`
}

// ---------------------------------------------------------------------------
// AlgoParams
// ---------------------------------------------------------------------------

type AlgoParams struct {
	NetworkDelayUS  int `json:"network_delay_us"`
	HostClockOffset int `json:"host_clock_offset_us"`
	MaxIteration    int `json:"max_iteration"`
}

// ---------------------------------------------------------------------------
// API methods
// ---------------------------------------------------------------------------

// L7FlowTracing calls the zerotrace-algorithms L7FlowTracing endpoint.
func (a *AlgorithmsService) L7FlowTracing(req *L7FlowTracingRequest) (*L7FlowTracingResponse, error) {
	if !a.Available() {
		return nil, fmt.Errorf("algorithms service not configured")
	}
	return a.doPost("/v1/stats/querier/L7FlowTracing", req)
}

// TracingCompletion calls the tracing completion endpoint.
func (a *AlgorithmsService) TracingCompletion(req *TracingCompletionRequest) (*TracingCompletionResponse, error) {
	if !a.Available() {
		return nil, fmt.Errorf("algorithms service not configured")
	}
	resp, err := a.doPost("/v1/stats/querier/tracing-completion-by-external-app-spans", req)
	if err != nil {
		return nil, err
	}
	return &TracingCompletionResponse{
		Type:        resp.Type,
		Description: resp.Description,
		Status:      resp.Status,
		Data:        resp.Data,
	}, nil
}

// TracingAlgoParams gets algorithm parameters.
func (a *AlgorithmsService) TracingAlgoParams() (*AlgoParams, error) {
	if !a.Available() {
		return nil, fmt.Errorf("algorithms service not configured")
	}

	resp, err := a.httpClient.Get(a.baseURL + "/v1/stats/querier/TracingAlgoParams")
	if err != nil {
		return nil, fmt.Errorf("algorithms GET failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("algorithms read failed: %w", err)
	}

	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("algorithms parse failed: %w", err)
	}

	var params AlgoParams
	if err := json.Unmarshal(wrapper.Data, &params); err != nil {
		return nil, fmt.Errorf("algorithms params parse failed: %w", err)
	}
	return &params, nil
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (a *AlgorithmsService) doPost(path string, reqBody interface{}) (*L7FlowTracingResponse, error) {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("algorithms marshal failed: %w", err)
	}

	url := a.baseURL + path
	log.Printf("🔬 ALGO POST %s body=%d", path, len(data))

	httpResp, err := a.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("algorithms POST failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("algorithms read failed: %w", err)
	}

	var resp L7FlowTracingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("algorithms parse failed: %w", err)
	}
	return &resp, nil
}

// Ensure the import is used.
