package showtagvalues

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"deeptrace-backend/query/showmetrics"
)

type SvRequest struct {
	Database string `json:"DATABASE"`
	Table    string `json:"TABLE"`
	Tag      string `json:"TAG"`
	Like     string `json:"LIKE"`
	Offset   int    `json:"OFFSET"`
	Limit    *int   `json:"LIMIT"`
}



// ---------------------------------------------------------------------------
// ClickHouse direct query for ShowTagValues
// ---------------------------------------------------------------------------

// chQueryShowTagValues queries ClickHouse for tag values.
// Returns nil if CH is unreachable or error.
func ChQueryShowTagValues(req SvRequest) []interface{} {
	db := req.Database
	tbl := req.Table
	tag := req.Tag

	// Normalize table name (flow_metrics tables use .1m suffix).
	resolvedDB := db
	resolvedTbl := tbl
	if db == "flow_metrics" && !strings.Contains(tbl, ".") {
		resolvedTbl = tbl + ".1m"
	}

	// Step 1: check if the tag has known values in flow_tag.int_enum_map (authoritative).
	if values := QueryEnumFromFlowTag(tag, req.Like, LimitVal(req.Limit)); values != nil {
		return values
	}

	// Step 2: check if the tag has enum values in system.columns.comment (stale fallback).
	if values := QueryEnumFromComment(resolvedDB, resolvedTbl, tag, req.Like, LimitVal(req.Limit)); values != nil {
		EnrichEnumEntries(values, tag)
		return values
	}

	// Step 3: check if the tag is a resource tag known in flow_tag mapping.
	if values := QueryResourceTag(req, resolvedDB, resolvedTbl, tag, req.Like, LimitVal(req.Limit)); values != nil {
		return values
	}

	// Step 3: resolve to actual column name and query DISTINCT values.
	colName := ResolveColumnName(req.Database, resolvedTbl, tag)
	if colName == "" {
		colName = tag
	}

	data := QueryDistinctValues(req, colName, req.Like, LimitVal(req.Limit))
	// Enrich raw values with display names from int_enum_map,
	// for tags that have known enum mappings (e.g. signal_source).
	if data != nil {
		EnrichEnumEntries(data, tag)
		return data
	}

	// Step 5: hardcoded fallback for well-known tags not found in any other source.
	return QueryBuiltinEnumFallback(tag, req.Like, LimitVal(req.Limit))
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
func QueryEnumFromComment(db, tbl, tag, likeFilter string, limit int) []interface{} {
	query := fmt.Sprintf("SELECT comment, type FROM system.columns WHERE database='%s' AND table='%s' AND name='%s'", db, tbl, tag)
	rows, err := showmetrics.HTTPQuery(query)
	if err != nil || len(rows) == 0 {
		return nil
	}

	comment := GetSVStr(rows[0], "comment")
	if comment == "" {
		return nil
	}

	colType := GetSVStr(rows[0], "type")

	// Parse comment for enum patterns: "0:normal, 1:error" or "0:正常, 1:异常"
	// Also handles "响应状态 0:正常, 1:异常" (prefix text before first digit:value)
	entries := ParseEnumComment(comment, colType)
	if len(entries) == 0 {
		return nil
	}

	// Apply LIKE filter.
	filtered := ApplyLikeToEntries(entries, likeFilter, tag)

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
func ParseEnumComment(comment, colType string) []enumEntry {
	// Try JSON-like descriptions first: {"0":"正常","1":"异常"}
	if strings.HasPrefix(comment, "{") {
		var m map[string]string
		if json.Unmarshal([]byte(comment), &m) == nil {
			var entries []enumEntry
			for k, v := range m {
				entries = append(entries, enumEntry{Value: ParseEnumValue(k, colType), DisplayName: v})
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
	parts := SplitEnumParts(cleaned)
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
			Value:       ParseEnumValue(keyStr, colType),
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
func SplitEnumParts(comment string) []string {
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
				IsDigit(runes[nextIdx]) && runes[nextIdx+1] == ':' {
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
func IsDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// parseEnumValue converts a string key to the appropriate type.
func ParseEnumValue(s, colType string) interface{} {
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
func ApplyLikeToEntries(entries []enumEntry, like, tag string) []enumEntry {
	if like == "" {
		return entries
	}

	// Handle OR expressions: split by " OR " and collect union of matching values.
	orParts := strings.Split(like, " OR ")
	if len(orParts) > 1 {
		var union []enumEntry
		seen := map[string]bool{}
		for _, part := range orParts {
			filtered := ApplyLikeToEntries(entries, strings.TrimSpace(part), tag)
			for _, e := range filtered {
				key := fmt.Sprintf("%v", e.Value)
				if !seen[key] {
					seen[key] = true
					union = append(union, e)
				}
			}
		}
		return union
	}

	// Parse equality filter: `tag_name`=value(s)
	// Note: (.+) is intentionally greedy so that `col`=0 OR `col`=4 produces
	// a single SQL expression "0 OR `col`=4" which parseLikeToWhere handles.
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
// Enum tag handling — query flow_tag.int_enum_map
// ---------------------------------------------------------------------------

// queryEnumFromFlowTag queries flow_tag.int_enum_map for tag display names and descriptions.
// enrichEnumEntries enriches raw enum entries with display names from int_enum_map.
// Only entries whose values exist in the map get updated display_name/description;
// unrecognized values keep their raw display.
func EnrichEnumEntries(data []interface{}, tag string) {
	enumTag := tag
	switch tag {
	case "signal_source":
		enumTag = "l7_signal_source"
	case "protocol":
		enumTag = "l7_protocol"
	}
	q := fmt.Sprintf("SELECT value, name_zh, description_zh FROM flow_tag.int_enum_map WHERE tag_name='%s'", enumTag)
	rows, err := showmetrics.HTTPQuery(q)

	// Build lookup map: value string -> {name_zh, description_zh}.
	type enumInfo struct{ name, desc string }
	lookup := make(map[string]enumInfo)

	if err == nil {
		for _, row := range rows {
			key := fmt.Sprintf("%v", row["value"])
			lookup[key] = enumInfo{
				name: GetSVStr(row, "name_zh"),
				desc: GetSVStr(row, "description_zh"),
			}
		}
	}

	// Fallback: use builtinEnumFallback when CH int_enum_map has no data.
	if len(lookup) == 0 {
		if fb, ok := BuiltinEnumFallback[tag]; ok {
			for k, v := range fb {
				lookup[k] = enumInfo{name: v}
			}
		}
	}

	if len(lookup) == 0 {
		return
	}
	// Enrich each data entry by index to modify in place.
	for i := range data {
		if e, ok := data[i].(enumEntry); ok {
			key := fmt.Sprintf("%v", e.Value)
			if info, found := lookup[key]; found {
				data[i] = enumEntry{
					Value:       e.Value,
					DisplayName: info.name,
					Description: info.desc,
				}
			}
		}
	}
}

func QueryEnumFromFlowTag(tag, like string, limit int) []interface{} {
	// Map API tag name to int_enum_map tag name.
	// The enum map stores tags with an optional "l7_" prefix for flow_log columns,
	// e.g. signal_source → l7_signal_source.
	enumTag := tag
	switch tag {
	case "signal_source":
		enumTag = "l7_signal_source"
	case "protocol":
		enumTag = "l7_protocol"
	}
	// Try int_enum_map first.
	q := fmt.Sprintf("SELECT value, name_zh, description_zh FROM flow_tag.int_enum_map WHERE tag_name='%s' ORDER BY toUInt64(value)", enumTag)
	rows, err := showmetrics.HTTPQuery(q)
	// If int_enum_map has no data, try string_enum_map (for tags like event_type).
	if err != nil || len(rows) == 0 {
		q2 := fmt.Sprintf("SELECT value, name_zh, description_zh FROM flow_tag.string_enum_map WHERE tag_name='%s' ORDER BY value", enumTag)
		rows, err = showmetrics.HTTPQuery(q2)
	}
	if err != nil || len(rows) == 0 {
		return nil
	}

	entries := make([]enumEntry, 0, len(rows))
	for _, row := range rows {
		// Convert value to number for JSON compatibility (ClickHouse FORMAT JSON
		// returns UInt64 as string). Cloud API expects numeric JSON values.
		val := row["value"]
		if s, ok := val.(string); ok {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				val = f
			}
		}
		entries = append(entries, enumEntry{
			Value:       val,
			DisplayName: GetSVStr(row, "name_zh"),
			Description: GetSVStr(row, "description_zh"),
		})
	}

	// Apply LIKE filter.
	if like != "" {
		entries = ApplyLikeToEntries(entries, like, tag)
	}

	// Apply limit.
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	result := make([]interface{}, len(entries))
	for i, e := range entries {
		result[i] = e
	}
	return result
}

// builtinEnumFallback provides hardcoded enum values for well-known tags
// that have fixed mappings not stored in flow_tag.int_enum_map.
// Matches the fallback enum maps in enum.go and the cloud API behavior.
var BuiltinEnumFallback = map[string]map[string]string{
	"event_type": {
		"read":      "读",
		"write":     "写",
	},
	"observation_point": {
		"c": "客户端网卡", "s": "服务端网卡",
		"c-p": "客户侧网络", "s-p": "服务侧网络",
		"c-app": "客户端应用", "s-app": "服务端应用",
		"app": "应用", "rest": "其他",
		"c-gw": "客户端网关", "s-gw": "服务端网关",
	},
}

// queryBuiltinEnumFallback returns hardcoded enum values for known tags
// whose enum mappings are not stored in int_enum_map or column comments.
func QueryBuiltinEnumFallback(tag, like string, limit int) []interface{} {
	fb, ok := BuiltinEnumFallback[tag]
	if !ok || len(fb) == 0 {
		return nil
	}
	// Sort by key for stable output.
	keys := make([]string, 0, len(fb))
	for k := range fb {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	entries := make([]enumEntry, 0, len(keys))
	for _, k := range keys {
		// Use string for LowCardinality(String) tags like event_type,
		// numeric for integer-typed tags like response_status.
		val := interface{}(k)
		if f, err := strconv.ParseFloat(k, 64); err == nil {
			val = f
		}
		entries = append(entries, enumEntry{
			Value:       val,
			DisplayName: fb[k],
			Description: "",
		})
	}

	// Apply LIKE filter.
	if like != "" {
		entries = ApplyLikeToEntries(entries, like, tag)
	}

	// Apply limit.
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	result := make([]interface{}, len(entries))
	for i, e := range entries {
		result[i] = e
	}
	return result
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
var ResourceTagMap = map[string]resourceTagInfo{
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
func QueryResourceTag(req SvRequest, db, tbl, tag, like string, limit int) []interface{} {
	info, ok := ResourceTagMap[tag]
	if !ok {
		return nil
	}

	// Try flow_tag mapping table first.
	if info.FlowTagTable != "" {
		if values := QueryFlowTagTable(info.FlowTagTable, info.FlowTagIDCol, info.FlowTagNameCol, limit); values != nil {
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
		return QueryDistinctValues(req, colName, like, limit)
	}

	return nil
}

// queryFlowTagTable queries a flow_tag mapping table for ID → name pairs.
func QueryFlowTagTable(table, idCol, nameCol string, limit int) []interface{} {
	q := fmt.Sprintf("SELECT %s, %s FROM flow_tag.%s WHERE %s != 0 ORDER BY %s", idCol, nameCol, table, idCol, idCol)
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := showmetrics.HTTPQuery(q)
	if err != nil || len(rows) == 0 {
		return nil
	}

	result := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		id := row[idCol]
		name := GetSVStr(row, nameCol)
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
func ResolveColumnName(db, tbl, tag string) string {
	// For flow_log, mapping resource tags to ID columns.
	if db == "flow_log" {
		if info, ok := ResourceTagMap[tag]; ok && info.FlowLogIDCol != "" {
			return info.FlowLogIDCol
		}
	}

	// For flow_metrics, mapping resource tags to single column.
	if db == "flow_metrics" {
		if info, ok := ResourceTagMap[tag]; ok && info.MetricsIDCol != "" {
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
	}

	// flow_log-specific column mappings (event_type→l7_protocol only for flow_log).
	if db == "flow_log" {
		switch tag {
		case "event_type":
			return "l7_protocol"
		case "event_desc":
			return "request_resource"
		}
	}

	if mapped, ok := colMap[tag]; ok {
		return mapped
	}

	return tag
}

// queryDistinctValues queries DISTINCT values of a column from the data table.
func QueryDistinctValues(req SvRequest, colName, like string, limit int) []interface{} {
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
	likeWheres := ParseLikeToWhere(like, colName)
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

	rows, err := showmetrics.HTTPQuery(q)
	if err != nil || len(rows) == 0 {
		return nil
	}

	result := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		val := row["v"]
		// Convert string values to float64 for JSON numeric compatibility.
		if s, ok := val.(string); ok {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				val = f
			}
		}
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
func ParseLikeToWhere(like, currentTag string) []string {
	if like == "" {
		return nil
	}

	var wheres []string

	// Handle AND expressions first.
	andParts := strings.Split(like, " AND ")
	for _, part := range andParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Handle OR within the AND part.
		orParts := strings.Split(part, " OR ")
		if len(orParts) > 1 {
			var orClauses []string
			for _, op := range orParts {
				op = strings.TrimSpace(op)
				eqRe := regexp.MustCompile("`([^`]+)`\\s*=\\s*(.+)")
				if m := eqRe.FindStringSubmatch(op); m != nil {
					col := strings.TrimSpace(m[1])
					val := strings.TrimSpace(m[2])
					orClauses = append(orClauses, fmt.Sprintf("`%s` = %s", col, val))
				}
			}
			if len(orClauses) > 0 {
				wheres = append(wheres, "("+strings.Join(orClauses, " OR ")+")")
				continue
			}
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
func LimitVal(limit *int) int {
	if limit == nil {
		return 0
	}
	return *limit
}

// getSVStr safely extracts a string from a map.
func GetSVStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// minInt returns the smaller of a and b.
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure unused import silencing (these are used transitively).
var _ = log.Printf
