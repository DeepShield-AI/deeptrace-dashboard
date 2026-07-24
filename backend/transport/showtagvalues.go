package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RegisterShowTagValues registers the ShowTagValues endpoint.
func RegisterShowTagValues(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/stats/querier/DBDescription/ShowTagValues", handleShowTagValues(deps))
}

// svRequest is the request body for ShowTagValues.
type svRequest struct {
	Database string `json:"DATABASE"`
	Table    string `json:"TABLE"`
	Tag      string `json:"TAG"`
	Like     string `json:"LIKE"`
	Offset   int    `json:"OFFSET"`
	Limit    *int   `json:"LIMIT"`
}

func handleShowTagValues(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		var req svRequest
		if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		if req.Database == "" {
			req.Database = "flow_log"
		}
		if req.Table == "" {
			req.Table = "l7_flow_log"
		}

		// 1. Try cache first.
		if deps.Cache != nil {
			if cached := deps.Cache.FindWithBody(r.Method, r.URL.RequestURI(), bodyStr); cached != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Write(cached)
				return
			}
		}

		// 2. Try ClickHouse direct query.
		if data := chQueryShowTagValues(req); data != nil {
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS":  "SUCCESS",
				"DESCRIPTION": "",
				"DATA":        data,
				"TYPE":        "DBDescription",
				"SCHEMAS":     map[string]interface{}{},
			})
			return
		}

		// 3. Fallback: empty.
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS":  "SUCCESS",
			"DESCRIPTION": "",
			"DATA":        []interface{}{},
			"TYPE":        "DBDescription",
			"SCHEMAS":     map[string]interface{}{},
		})
	}
}

// ---------------------------------------------------------------------------
// ClickHouse direct query for ShowTagValues
// ---------------------------------------------------------------------------

// chQueryShowTagValues queries ClickHouse for tag values.
// Returns nil if CH is unreachable or error.
func chQueryShowTagValues(req svRequest) []interface{} {
	db := req.Database
	tbl := req.Table
	tag := req.Tag

	// Normalize table name (flow_metrics tables use .1m suffix).
	resolvedDB := db
	resolvedTbl := tbl
	if db == "flow_metrics" && !strings.Contains(tbl, ".") {
		resolvedTbl = tbl + ".1m"
	}

	// Step 1: check if the tag has enum values in system.columns.comment.
	if values := queryEnumFromComment(resolvedDB, resolvedTbl, tag, req.Like, limitVal(req.Limit)); values != nil {
		return values
	}

	// Step 2: check if the tag is a resource tag known in flow_tag mapping.
	if values := queryResourceTag(req, resolvedDB, resolvedTbl, tag, req.Like, limitVal(req.Limit)); values != nil {
		return values
	}

	// Step 3: resolve to actual column name and query DISTINCT values.
	colName := resolveColumnName(req.Database, resolvedTbl, tag)
	if colName == "" {
		colName = tag
	}

	return queryDistinctValues(req, colName, req.Like, limitVal(req.Limit))
}

// ---------------------------------------------------------------------------
// Enum column handling — parse system.columns.comment
// ---------------------------------------------------------------------------

// enumEntry holds a parsed enum value+display+description.
type enumEntry struct {
	Value       interface{} `json:"value"`
	DisplayName string      `json:"display_name"`
	Description string      `json:"description"`
}

// queryEnumFromComment queries system.columns.comment for the given tag.
// If the comment contains key:value pairs, parses them as enum entries.
func queryEnumFromComment(db, tbl, tag, likeFilter string, limit int) []interface{} {
	query := fmt.Sprintf("SELECT comment, type FROM system.columns WHERE database='%s' AND table='%s' AND name='%s'", db, tbl, tag)
	rows, err := chHTTPQuery(query)
	if err != nil || len(rows) == 0 {
		return nil
	}

	comment := getSVStr(rows[0], "comment")
	if comment == "" {
		return nil
	}

	colType := getSVStr(rows[0], "type")

	// Parse comment for enum patterns: "0:normal, 1:error" or "0:正常, 1:异常"
	// Also handles "响应状态 0:正常, 1:异常" (prefix text before first digit:value)
	entries := parseEnumComment(comment, colType)
	if len(entries) == 0 {
		return nil
	}

	// Apply LIKE filter.
	filtered := applyLikeToEntries(entries, likeFilter, tag)

	// Sort by value.
	sort.Slice(filtered, func(i, j int) bool {
		return fmt.Sprintf("%v", filtered[i].Value) < fmt.Sprintf("%v", filtered[j].Value)
	})

	// Apply limit.
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	result := make([]interface{}, len(filtered))
	for i, e := range filtered {
		result[i] = e
	}
	return result
}

// parseEnumComment parses a column comment for enum patterns.
// Recognized formats:
//   "0:正常, 1:异常, 2:超时"
//   "响应状态 0:正常, 1:异常"
//   "0:N/A, 20:http1, 21:http2"
//   "0:未知 1:其他, 20:http1"  (space-separated + comma-separated mixed)
//   "0:正常, 1:异常 ,2:不存在，3:服务端异常"  (fullwidth Chinese commas)
func parseEnumComment(comment, colType string) []enumEntry {
	// Try JSON-like descriptions first: {"0":"正常","1":"异常"}
	if strings.HasPrefix(comment, "{") {
		var m map[string]string
		if json.Unmarshal([]byte(comment), &m) == nil {
			var entries []enumEntry
			for k, v := range m {
				entries = append(entries, enumEntry{Value: parseEnumValue(k, colType), DisplayName: v})
			}
			return entries
		}
	}

	// Normalize: replace Chinese punctuation with ASCII.
	normalized := comment
	normalized = strings.ReplaceAll(normalized, "，", ",")
	normalized = strings.ReplaceAll(normalized, "：", ":")
	normalized = strings.ReplaceAll(normalized, "；", ";")
	normalized = strings.ReplaceAll(normalized, "、", ",")

	// Strip leading text before the first digit+colon pattern.
	// e.g., "响应状态 0:正常" → "0:正常"
	cleaned := normalized
	reFirst := regexp.MustCompile(`\b\d+\s*:`)
	loc := reFirst.FindStringIndex(cleaned)
	if loc != nil && loc[0] > 0 {
		prefix := strings.TrimSpace(normalized[:loc[0]])
		if !strings.Contains(prefix, ":") && !strings.HasPrefix(prefix, "http") {
			cleaned = normalized[loc[0]:]
		}
	}

	var entries []enumEntry

	// Split into key:value parts.
	parts := splitEnumParts(cleaned)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, ":") {
			continue
		}
		colonIdx := strings.Index(part, ":")
		if colonIdx < 0 {
			continue
		}
		keyStr := strings.TrimSpace(part[:colonIdx])
		valStr := strings.TrimSpace(part[colonIdx+1:])
		if keyStr == "" {
			continue
		}
		entries = append(entries, enumEntry{
			Value:       parseEnumValue(keyStr, colType),
			DisplayName: valStr,
			Description: "",
		})
	}

	return entries
}

// splitEnumParts splits a normalized enum comment into key:value parts.
// Handles mixed separators: commas and space-separated entries.
// e.g., "0:未知 1:其他, 20:http1" → ["0:未知", "1:其他", "20:http1"]
// Go's regexp (RE2) doesn't support lookahead, so we use string scanning.
func splitEnumParts(comment string) []string {
	// First normalize: replace Chinese punctuation.
	normalized := strings.ReplaceAll(comment, "，", ",")
	normalized = strings.ReplaceAll(normalized, "：", ":")
	normalized = strings.ReplaceAll(normalized, "；", ";")

	// Strategy: scan the string, grouping characters into "key:value" segments.
	// Entries are separated by commas, semicolons, or spaces (when between
	// digit+colon patterns).
	var parts []string
	current := strings.Builder{}
	runes := []rune(normalized)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case ch == ',' || ch == ';':
			// Separator: flush current segment.
			if current.Len() > 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
			}
			// separator only — no isInValue tracking needed

		case ch == ' ' || ch == '\t':
			// Space: could be separator between entries, or inside a value.
			// Check if next non-space char is a digit followed by ':'.
			nextIdx := i + 1
			for nextIdx < len(runes) && (runes[nextIdx] == ' ' || runes[nextIdx] == '\t') {
				nextIdx++
			}
			if nextIdx < len(runes) && nextIdx+1 < len(runes) &&
				isDigit(runes[nextIdx]) && runes[nextIdx+1] == ':' {
				// Space before a "digit:" sequence → flush as separator.
				if current.Len() > 0 {
					parts = append(parts, strings.TrimSpace(current.String()))
					current.Reset()
				}
			} else if current.Len() > 0 {
				// Inside a value, keep the space.
				current.WriteRune(ch)
			}

		case ch == ':':
			current.WriteRune(ch)

		default:
			current.WriteRune(ch)
		}
	}
	// Flush last segment.
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}

// isDigit reports whether a rune is a decimal digit.
func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// parseEnumValue converts a string key to the appropriate type.
func parseEnumValue(s, colType string) interface{} {
	// Try integer first.
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		if strings.Contains(strings.ToLower(colType), "uint") {
			return uint64(i)
		}
		return i
	}
	// Try float.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	// Return as string.
	return s
}

// applyLikeToEntries filters enum entries based on LIKE expression.
func applyLikeToEntries(entries []enumEntry, like, tag string) []enumEntry {
	if like == "" {
		return entries
	}

	// Parse equality filter: `tag_name`=value
	eqRe := regexp.MustCompile("`([^`]+)`\\s*=\\s*(.+)")
	if m := eqRe.FindStringSubmatch(like); m != nil {
		targetTag := m[1]
		targetVal := strings.TrimSpace(m[2])
		if targetTag == tag {
			var filtered []enumEntry
			for _, e := range entries {
				if fmt.Sprintf("%v", e.Value) == targetVal {
					filtered = append(filtered, e)
				}
			}
			return filtered
		}
	}

	// For exist() filters on enum columns, just return all (no filter needed).
	existRe := regexp.MustCompile(`exist\([^)]+\)`)
	if existRe.MatchString(like) {
		return entries
	}

	return entries
}

// ---------------------------------------------------------------------------
// Resource tag handling — query flow_tag mapping tables
// ---------------------------------------------------------------------------

// resourceTagInfo describes how to query a resource tag.
type resourceTagInfo struct {
	FlowTagTable  string // flow_tag table name
	FlowTagIDCol  string // ID column name in flow_tag table
	FlowTagNameCol string // name column name in flow_tag table
	FlowLogIDCol  string // column in flow_log (with _0/_1 suffix)
	MetricsIDCol  string // column in flow_metrics
}

// resourceTagMap maps resource tag names to their metadata.
var resourceTagMap = map[string]resourceTagInfo{
	"region":       {"region_map", "id", "name", "region_id", "region_id"},
	"az":           {"az_map", "id", "name", "az_id", "az_id"},
	"vpc":          {"l3_epc_map", "id", "name", "epc_id", "l3_epc_id"},
	"l2_vpc":       {"l3_epc_map", "id", "name", "epc_id", "l3_epc_id"},
	"chost":        {"chost_map", "id", "name", "l3_device_id", "l3_device_id"},
	"host":         {"chost_map", "id", "name", "l3_device_id", "l3_device_id"},
	"pod_service":  {"pod_service_map", "id", "name", "pod_service_id", "pod_service_id"},
	"pod_cluster":  {"pod_cluster_map", "id", "name", "pod_cluster_id", "pod_cluster_id"},
	"pod_group":    {"pod_group_map", "id", "name", "pod_group_id", "pod_group_id"},
	"pod_ns":       {"pod_ns_map", "id", "name", "pod_ns_id", "pod_ns_id"},
	"subnet":       {"", "", "", "subnet_id", "subnet_id"},
	"router":       {"", "", "", "router_id", "router_id"},
	"lb":           {"","","","lb_id", "lb_id"},
	"lb_listener":  {"lb_listener_map", "id", "name", "lb_listener_id", "lb_listener_id"},
	"pod_node":     {"pod_node_map", "id", "name", "pod_node_id", "pod_node_id"},
	"pod_ingress":  {"pod_ingress_map", "id", "name", "pod_ingress_id", "pod_ingress_id"},
	"nat_gateway":  {"", "", "", "nat_gateway_id", "nat_gateway_id"},
	"service":      {"biz_service_map", "id", "name", "biz_service_id", "biz_service_id"},
}

// queryResourceTag queries tag values from flow_tag mapping tables or data table.
func queryResourceTag(req svRequest, db, tbl, tag, like string, limit int) []interface{} {
	info, ok := resourceTagMap[tag]
	if !ok {
		return nil
	}

	// Try flow_tag mapping table first.
	if info.FlowTagTable != "" {
		if values := queryFlowTagTable(info.FlowTagTable, info.FlowTagIDCol, info.FlowTagNameCol, limit); values != nil {
			return values
		}
	}

	// If no flow_tag data, query DISTINCT from the data table.
	colName := ""
	if db == "flow_log" && info.FlowLogIDCol != "" {
		colName = info.FlowLogIDCol
	} else if db == "flow_metrics" && info.MetricsIDCol != "" {
		colName = info.MetricsIDCol
	}
	if colName != "" {
		return queryDistinctValues(req, colName, like, limit)
	}

	return nil
}

// queryFlowTagTable queries a flow_tag mapping table for ID → name pairs.
func queryFlowTagTable(table, idCol, nameCol string, limit int) []interface{} {
	q := fmt.Sprintf("SELECT %s, %s FROM flow_tag.%s WHERE %s != 0 ORDER BY %s", idCol, nameCol, table, idCol, idCol)
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := chHTTPQuery(q)
	if err != nil || len(rows) == 0 {
		return nil
	}

	result := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		id := row[idCol]
		name := getSVStr(row, nameCol)
		if name == "" {
			name = fmt.Sprintf("%v", id)
		}
		result = append(result, enumEntry{
			Value:       id,
			DisplayName: name,
			Description: "",
		})
	}
	return result
}

// ---------------------------------------------------------------------------
// Direct DISTINCT value queries
// ---------------------------------------------------------------------------

// resolveColumnName maps a tag name to the actual ClickHouse column name.
func resolveColumnName(db, tbl, tag string) string {
	// For flow_log, mapping resource tags to ID columns.
	if db == "flow_log" {
		if info, ok := resourceTagMap[tag]; ok && info.FlowLogIDCol != "" {
			return info.FlowLogIDCol
		}
	}

	// For flow_metrics, mapping resource tags to single column.
	if db == "flow_metrics" {
		if info, ok := resourceTagMap[tag]; ok && info.MetricsIDCol != "" {
			return info.MetricsIDCol
		}
	}

	// Column name mappings from the existing querier system.
	colMap := map[string]string{
		"auto_service":     "app_service",
		"auto_instance":    "app_instance",
		"auto_service_0":   "auto_service_id_0",
		"auto_service_1":   "auto_service_id_1",
		"auto_instance_0":  "auto_instance_id_0",
		"auto_instance_1":  "auto_instance_id_1",
		"service_id_0":     "auto_service_id_0",
		"service_id_1":     "auto_service_id_1",
		"instance_id_0":    "auto_instance_id_0",
		"instance_id_1":    "auto_instance_id_1",
		"chost_id":         "l3_device_id",
		"vpc_id":           "epc_id",
		"pod_service_id":   "pod_service_id",
		"pod_cluster_id":   "pod_cluster_id",
		"pod_group_id":     "pod_group_id",
		"pod_ns_id":        "pod_ns_id",
		"event_type":       "l7_protocol",
		"event_desc":       "request_resource",
	}

	if mapped, ok := colMap[tag]; ok {
		return mapped
	}

	return tag
}

// queryDistinctValues queries DISTINCT values of a column from the data table.
func queryDistinctValues(req svRequest, colName, like string, limit int) []interface{} {
	db := req.Database
	tbl := req.Table

	// Normalize table.
	resolvedTbl := tbl
	if db == "flow_metrics" && !strings.Contains(tbl, ".") {
		resolvedTbl = tbl + ".1m"
	}

	isFlowLog := db == "flow_log"

	// Build WHERE conditions.
	var wheres []string

	// Check if column is numeric (use != 0 filter for ID columns).
	isIDColumn := strings.HasSuffix(colName, "_id")
	isSideColumn := strings.HasSuffix(colName, "_0") || strings.HasSuffix(colName, "_1")

	if isIDColumn || isSideColumn {
		// For ID columns, exclude 0 (no value).
		wheres = append(wheres, fmt.Sprintf("`%s` != 0", colName))
	}

	// Parse LIKE filter for additional WHERE conditions.
	likeWheres := parseLikeToWhere(like, colName)
	wheres = append(wheres, likeWheres...)

	// Build and execute DISTINCT query.
	whereClause := ""
	if len(wheres) > 0 {
		whereClause = " WHERE " + strings.Join(wheres, " AND ")
	}

	q := fmt.Sprintf("SELECT DISTINCT `%s` AS v FROM `%s`.`%s`%s ORDER BY v", colName, db, resolvedTbl, whereClause)

	// For flow_log with _0/_1 side columns, we need to query both sides.
	if isFlowLog && !strings.HasSuffix(colName, "_0") && !strings.HasSuffix(colName, "_1") && isIDColumn {
		// Query both _0 and _1 sides.
		q = fmt.Sprintf(
			"SELECT DISTINCT v FROM (SELECT `%s_0` AS v FROM `%s`.`%s` WHERE `%s_0` != 0 UNION ALL SELECT `%s_1` AS v FROM `%s`.`%s` WHERE `%s_1` != 0) ORDER BY v",
			colName, db, resolvedTbl, colName, colName, db, resolvedTbl, colName,
		)
		// Add LIKE conditions to both subqueries if they reference the side column.
		if len(likeWheres) > 0 {
			lw := strings.Join(likeWheres, " AND ")
			q = fmt.Sprintf(
				"SELECT DISTINCT v FROM (SELECT `%s_0` AS v FROM `%s`.`%s` WHERE `%s_0` != 0 AND %s UNION ALL SELECT `%s_1` AS v FROM `%s`.`%s` WHERE `%s_1` != 0 AND %s) ORDER BY v",
				colName, db, resolvedTbl, colName, lw, colName, db, resolvedTbl, colName, lw,
			)
		}
	}

	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := chHTTPQuery(q)
	if err != nil || len(rows) == 0 {
		return nil
	}

	result := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		val := row["v"]
		result = append(result, enumEntry{
			Value:       val,
			DisplayName: fmt.Sprintf("%v", val),
			Description: "",
		})
	}
	return result
}

// ---------------------------------------------------------------------------
// LIKE filter parsing
// ---------------------------------------------------------------------------

// parseLikeToWhere converts a LIKE expression to SQL WHERE conditions.
// Supported formats:
//   `col`=value        → col = value
//   exist(col)         → col != 0 (for ID columns)
//   expr AND expr      → combined with AND
//   expr OR expr       → combined with OR
func parseLikeToWhere(like, currentTag string) []string {
	if like == "" {
		return nil
	}

	var wheres []string

	// Handle AND expressions.
	parts := strings.Split(like, " AND ")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Handle equality: `col`=value
		eqRe := regexp.MustCompile("`([^`]+)`\\s*=\\s*(.+)")
		if m := eqRe.FindStringSubmatch(part); m != nil {
			col := strings.TrimSpace(m[1])
			val := strings.TrimSpace(m[2])
			wheres = append(wheres, fmt.Sprintf("`%s` = %s", col, val))
			continue
		}

		// Handle exist(col): check if column is non-zero (for ID columns).
		existRe := regexp.MustCompile(`exist\(([^)]+)\)`)
		if m := existRe.FindStringSubmatch(part); m != nil {
			col := strings.TrimSpace(m[1])
			wheres = append(wheres, fmt.Sprintf("`%s` != 0", col))
			continue
		}

		// Pass through as raw SQL.
		wheres = append(wheres, part)
	}

	return wheres
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// limitVal returns the limit value or 0 if nil.
func limitVal(limit *int) int {
	if limit == nil {
		return 0
	}
	return *limit
}

// getSVStr safely extracts a string from a map.
func getSVStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// minInt returns the smaller of a and b.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure unused import silencing (these are used transitively).
var _ = log.Printf
