package duration_detail

import (
	"context"
	"fmt"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/logging"
	"deeptrace-backend/query/fastlist"
)

// Request is the request body for duration_detail.
type Request struct {
	TimeStart int64    `json:"time_start"`
	TimeEnd   int64    `json:"time_end"`
	Region    string   `json:"region"`
	Offset    int      `json:"offset"`
	Limit     int      `json:"limit"`
	GroupBy   []string `json:"group_by"`
	Where     *struct {
		Paths        []map[string]string `json:"paths"`
		ResourceSets []struct {
			ID        string        `json:"id"`
			Condition []interface{} `json:"condition"`
			GroupBy   []string      `json:"groupBy"`
		} `json:"resourceSets"`
	} `json:"where"`
}

// Query executes a duration_detail query against ClickHouse.
// Returns nil when the query fails.
func Query(ch *clickhouse.CHService, ctx context.Context, req *Request, db, tbl string) []map[string]interface{} {
	if req.Limit <= 0 {
		req.Limit = 20
	}

	var tagConditions, metricConditions []string
	var allGroupBys []string
	seenGroupBy := map[string]bool{}

	if req.Where != nil {
		for _, rs := range req.Where.ResourceSets {
			conds := fastlist.FlattenFastListConditions(rs.Condition, "flow_log")
			for _, c := range conds {
				if strings.Contains(c, "response_duration") || strings.Contains(c, "request") {
					metricConditions = append(metricConditions, c)
				} else {
					skip := false
					for _, vcol := range []string{"role", "is_internet", "ip"} {
						if strings.Contains(c, "`"+vcol+"`") {
							skip = true
							break
						}
					}
					if !skip {
						tagConditions = append(tagConditions, c)
					}
				}
			}
			for _, gb := range rs.GroupBy {
				if !seenGroupBy[gb] {
					allGroupBys = append(allGroupBys, gb)
					seenGroupBy[gb] = true
				}
			}
		}
	}
	for _, gb := range req.GroupBy {
		if !seenGroupBy[gb] {
			allGroupBys = append(allGroupBys, gb)
			seenGroupBy[gb] = true
		}
	}

	// Virtual tags without a physical column — drop them from grouping.
	// auto_service (bare) is virtual too: the per-side auto_service_0/1
	// name columns are generated below (cloud contract has no bare
	// auto_service key).
	ztVirtual := map[string]bool{"is_internet": true, "role": true, "auto_service": true}
	filteredGB := allGroupBys[:0]
	for _, gb := range allGroupBys {
		if !ztVirtual[gb] {
			filteredGB = append(filteredGB, gb)
		}
	}
	allGroupBys = filteredGB

	tagColMap := map[string]string{
		"auto_instance":     "app_instance",
		"observation_point": "observation_point",
	}
	var tagCols, groupParts []string
	for _, gb := range allGroupBys {
		mapped := gb
		if m, ok := tagColMap[gb]; ok {
			mapped = m
		} else if strings.HasPrefix(gb, "auto_service_") && len(gb) > len("auto_service_") {
			// per-side virtual name tag: pure dictionary resolution (no IP
			// fallback — it would drag is_ipv4/ip4/ip6 into GROUP BY),
			// grouping on the physical type+id key columns.
			side := gb[len("auto_service_"):]
			tagCols = append(tagCols, fmt.Sprintf(
				"dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_%s), toUInt64(auto_service_id_%s)), toString(auto_service_id_%s)) AS `%s`",
				side, side, side, gb))
			groupParts = append(groupParts, "`auto_service_id_"+side+"`", "`auto_service_type_"+side+"`")
			continue
		}
		tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mapped, gb))
		groupParts = append(groupParts, fmt.Sprintf("`%s`", mapped))
	}
	// Always include per-side auto_service id/type columns so downstream
	// filters can reference them. The name column auto_service_0/1 is a
	// virtual tag — resolve it through the unified flow_log map (dictionary),
	// grouping on the physical ID column.
	for _, side := range []string{"_0", "_1"} {
		for _, tag := range []string{"auto_service_id", "auto_service_type"} {
			col := tag + side
			if !seenGroupBy[col] {
				tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", col, col))
				groupParts = append(groupParts, fmt.Sprintf("`%s`", col))
			}
		}
		col := "auto_service" + side
		if !seenGroupBy[col] {
			// Pure dictionary resolution (no IP fallback — see above).
			tagCols = append(tagCols, fmt.Sprintf(
				"dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type%s), toUInt64(auto_service_id%s)), toString(auto_service_id%s)) AS `%s`",
				side, side, side, col))
			groupParts = append(groupParts, "`auto_service_id"+side+"`", "`auto_service_type"+side+"`")
		}
	}

	metricSelects := []string{
		"count(*) AS `Sum(请求)`",
		"avg(response_duration) AS `Avg(响应时延)`",
		"countIf(response_status != 2) / count(*) * 100 AS `Avg(响应比例)`",
		"countIf(response_status = 0) / count(*) * 100 AS `Avg(正常比例)`",
	}
	// Cloud contract carries Enum(observation_point) next to the raw
	// observation_point value — translate through the string_enum_map
	// dictionary (same key shape as the fast_list Enum expansion).
	// any() keeps it legal when observation_point is not grouped.
	tagCols = append(tagCols,
		"any(dictGetOrDefault('flow_tag.string_enum_map', 'name_zh', ('observation_point', observation_point), observation_point)) AS `Enum(observation_point)`")

	var wheres []string
	if req.TimeStart > 0 {
		wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart))
	}
	if req.TimeEnd > 0 {
		wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd))
	}
	wheres = append(wheres, tagConditions...)
	wheres = append(wheres, metricConditions...)
	whereStr := ""
	if len(wheres) > 0 {
		whereStr = " WHERE " + strings.Join(wheres, " AND ")
	}

	// flow_log Distributed tables are broken on this deployment — use _local.
	resolvedTbl := tbl
	if db == "flow_log" && !strings.Contains(resolvedTbl, "_local") {
		resolvedTbl += "_local"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTbl)
	allSel := append(tagCols, metricSelects...)
	sql := fmt.Sprintf("SELECT %s FROM %s%s GROUP BY %s ORDER BY `Sum(请求)` DESC LIMIT %d",
		strings.Join(allSel, ", "), fullTable, whereStr, strings.Join(groupParts, ", "), req.Limit)
	if req.Offset > 0 {
		sql += fmt.Sprintf(" OFFSET %d", req.Offset)
	}
	logging.Debugf("duration_detail SQL: %s", sql)

	if ch == nil || !ch.Enabled() {
		return nil
	}
	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rows, err := ch.Query(qCtx, sql)
	if err != nil {
		logging.Errorf("duration_detail CH error: %v", err)
		return nil
	}
	defer rows.Close()
	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		logging.Errorf("duration_detail scan error: %v", err)
		return nil
	}
	if data == nil {
		data = []map[string]interface{}{}
	}
	decorateRows(data, queryID(req), clickhouse.QuerierRegion)
	return data
}

// queryID derives the cloud-format query_id from the where.paths pairs
// (client-server, e.g. "R1-R2"). Empty when the request has no paths.
func queryID(req *Request) string {
	if req.Where == nil {
		return ""
	}
	var ids []string
	for _, p := range req.Where.Paths {
		ids = append(ids, p["client"]+"-"+p["server"])
	}
	return strings.Join(ids, ",")
}

// decorateRows adds the cloud-contract decoration fields: per-side
// node_type/icon_id derived from auto_service_type (NodeTypeFor/IconFor),
// the request-scoped query_id, and _querier_region.
func decorateRows(data []map[string]interface{}, qid, region string) {
	for _, row := range data {
		if qid != "" {
			row["query_id"] = qid
		}
		for _, side := range []struct{ typ, node, icon string }{
			{"auto_service_type_0", "client_node_type", "client_icon_id"},
			{"auto_service_type_1", "server_node_type", "server_icon_id"},
		} {
			if t, ok := clickhouse.ToIntOK(row[side.typ]); ok {
				row[side.node] = clickhouse.NodeTypeFor(t)
				row[side.icon] = clickhouse.IconFor(t)
			}
		}
		if region != "" {
			row["_querier_region"] = region
		}
	}
}
