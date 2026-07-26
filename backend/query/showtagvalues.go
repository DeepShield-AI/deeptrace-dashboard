package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
)

// ShowTagValuesRequest is the JSON body of a ShowTagValues query.
type ShowTagValuesRequest struct {
	Database string `json:"DATABASE"`
	Table    string `json:"TABLE"`
	Tag      string `json:"TAG"`
	Limit    int    `json:"LIMIT"`
	Where    string `json:"WHERE"`
}

// QueryShowTagValues queries deepflow-server for distinct tag values.
func QueryShowTagValues(zt *client.ZerotraceService, bodyStr string) (*Result, error) {
	if zt == nil || !zt.Available() {
		return nil, nil
	}
	var req ShowTagValuesRequest
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, nil
	}
	db := req.Database
	if db == "" {
		db = "flow_log"
	}
	tbl := req.Table
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	tag := req.Tag
	if tag == "" {
		return nil, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	// ZT only supports flow_log / event / application_log.
	if db != "flow_log" && db != "event" && db != "application_log" {
		return nil, nil
	}
	enumTagSet := map[string]bool{
		"response_status": true, "status": true, "state": true,
		"protocol": true, "observation_point": true, "l7_protocol": true,
	}
	var selectExpr string
	if enumTagSet[tag] {
		selectExpr = fmt.Sprintf("Enum(`%s`) AS `display_name`, `%s` AS `value`", tag, tag)
	} else {
		selectExpr = fmt.Sprintf("`%s` AS `value`, `%s` AS `display_name`", tag, tag)
	}
	sql := fmt.Sprintf("SELECT DISTINCT %s FROM `%s`", selectExpr, tbl)
	if req.Where != "" {
		sql += " WHERE " + req.Where
	}
	sql += fmt.Sprintf(" LIMIT %d", limit)
	rows, err := zt.QueryRaw(db, sql)
	if err != nil {
		return nil, nil
	}
	if len(rows.Values) == 0 {
		return &Result{Data: []map[string]interface{}{}, Type: "DBDescription"}, nil
	}
	data := make([]map[string]interface{}, 0, len(rows.Values))
	for _, row := range rows.Values {
		r := make(map[string]interface{}, len(rows.Columns)+1)
		for i, col := range rows.Columns {
			if i >= len(row) {
				continue
			}
			val := row[i]
			if num, ok := val.(json.Number); ok {
				if f, err := num.Float64(); err == nil {
					val = f
				} else {
					val = num.String()
				}
			}
			r[col] = val
		}
		r["_querier_region"] = "本地"
		data = append(data, r)
	}
	return &Result{Data: data, Count: len(data), Type: "DBDescription", Fields: map[string]interface{}{}}, nil
}

var intEnumTags = map[string]bool{
	"response_status": true, "status": true, "state": true,
	"protocol": true, "l7_protocol": true, "l4_protocol": true,
	"event_level": true, "policy_type": true, "tap_type": true,
	"signal_source": true, "tunnel_type": true, "nat_source": true,
	"observation_point": true,
}

var stringEnumTags = map[string]bool{
	"event_type": true,
}

// TryCHShowTagValues queries ClickHouse for tag values via the native driver.
func TryCHShowTagValues(ch *clickhouse.CHService, bodyStr string) (*Result, error) {
	if ch == nil || !ch.Enabled() {
		return nil, nil
	}
	var req ShowTagValuesRequest
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, nil
	}
	tag := req.Tag
	if tag == "" {
		return nil, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Int enum map.
	if intEnumTags[tag] {
		sql := fmt.Sprintf("SELECT `value`, `name_zh` AS `display_name`, `description_zh` AS `description` FROM `flow_tag`.`int_enum_map` WHERE `tag_name` = '%s' LIMIT %d", tag, limit)
		rows, err := ch.Query(ctx, sql)
		if err == nil {
			data, err2 := clickhouse.ScanRows(rows)
			rows.Close()
			if err2 == nil && len(data) > 0 {
				for _, r := range data {
					r["_querier_region"] = "本地"
				}
				return &Result{Data: data, Count: len(data), Type: "DBDescription", Fields: map[string]interface{}{}}, nil
			}
		}
	}

	// 2. String enum map.
	if stringEnumTags[tag] {
		sql := fmt.Sprintf("SELECT `value`, `name_zh` AS `display_name`, `description_zh` AS `description` FROM `flow_tag`.`string_enum_map` WHERE `tag_name` = '%s' LIMIT %d", tag, limit)
		rows, err := ch.Query(ctx, sql)
		if err == nil {
			data, err2 := clickhouse.ScanRows(rows)
			rows.Close()
			if err2 == nil && len(data) > 0 {
				for _, r := range data {
					r["_querier_region"] = "本地"
				}
				return &Result{Data: data, Count: len(data), Type: "DBDescription", Fields: map[string]interface{}{}}, nil
			}
		}
	}

	// 3. SELECT DISTINCT from data table.
	db := req.Database
	if db == "" {
		db = "flow_log"
	}
	tbl := req.Table
	if tbl == "" {
		tbl = "l7_flow_log"
	}
	resolvedTable := tbl
	if db == "flow_metrics" && !strings.Contains(tbl, ".") {
		resolvedTable = tbl + ".1m"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)
	sql := fmt.Sprintf("SELECT DISTINCT `%s` AS `value`, `%s` AS `display_name` FROM %s LIMIT %d", tag, tag, fullTable, limit)
	if req.Where != "" {
		sql = fmt.Sprintf("SELECT DISTINCT `%s` AS `value`, `%s` AS `display_name` FROM %s WHERE %s LIMIT %d", tag, tag, fullTable, req.Where, limit)
	}
	rows, err := ch.Query(ctx, sql)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	data, err := clickhouse.ScanRows(rows)
	if err != nil || len(data) == 0 {
		return nil, nil
	}
	for _, r := range data {
		r["_querier_region"] = "本地"
	}
	return &Result{Data: data, Count: len(data), Type: "DBDescription", Fields: map[string]interface{}{}}, nil
}
