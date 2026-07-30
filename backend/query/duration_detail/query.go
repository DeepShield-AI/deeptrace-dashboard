package duration_detail

import (
	"fmt"
	"log"
	"strings"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/query/fastlist"
	"deeptrace-backend/query/showmetrics"
)

// Request is the request body for duration_detail.
type Request struct {
	TimeStart int64     `json:"time_start"`
	TimeEnd   int64     `json:"time_end"`
	Region    string    `json:"region"`
	Offset    int       `json:"offset"`
	Limit     int       `json:"limit"`
	GroupBy   []string  `json:"group_by"`
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
func Query(ch *clickhouse.CHService, req *Request, db, tbl string) []map[string]interface{} {
	if req.Limit <= 0 { req.Limit = 20 }

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
						if strings.Contains(c, "`"+vcol+"`") { skip = true; break }
					}
					if !skip { tagConditions = append(tagConditions, c) }
				}
			}
			for _, gb := range rs.GroupBy {
				if !seenGroupBy[gb] { allGroupBys = append(allGroupBys, gb); seenGroupBy[gb] = true }
			}
		}
	}
	for _, gb := range req.GroupBy {
		if !seenGroupBy[gb] { allGroupBys = append(allGroupBys, gb); seenGroupBy[gb] = true }
	}

	ztVirtual := map[string]bool{"is_internet": true, "role": true}
	var filteredGB []string
	for _, gb := range allGroupBys {
		if !ztVirtual[gb] { filteredGB = append(filteredGB, gb) }
	}
	allGroupBys = filteredGB

	tagColMap := map[string]string{
		"auto_service": "app_service", "auto_instance": "app_instance",
		"observation_point": "observation_point",
	}
	var tagCols, groupParts []string
	for _, gb := range allGroupBys {
		mapped := gb
		if m, ok := tagColMap[gb]; ok { mapped = m }
		tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mapped, gb))
		groupParts = append(groupParts, fmt.Sprintf("`%s`", mapped))
	}
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
			tagCols = append(tagCols, "`app_service` AS `"+col+"`")
			groupParts = append(groupParts, "`app_service`")
		}
	}

	metricSelects := []string{
		"count(*) AS `Sum(请求)`",
		"avg(response_duration) AS `Avg(响应时延)`",
		"countIf(response_status != 2) / count(*) * 100 AS `Avg(响应比例)`",
		"countIf(response_status = 0) / count(*) * 100 AS `Avg(正常比例)`",
	}

	var wheres []string
	if req.TimeStart > 0 { wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart)) }
	if req.TimeEnd > 0 { wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd)) }
	wheres = append(wheres, tagConditions...)
	wheres = append(wheres, metricConditions...)
	whereStr := ""
	if len(wheres) > 0 { whereStr = " WHERE " + strings.Join(wheres, " AND ") }

	fullTable := fmt.Sprintf("`%s`.`%s`", db, tbl)
	allSel := append(tagCols, metricSelects...)
	sql := fmt.Sprintf("SELECT %s FROM %s%s GROUP BY %s ORDER BY `Sum(请求)` DESC LIMIT %d",
		strings.Join(allSel, ", "), fullTable, whereStr, strings.Join(groupParts, ", "), req.Limit)
	if req.Offset > 0 { sql += fmt.Sprintf(" OFFSET %d", req.Offset) }
	log.Printf("📊 duration_detail SQL: %s", sql)

	rows, err := showmetrics.HTTPQuery(sql)
	if err != nil {
		log.Printf("⚠️ duration_detail CH error: %v", err)
		return nil
	}
	return rows
}
