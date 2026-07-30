package flowlog

import (
	"deeptrace-backend/query"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
)

// QueryFlowLogDetailCH queries ClickHouse directly for FlowLog detail data.
func QueryFlowLogDetailCH(ch *clickhouse.CHService, ctx context.Context, bodyStr string) (*query.QueryFlowLogResult, error) {
	var req struct {
		Database string `json:"DATABASE"`
		Table    string `json:"TABLE"`
		Queries  []struct {
			QueryID string   `json:"QUERY_ID"`
			Roles   []string `json:"ROLES"`
			Select  string   `json:"SELECT"`
		} `json:"QUERIES"`
		TimeStart int64 `json:"time_start"`
		TimeEnd   int64 `json:"time_end"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &req); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	db := req.Database
	if db == "" { db = "flow_log" }
	tbl := req.Table
	if tbl == "" { tbl = "l7_flow_log" }
	resolvedTable := tbl
	if !strings.Contains(tbl, ".") && db == "flow_metrics" {
		resolvedTable = tbl + ".1m"
	}
	fullTable := fmt.Sprintf("`%s`.`%s`", db, resolvedTable)

	colMap := map[string]string{
		"auto_service_0":     "if(empty(app_service), toString(auto_service_id_0), app_service)",
		"auto_instance_0":    "if(empty(app_instance), toString(auto_instance_id_0), app_instance)",
		"auto_service_1":     "if(empty(app_service), toString(auto_service_id_1), app_service)",
		"auto_instance_1":    "if(empty(app_instance), toString(auto_instance_id_1), app_instance)",
		"auto_service_id_0":  "auto_service_id_0",
		"auto_service_id_1":  "auto_service_id_1",
		"auto_instance_id_0": "auto_instance_id_0",
		"auto_instance_id_1": "auto_instance_id_1",
		"protocol":           "l7_protocol",
		"_id":                "toString(_id)",
	}
	isFlowLogDetail := db == "flow_log"
	if isFlowLogDetail {
		colMap["event_type"] = "l7_protocol"
		colMap["auto_instance"] = "if(empty(app_instance), toString(auto_instance_id_0), app_instance)"
		colMap["event_desc"] = "request_resource"
	}

	selectCols := "*"
	if len(req.Queries) > 0 && req.Queries[0].Select != "" {
		items := clickhouse.ParseSelectList(req.Queries[0].Select)
		var cols []string
		for _, item := range items {
			lower := strings.ToLower(item.Expr)
			switch {
			case strings.HasPrefix(lower, "newtag("):
				continue
			case strings.HasPrefix(lower, "enum("):
				inner := strings.TrimSpace(item.Expr[len("Enum(") : len(item.Expr)-1])
				cols = append(cols, fmt.Sprintf("`%s` AS `%s`", inner, item.Key))
			case strings.HasPrefix(lower, "icon_id("):
				cols = append(cols, fmt.Sprintf("-13 AS `%s`", item.Key))
			case strings.HasPrefix(lower, "node_type("):
				inner := strings.TrimSpace(item.Expr[len("node_type(") : len(item.Expr)-1])
				cols = append(cols, fmt.Sprintf("toString(`%s`) AS `%s`", inner, item.Key))
			default:
				col := strings.Trim(item.Expr, "`")
				if mapped, ok := colMap[col]; ok {
					cols = append(cols, fmt.Sprintf("%s AS `%s`", mapped, item.Key))
				} else if col != item.Key {
					cols = append(cols, fmt.Sprintf("`%s` AS `%s`", col, item.Key))
				} else {
					cols = append(cols, fmt.Sprintf("`%s`", col))
				}
			}
		}
		if len(cols) > 0 {
			selectCols = strings.Join(cols, ", ")
		}
	}

	var wheres []string
	if req.TimeStart > 0 { wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart)) }
	if req.TimeEnd > 0 { wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd)) }

	sql := fmt.Sprintf("SELECT %s FROM %s", selectCols, fullTable)
	if len(wheres) > 0 { sql += " WHERE " + strings.Join(wheres, " AND ") }
	sql += " LIMIT 500"

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := ch.Query(qCtx, sql)
	if err != nil { return nil, fmt.Errorf("query: %w", err) }
	defer rows.Close()

	rawData, err := clickhouse.ScanRows(rows)
	if err != nil { return nil, fmt.Errorf("scan: %w", err) }
	if rawData == nil { rawData = []map[string]interface{}{} }

	enumDisplay := map[string]map[string]string{
		"response_status":   {"0": "正常", "1": "异常", "2": "超时", "3": "服务端异常", "4": "客户端异常", "5": "取消"},
		"observation_point": {"c": "客户端网卡", "s": "服务端网卡", "c-p": "客户侧网络", "s-p": "服务侧网络", "c-app": "客户端应用", "s-app": "服务端应用", "app": "应用", "rest": "其他"},
		"l7_protocol":       {"20": "HTTP", "21": "Dubbo", "41": "gRPC", "60": "MySQL", "61": "PostgreSQL", "68": "Redis", "80": "DNS", "100": "TLS", "120": "FastCGI"},
		"is_tls":            {"0": "否", "1": "是"},
		"is_async":          {"0": "否", "1": "是"},
		"status":            {"0": "正常", "1": "异常", "2": "超时"},
		"protocol":          {"6": "TCP", "17": "UDP"},
		"close_type":        {"0": "TCP 连接超时", "1": "TCP 连接重置", "2": "TCP 服务端断开", "3": "TCP 客户端断开", "4": "TCP 服务端 fin", "5": "周期性上报"},
		"event_type": {"0": "读", "1": "写", "2": "创建", "3": "删除", "4": "修改权限", "5": "修改属性", "6": "修改名称", "7": "打开", "8": "关闭", "9": "读目录",
			"read": "读", "write": "写", "create": "创建", "delete": "删除"},
	}

	var processed []map[string]interface{}
	for _, row := range rawData {
		row["_querier_region"] = "本地"
		for k, v := range row {
			if v == nil {
				if k == "response_code" { row[k] = 0 }
				if k == "response_exception" { row[k] = "" }
				continue
			}
			strVal := fmt.Sprintf("%v", v)
			if strings.HasPrefix(k, "Enum(") && strings.HasSuffix(k, ")") {
				innerKey := k[5 : len(k)-1]
				if emap, ok := enumDisplay[innerKey]; ok {
					if display, ok2 := emap[strVal]; ok2 { row[k] = display }
				}
			}
			if k == "event_type" {
				enumName := "event_type"
				if isFlowLogDetail { enumName = "l7_protocol" }
				if emap, ok := enumDisplay[enumName]; ok {
					if display, ok2 := emap[strVal]; ok2 { row[k] = display }
				}
			}
			if k == "_id" { row[k] = strVal }
			if k == "start_time" || k == "end_time" {
				switch val := v.(type) {
				case float64:
					row[k] = time.UnixMicro(int64(val)).Format("2006-01-02T15:04:05.000000-07:00")
				case uint64:
					row[k] = time.UnixMicro(int64(val)).Format("2006-01-02T15:04:05.000000-07:00")
				case int64:
					row[k] = time.UnixMicro(val).Format("2006-01-02T15:04:05.000000-07:00")
				}
			}
			if strings.Contains(k, "icon_id") {
				if fv, ok := v.(float64); ok && fv == 0 { row[k] = float64(-16) }
			}
		}
		processed = append(processed, row)
	}
	return &query.QueryFlowLogResult{Data: processed}, nil
}
