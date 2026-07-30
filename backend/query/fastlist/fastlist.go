package fastlist

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/query/showmetrics"
	"deeptrace-backend/query/showtagvalues"
)

type FastListRequest struct {
	DB         string `json:"db"`
	Table      string `json:"table"`
	TimeStart  int64  `json:"time_start"`
	TimeEnd    int64  `json:"time_end"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	DataSource string `json:"data_source"`
	Where      *struct {
		ResourceSets []struct {
			ID        string `json:"id"`
			Condition []interface{} `json:"condition"` // flat [{key,op,val}] or nested AND/OR tree
		} `json:"resourceSets"`
		Paths []map[string]string `json:"paths"`
	} `json:"where"`
}

type FastListDebugInfo struct {
	requestBody []byte
	db          string
	table       string
	selStr      string
	sel         string // full SELECT clause including Count(row)
	extras      []string
	limit       int
	offset      int
	sql         string // final SQL sent to ZT
	queryStart  time.Time
	queryEnd    time.Time
	result      *client.QueryResult
	err         error
}


// AND/OR condition tree (sent by the frontend in QuerierJs format).
func FlattenFastListConditions(conds []interface{}, db string) []string {
	var result []string
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		// Leaf condition: has "key" field
		if key, hasKey := m["key"]; hasKey {
			op, _ := m["op"].(string)
			if op == "" {
				op = "="
			}
			col := fmt.Sprintf("%v", key)
			val := m["val"]

		// Skip columns that dont exist in raw ClickHouse.
		if _, skip := clickhouse.FastListSkipCols[col]; skip {
			continue
		}
			// Virtual tag (String) compared to number: use the physical ID column.
			if physicalCol := clickhouse.IDColumn(col); physicalCol != col {
				if _, isNum := val.(float64); isNum {
					col = physicalCol
					if db == "flow_log" {
						col += "_0"
					}
				}
			}

			// Quote string values, leave numeric values unquoted.
			var valStr string
			if s, ok := val.(string); ok {
				valStr = "'" + s + "'"
			} else {
				valStr = fmt.Sprintf("%v", val)
			}

			result = append(result, "`" + col + "` " + op + " " + valStr)
			continue
		}
		// Branch condition: has "val" array (nested children)
		if val, hasVal := m["val"]; hasVal {
			if children, ok := val.([]interface{}); ok {
				result = append(result, FlattenFastListConditions(children, db)...)
			}
		}
	}
	return result
}

// Column info for a tag or metric in the QuerierJs intermediate response.
type QuerierColumn struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	IsResource  bool   `json:"isResource"`
	Type        int    `json:"type,omitempty"`
	Unit        string `json:"unit,omitempty"`
}

// E.g., Enum(observation_point) → observation_point, node_type(x) → x.
func NormalizeFastListSelect(sel string) string {
	result := sel
	for _, fn := range []string{"Enum", "node_type", "icon_id", "newTag"} {
		lower := strings.ToLower(result)
		idx := strings.Index(lower, strings.ToLower(fn)+"(")
		for idx >= 0 {
			end := idx + len(fn) + 1
			depth := 1
			for end < len(result) && depth > 0 {
				if result[end] == '(' { depth++ }
				if result[end] == ')' { depth-- }
				end++
			}
			inner := strings.TrimSpace(result[idx+len(fn)+1 : end-1])
			result = result[:idx] + inner + result[end:]
			lower = strings.ToLower(result)
			idx = strings.Index(lower, strings.ToLower(fn)+"(")
		}
	}
	return result
}


func ChQueryFastList(ch *clickhouse.CHService, db, tbl, selStr string, extras []string,
	timeStart, timeEnd int64, limit, offset int) []interface{} {
	if ch == nil || !ch.Enabled() {
		return nil
	}
	// Build a ClickHouse-compatible SQL.
	chSel := NormalizeFastListSelect(selStr)
	// Deduplicate after stripping DSL functions (Enum(x) and x both become x).
	seenSel := map[string]bool{}
	var dedupParts []string
	for _, p := range strings.Split(chSel, ",") {
		p = strings.TrimSpace(p)
		if !seenSel[p] {
			seenSel[p] = true
			dedupParts = append(dedupParts, p)
		}
	}
	chSel = strings.Join(dedupParts, ", ")
	// Map virtual tag names to real ClickHouse columns (matching ZT behavior).
	chSelParts := strings.Split(chSel, ",")
	for i, p := range chSelParts {
		p = strings.TrimSpace(p)
		// Virtual tag → real ID column mapping (same as topColMap/flowLogColMap for flow_log).
		switch p {
		case "pod_node_1": chSelParts[i] = "pod_node_id_1"
		case "pod_node_0": chSelParts[i] = "pod_node_id_0"
		case "pod_ns_1": chSelParts[i] = "pod_ns_id_1"
		case "pod_ns_0": chSelParts[i] = "pod_ns_id_0"
		case "pod_cluster_1": chSelParts[i] = "pod_cluster_id_1"
		case "pod_cluster_0": chSelParts[i] = "pod_cluster_id_0"
		case "pod_service_1": chSelParts[i] = "pod_service_id_1"
		case "pod_service_0": chSelParts[i] = "pod_service_id_0"
		case "pod_group_1": chSelParts[i] = "pod_group_id_1"
		case "pod_group_0": chSelParts[i] = "pod_group_id_0"
		case "pod_1": chSelParts[i] = "pod_id_1"
		case "pod_0": chSelParts[i] = "pod_id_0"
		case "region_1": chSelParts[i] = "region_id_1"
		case "region_0": chSelParts[i] = "region_id_0"
		case "az_1": chSelParts[i] = "az_id_1"
		case "az_0": chSelParts[i] = "az_id_0"
		case "chost_1": chSelParts[i] = "l3_device_id_1"
		case "chost_0": chSelParts[i] = "l3_device_id_0"
		case "vpc_1": chSelParts[i] = "epc_id_1"
		case "vpc_0": chSelParts[i] = "epc_id_0"
		case "subnet_1": chSelParts[i] = "subnet_id_1"
		case "subnet_0": chSelParts[i] = "subnet_id_0"
		case "router_1": chSelParts[i] = "router_id_1"
		case "router_0": chSelParts[i] = "router_id_0"
		case "lb_1": chSelParts[i] = "lb_id_1"
		case "lb_0": chSelParts[i] = "lb_id_0"
		case "gprocess_1": chSelParts[i] = "gprocess_id_1"
		case "gprocess_0": chSelParts[i] = "gprocess_id_0"
		case "service_1": chSelParts[i] = "service_id_1"
		case "service_0": chSelParts[i] = "service_id_0"
		}
	}
	chSel = strings.Join(chSelParts, ", ")
	sel := fmt.Sprintf("%s, count(*) AS count_row", chSel)
	groupBy := chSel
	// Build SQL with `db`.`table` prefix for ClickHouse.
	fullTable := fmt.Sprintf("`%s`.`%s`", db, tbl)
	var clauses []string
	if timeStart > 0 { clauses = append(clauses, fmt.Sprintf("time >= %d", timeStart)) }
	if timeEnd > 0 { clauses = append(clauses, fmt.Sprintf("time <= %d", timeEnd)) }
	// Strip ZT-only virtual columns not present in CH (e.g., role).
	var chExtras []string
	for _, e := range extras {
		skip := false
		for _, vcol := range []string{"role"} {
			if strings.Contains(e, "`"+vcol+"`") { skip = true; break }
		}
		if !skip { chExtras = append(chExtras, e) }
	}
	clauses = append(clauses, chExtras...)
	whereClause := ""
	if len(clauses) > 0 { whereClause = " WHERE " + strings.Join(clauses, " AND ") }
	sql := fmt.Sprintf("SELECT %s FROM %s%s GROUP BY %s ORDER BY `count_row` DESC LIMIT %d", sel, fullTable, whereClause, groupBy, limit)
	if offset > 0 { sql += fmt.Sprintf(" OFFSET %d", offset) }
	log.Printf("🔍 CH fast_list fallback: db=%s sql=%s", db, sql)

	rows, err := showmetrics.HTTPQuery(sql)
	if err != nil {
		log.Printf("⚠️  CH fast_list error: %v", err)
		return nil
	}

	// Detect Enum(column) patterns in original SELECT for post-translation.
	var enumCols []string
	enumRe := regexp.MustCompile(`Enum\(([^)]+)\)`)
	for _, match := range enumRe.FindAllStringSubmatch(selStr, -1) {
		enumCols = append(enumCols, strings.TrimSpace(match[1]))
	}

	// Load int_enum_map for translation.
	enumCache := map[string]map[string]string{}
	for _, col := range enumCols {
		eRes, eErr := showmetrics.HTTPQuery(fmt.Sprintf("SELECT toString(value), name_zh FROM flow_tag.int_enum_map WHERE tag_name='%s'", col))
		m := map[string]string{}
		if eErr == nil {
			for _, er := range eRes {
				k := fmt.Sprintf("%v", er["toString(value)"])
				v := showtagvalues.GetSVStr(er, "name_zh")
				m[k] = v
			}
		}
		if len(m) == 0 {
			if fb, ok := showtagvalues.BuiltinEnumFallback[col]; ok {
				m = fb
			}
		}
		enumCache[col] = m
	}

	result := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		r := make(map[string]interface{})
		for k, v := range row {
			r[k] = v
		}
		// Add Enum(column) display name columns.
		for _, ec := range enumCols {
			enumKey := "Enum(" + ec + ")"
			if _, exists := r[enumKey]; !exists {
				if raw, ok := r[ec]; ok {
					rawStr := fmt.Sprintf("%v", raw)
					if m, ok := enumCache[ec]; ok {
						if display, ok2 := m[rawStr]; ok2 {
							r[enumKey] = display
						} else if f, err := strconv.ParseFloat(rawStr, 64); err == nil {
							if display, ok2 := m[fmt.Sprintf("%.0f", f)]; ok2 {
								r[enumKey] = display
							} else {
								r[enumKey] = raw
							}
						} else {
							r[enumKey] = raw
						}
					} else {
						r[enumKey] = raw
					}
				}
			}
		}
		r["_querier_region"] = "本地"
		result = append(result, r)
	}
	return result
}

// buildFastListDebug constructs the 4-entry _debug array matching the cloud's internal pipeline:
//
//	[0] QuerierJs发送请求 — frontend request as seen by the querier middleware
//	[1] QuerierJs收到响应 — querier middleware's SQL generation output
//	[2] Statistics发送请求 — request sent to the Statistics service

