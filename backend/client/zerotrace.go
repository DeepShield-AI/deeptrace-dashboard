package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ZerotraceService is an HTTP client for zerotrace-server's /v1/query/ endpoint.
type ZerotraceService struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// zerotraceResponse is the top-level response envelope from zerotrace-server.
type zerotraceResponse struct {
	OPT_STATUS  string          `json:"OPT_STATUS"`
	Description string          `json:"DESCRIPTION"`
	Result      *ztQueryResult  `json:"result"`
}

type ztQueryResult struct {
	Columns []string          `json:"columns"`
	Values  [][]interface{}   `json:"values"`
	Schemas []ztColumnSchema  `json:"schemas"`
}

type ztColumnSchema struct {
	Unit       string `json:"unit"`
	Type       int    `json:"type"`
	ValueType  string `json:"value_type"`
	PreAs      string `json:"pre_as"`
	LabelType  string `json:"label_type"`
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
	Columns []string
	Values  [][]interface{}
	Schemas []ztColumnSchema
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
	log.Printf("🔍 ZT query db=%s sql=%s", db, sql[:min(len(sql), 120)])

	resp, err := z.httpClient.PostForm(reqURL, form)
	if err != nil {
		return nil, fmt.Errorf("zerotrace query failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("zerotrace read failed: %w", err)
	}

	var ztResp zerotraceResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&ztResp); err != nil {
		return nil, fmt.Errorf("zerotrace parse failed: %w", err)
	}

	if ztResp.OPT_STATUS != "SUCCESS" || ztResp.Result == nil {
		return nil, fmt.Errorf("zerotrace query error: %s", ztResp.Description)
	}

	return &QueryResult{
		Columns: ztResp.Result.Columns,
		Values:  ztResp.Result.Values,
		Schemas: ztResp.Result.Schemas,
	}, nil
}

// QueryFlowLogs is a convenience wrapper for querying l7_flow_log.
func (z *ZerotraceService) QueryFlowLogs(db, table, where string, timeStart, timeEnd int64) ([]map[string]interface{}, error) {
	var clauses []string
	if timeStart > 0 {
		clauses = append(clauses, fmt.Sprintf("time >= %d", timeStart))
	}
	if timeEnd > 0 {
		clauses = append(clauses, fmt.Sprintf("time <= %d", timeEnd))
	}
	if where != "" {
		clauses = append(clauses, where)
	}

	fullWhere := strings.Join(clauses, " AND ")
	sql := fmt.Sprintf("SELECT * FROM %s", table)
	if fullWhere != "" {
		sql += " WHERE " + fullWhere
	}
	sql += " LIMIT 500"

	return z.Query(db, sql)
}

// ShowDatabases returns the list of databases.
func (z *ZerotraceService) ShowDatabases() ([]map[string]interface{}, error) {
	return z.Query("", "show databases")
}

// ShowTables returns the list of tables in a database.
func (z *ZerotraceService) ShowTables(db string) ([]map[string]interface{}, error) {
	return z.Query(db, "show tables")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure the import is used.
