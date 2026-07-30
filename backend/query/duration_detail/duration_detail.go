package duration_detail

import (
	"fmt"
	"strings"

	"deeptrace-backend/clickhouse"
)

// BuildDurationDetailSQL builds the ClickHouse SQL for duration_detail queries.
func BuildSQL(tagConditions, metricConditions []string, allGroupBys []string, timeStart, timeEnd int64, limit, offset int) string {
		tagColMap := map[string]string{			"auto_service": "app_service", "auto_instance": "app_instance",			"observation_point": "observation_point",		}		var tagCols, groupParts []string		for _, gb := range allGroupBys {			mapped := gb			if m, ok := tagColMap[gb]; ok { mapped = m }			tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", mapped, gb))			groupParts = append(groupParts, fmt.Sprintf("`%s`", mapped))		}		for _, side := range []string{"_0", "_1"} {			for _, tag := range []string{"auto_service_id", "auto_service_type"} {				col := tag + side				if !seenGroupBy[col] {					tagCols = append(tagCols, fmt.Sprintf("`%s` AS `%s`", col, col))					groupParts = append(groupParts, fmt.Sprintf("`%s`", col))				}			}			col := "auto_service" + side			if !seenGroupBy[col] {				tagCols = append(tagCols, "`app_service` AS `"+col+"`")				groupParts = append(groupParts, "`app_service`")			}		}		metricSelects := []string{			"count(*) AS `Sum(请求)`",			"avg(response_duration) AS `Avg(响应时延)`",			"countIf(response_status != 2) / count(*) * 100 AS `Avg(响应比例)`",			"countIf(response_status = 0) / count(*) * 100 AS `Avg(正常比例)`",		}		var wheres []string		if req.TimeStart > 0 { wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart)) }		if req.TimeEnd > 0 { wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd)) }		wheres = append(wheres, tagConditions...)		wheres = append(wheres, metricConditions...)		whereStr := ""		if len(wheres) > 0 { whereStr = " WHERE " + strings.Join(wheres, " AND ") }		fullTable := "`flow_log`.`l7_flow_log`"		allSel := append(tagCols, metricSelects...)		sql := fmt.Sprintf("SELECT %s FROM %s%s GROUP BY %s ORDER BY `Sum(请求)` DESC LIMIT %d",			strings.Join(allSel, ", "), fullTable, whereStr, strings.Join(groupParts, ", "), req.Limit)		if req.Offset > 0 { sql += fmt.Sprintf(" OFFSET %d", req.Offset) }		log.Printf("📊 duration_detail SQL: %s", sql)	return sql
}
