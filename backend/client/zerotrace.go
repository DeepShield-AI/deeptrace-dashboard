package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"deeptrace-backend/logging"
)

// ZerotraceService is an HTTP client for zerotrace-server's /v1/query/ endpoint.
type ZerotraceService struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// zerotraceResponse is the top-level response envelope from zerotrace-server.
type zerotraceResponse struct {
	OPT_STATUS  string         `json:"OPT_STATUS"`
	Description string         `json:"DESCRIPTION"`
	Result      *ztQueryResult `json:"result"`
}

type ztQueryResult struct {
	Columns []string         `json:"columns"`
	Values  [][]interface{}  `json:"values"`
	Schemas []ztColumnSchema `json:"schemas"`
}

type ztColumnSchema struct {
	Unit      string `json:"unit"`
	Type      int    `json:"type"`
	ValueType string `json:"value_type"`
	PreAs     string `json:"pre_as"`
	LabelType string `json:"label_type"`
}

// NewZerotrace creates a client for the zerotrace-server.
// addr should be "host:port", e.g. "localhost:20416".
func NewZerotrace(addr string) *ZerotraceService {
	if addr == "" {
		return nil
	}
	baseURL := fmt.Sprintf("http://%s", addr)
	return &ZerotraceService{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		timeout: 30 * time.Second,
	}
}

// Available returns true if the service was configured (non-nil).
func (z *ZerotraceService) Available() bool {
	return z != nil && z.baseURL != ""
}

// QueryResult holds the raw result from zerotrace-server query.
type QueryResult struct {
	OptStatus string // SUCCESS, PARTIAL_RESULT — from deepflow-server
	Columns   []string
	Values    [][]interface{}
	Schemas   []ztColumnSchema
}

// Query sends a SQL query to zerotrace-server and returns the result as rows.
func (z *ZerotraceService) Query(db, sql string) ([]map[string]interface{}, error) {
	raw, err := z.queryRaw(db, sql)
	if err != nil {
		return nil, err
	}
	// Transform columns + values -> []map[string]interface{}.
	result := make([]map[string]interface{}, 0, len(raw.Values))
	for _, row := range raw.Values {
		r := make(map[string]interface{}, len(raw.Columns))
		for i, col := range raw.Columns {
			if i < len(row) {
				r[col] = row[i]
			}
		}
		result = append(result, r)
	}
	return result, nil
}

// QueryRaw sends a SQL query and returns the raw result with columns/values/schemas.
func (z *ZerotraceService) QueryRaw(db, sql string) (*QueryResult, error) {
	return z.queryRaw(db, sql)
}

// queryRaw is the internal implementation shared by Query and QueryRaw.
func (z *ZerotraceService) queryRaw(db, sql string) (*QueryResult, error) {
	if !z.Available() {
		return nil, fmt.Errorf("zerotrace-server not configured")
	}

	form := url.Values{}
	form.Set("db", db)
	form.Set("sql", sql)

	reqURL := z.baseURL + "/v1/query/"
	logging.Debugf("ZT query db=%s sql=%s", db, sql[:min(len(sql), 120)])

	resp, err := z.httpClient.PostForm(reqURL, form)
	if err != nil {
		return nil, fmt.Errorf("zerotrace query failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("zerotrace read failed: %w", err)
	}
	// Check the HTTP status before decoding JSON: a 4xx/5xx body is an error
	// payload, not a query result — misreporting it as a parse failure hides
	// the server-side problem.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("zerotrace query failed: http %d: %s", resp.StatusCode, snippet)
	}

	var ztResp zerotraceResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&ztResp); err != nil {
		return nil, fmt.Errorf("zerotrace parse failed: %w", err)
	}

	if ztResp.OPT_STATUS != "SUCCESS" && ztResp.OPT_STATUS != "PARTIAL_RESULT" || ztResp.Result == nil {
		return nil, fmt.Errorf("zerotrace query error: %s", ztResp.Description)
	}

	return &QueryResult{
		OptStatus: ztResp.OPT_STATUS,
		Columns:   ztResp.Result.Columns,
		Values:    ztResp.Result.Values,
		Schemas:   ztResp.Result.Schemas,
	}, nil
}
