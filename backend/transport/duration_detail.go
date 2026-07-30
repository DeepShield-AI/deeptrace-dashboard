package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"deeptrace-backend/clickhouse"
)

type durationDetailRequest struct {
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

func handleDurationDetail(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req durationDetailRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, "bad request", 400)
			return
		}
		if req.Limit <= 0 { req.Limit = 20 }
		if req.TimeStart == 0 || req.TimeEnd == 0 {
			writeSuccess(w, map[string]interface{}{"result": []interface{}{}})
			return
		}
		log.Printf("📊 duration_detail: time=%d-%d limit=%d", req.TimeStart, req.TimeEnd, req.Limit)

		var tagConditions, metricConditions []string
		var allGroupBys []string
		seenGroupBy := map[string]bool{}

		if req.Where != nil {
			for _, rs := range req.Where.ResourceSets {
				conds := flattenFastListConditions(rs.Condition, "flow_log")
				for _, c := range conds {
					if strings.Contains(c, "response_duration") || strings.Contains(c, "request") {
						metricConditions = append(metricConditions, c)
					} else {
						// Strip ZT-only virtual columns not present in CH (e.g., role).
						skip := false
						for _, vcol := range []string{"role", "is_internet", "ip"} {
							if strings.Contains(c, "`"+vcol+"`") { skip = true; break }
						}
						if !skip {
							tagConditions = append(tagConditions, c)
						}
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

		// Strip ZT-only virtual columns not present in CH.
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

		fullTable := "`flow_log`.`l7_flow_log`"
		allSel := append(tagCols, metricSelects...)
		sql := fmt.Sprintf("SELECT %s FROM %s%s GROUP BY %s ORDER BY `Sum(请求)` DESC LIMIT %d",
			strings.Join(allSel, ", "), fullTable, whereStr, strings.Join(groupParts, ", "), req.Limit)
		if req.Offset > 0 { sql += fmt.Sprintf(" OFFSET %d", req.Offset) }
		log.Printf("📊 duration_detail SQL: %s", sql)

		rows, err := clickhouse.HTTPQuery(sql)
		if err != nil {
			log.Printf("⚠️ duration_detail CH error: %v", err)
			writeSuccess(w, map[string]interface{}{"result": []interface{}{}})
			return
		}

		result := make([]map[string]interface{}, 0, len(rows))
		for _, row := range rows {
			r := make(map[string]interface{})
			r["query_id"] = "R1-R2"
			for k, v := range row { r[k] = v }
			for _, prefix := range []string{"client_", "server_"} {
				typeCol := "auto_service_type_0"
				iconKey, nodeKey := prefix+"icon_id", prefix+"node_type"
				if prefix == "server_" { typeCol = "auto_service_type_1" }
				if _, exists := r[nodeKey]; !exists {
					if tv, ok := r[typeCol]; ok {
						if t, ok2 := toInt(tv); ok2 {
							r[nodeKey] = clickhouse.NodeTypeFor(t)
							r[iconKey] = clickhouse.IconFor(t)
						}
					}
				}
			}
			r["_querier_region"] = "本地"
			if _, exists := r["Enum(observation_point)"]; !exists {
				if op, ok := r["observation_point"]; ok {
					if fb, found := builtinEnumFallback["observation_point"]; found {
						if display, ok2 := fb[fmt.Sprintf("%v", op)]; ok2 {
							r["Enum(observation_point)"] = display
						} else { r["Enum(observation_point)"] = op }
					} else { r["Enum(observation_point)"] = op }
				}
			}
			result = append(result, r)
		}

		var maxRequest, maxDuration map[string]interface{}
		var maxReqVal, maxDurVal float64
		for _, r := range result {
			if v, ok := toFloat(r["Sum(请求)"]); ok && (maxRequest == nil || v > maxReqVal) {
				maxRequest, maxReqVal = r, v
			}
			if v, ok := toFloat(r["Avg(响应时延)"]); ok && (maxDuration == nil || v > maxDurVal) {
				maxDuration, maxDurVal = r, v
			}
		}

		resp := map[string]interface{}{"result": result, "maxRequest": maxRequest, "maxResponseDuration": maxDuration}
		writeSuccess(w, resp)
	}
}
